package cx

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const OAuth2PrivateKeySecretKey = "private.key"

const pidPrefixOAuth2HeadlessServer = "com.liferay.oauth2.provider.configuration.OAuth2ProviderApplicationHeadlessServerConfiguration~"

type jsonWebKey struct {
	Algorithm string `json:"alg"`
	Exponent  string `json:"e"`
	KeyID     string `json:"kid"`
	KeyType   string `json:"kty"`
	Modulus   string `json:"n"`
	Use       string `json:"use"`
}

func base64URL(bytes []byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func clientAuthenticationMethod(clientExtension *cxv1alpha1.ClientExtension) string {
	if (clientExtension.Spec.OAuth2 == nil) || (clientExtension.Spec.OAuth2.ClientAuthenticationMethod == "") {
		return cxv1alpha1.ClientAuthenticationMethodClientSecretPost
	}

	return clientExtension.Spec.OAuth2.ClientAuthenticationMethod
}

func (clientExtensionReconciler *ClientExtensionReconciler) ensureOAuth2Credentials(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	externalReferenceCodes []string,
) (*rsa.PrivateKey, map[string]string, error) {
	name := types.NamespacedName{Name: oAuth2PrivateKeySecretName(clientExtension), Namespace: clientExtension.Namespace}

	var secret corev1.Secret

	error := clientExtensionReconciler.APIReader.Get(context, name, &secret)

	if apierrors.IsNotFound(error) {
		return clientExtensionReconciler.createOAuth2Credentials(clientExtension, context, externalReferenceCodes, name)
	}

	if error != nil {
		return nil, nil, error
	}

	privateKey, error := parseOAuth2PrivateKey(secret.Data[OAuth2PrivateKeySecretKey])

	if error != nil {
		return nil, nil, error
	}

	clientIDs := map[string]string{}
	missing := false

	for _, externalReferenceCode := range externalReferenceCodes {
		clientID := string(secret.Data[clientIDSecretKey(externalReferenceCode)])

		if clientID == "" {
			clientID = newClientID()
			missing = true

			secret.Data[clientIDSecretKey(externalReferenceCode)] = []byte(clientID)
		}

		clientIDs[externalReferenceCode] = clientID
	}

	if missing {
		if error := clientExtensionReconciler.Update(context, &secret); error != nil {
			return nil, nil, error
		}
	}

	return privateKey, clientIDs, nil
}

func (clientExtensionReconciler *ClientExtensionReconciler) createOAuth2Credentials(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
	externalReferenceCodes []string,
	name types.NamespacedName,
) (*rsa.PrivateKey, map[string]string, error) {
	privateKey, error := rsa.GenerateKey(rand.Reader, 2048)

	if error != nil {
		return nil, nil, error
	}

	pkcs8, error := x509.MarshalPKCS8PrivateKey(privateKey)

	if error != nil {
		return nil, nil, error
	}

	clientIDs := map[string]string{}

	secret := corev1.Secret{
		Data: map[string][]byte{
			OAuth2PrivateKeySecretKey: pem.EncodeToMemory(&pem.Block{Bytes: pkcs8, Type: "PRIVATE KEY"}),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    map[string]string{LabelOwner: clientExtension.Name},
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
	}

	for _, externalReferenceCode := range externalReferenceCodes {
		clientIDs[externalReferenceCode] = newClientID()

		secret.Data[clientIDSecretKey(externalReferenceCode)] = []byte(clientIDs[externalReferenceCode])
	}

	if error := controllerutil.SetControllerReference(
		clientExtension, &secret, clientExtensionReconciler.Scheme(),
	); error != nil {
		return nil, nil, error
	}

	if error := clientExtensionReconciler.Create(context, &secret); error != nil {
		if apierrors.IsAlreadyExists(error) {
			return clientExtensionReconciler.ensureOAuth2Credentials(clientExtension, context, externalReferenceCodes)
		}

		return nil, nil, error
	}

	clientExtensionReconciler.Recorder.Eventf(
		clientExtension, corev1.EventTypeNormal, "OAuth2CredentialsCreated",
		"Created OAuth 2 private key %s and client IDs in Secret %s", jwkThumbprint(&privateKey.PublicKey), name.Name,
	)

	return privateKey, clientIDs, nil
}

// extInitRequiredIdentifiers returns the OAuth 2 applications whose
// credentials the workload reads from the ext-init ConfigMap. A private_key_jwt
// client extension reads none: its headless server client IDs and key are in
// the private key Secret, and the SDK looks up user agent client IDs at DXP.
func extInitRequiredIdentifiers(clientExtension *cxv1alpha1.ClientExtension) []string {
	if clientAuthenticationMethod(clientExtension) == cxv1alpha1.ClientAuthenticationMethodPrivateKeyJWT {
		return nil
	}

	return extInitIdentifiers(clientExtension)
}

func headlessServerExternalReferenceCodes(configs map[string]cxv1alpha1.Configuration) []string {
	var externalReferenceCodes []string

	for pid := range configs {
		if !strings.HasPrefix(pid, pidPrefixOAuth2HeadlessServer) {
			continue
		}

		externalReferenceCode, _, _ := strings.Cut(strings.TrimPrefix(pid, pidPrefixOAuth2HeadlessServer), "/")

		externalReferenceCodes = append(externalReferenceCodes, externalReferenceCode)
	}

	slices.Sort(externalReferenceCodes)

	return slices.Compact(externalReferenceCodes)
}

func injectPrivateKeyJWT(
	configs map[string]cxv1alpha1.Configuration,
	publicKey *rsa.PublicKey,
	clientIDs map[string]string,
) (map[string]cxv1alpha1.Configuration, error) {
	jwks, error := json.Marshal(map[string][]jsonWebKey{"keys": {newJSONWebKey(publicKey)}})

	if error != nil {
		return nil, error
	}

	injectedConfigs := make(map[string]cxv1alpha1.Configuration, len(configs))

	for pid, configuration := range configs {
		if !strings.HasPrefix(pid, pidPrefixOAuth2HeadlessServer) {
			injectedConfigs[pid] = configuration

			continue
		}

		properties := map[string]any{}

		if error := json.Unmarshal(configuration.Raw, &properties); error != nil {
			return nil, fmt.Errorf("unable to parse configuration %s: %w", pid, error)
		}

		externalReferenceCode, _, _ := strings.Cut(strings.TrimPrefix(pid, pidPrefixOAuth2HeadlessServer), "/")

		properties["clientAuthenticationMethod"] = cxv1alpha1.ClientAuthenticationMethodPrivateKeyJWT

		if clientID := clientIDs[externalReferenceCode]; clientID != "" {
			properties["clientId"] = clientID
		}

		properties["jwks"] = string(jwks)

		raw, error := json.Marshal(properties)

		if error != nil {
			return nil, error
		}

		configuration.Raw = raw

		injectedConfigs[pid] = configuration
	}

	return injectedConfigs, nil
}

func clientIDSecretKey(externalReferenceCode string) string {
	return externalReferenceCode + ".oauth2.headless.server.client.id"
}

func jwkThumbprint(publicKey *rsa.PublicKey) string {

	// RFC 7638: SHA-256 over the required members in lexicographic order

	canonical := fmt.Sprintf(
		`{"e":"%s","kty":"RSA","n":"%s"}`,
		base64URL(big.NewInt(int64(publicKey.E)).Bytes()), base64URL(publicKey.N.Bytes()),
	)

	digest := sha256.Sum256([]byte(canonical))

	return base64URL(digest[:])
}

func newJSONWebKey(publicKey *rsa.PublicKey) jsonWebKey {
	return jsonWebKey{
		Algorithm: "RS256",
		Exponent:  base64URL(big.NewInt(int64(publicKey.E)).Bytes()),
		KeyID:     jwkThumbprint(publicKey),
		KeyType:   "RSA",
		Modulus:   base64URL(publicKey.N.Bytes()),
		Use:       "sig",
	}
}

func newClientID() string {
	return "id-" + uuid.NewString()
}

func oAuth2PrivateKeySecretName(clientExtension *cxv1alpha1.ClientExtension) string {
	return clientExtension.Name + "-oauth2-private-key"
}

func parseOAuth2PrivateKey(bytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(bytes)

	if block == nil {
		return nil, errors.New("oauth2 private key: no PEM block")
	}

	key, error := x509.ParsePKCS8PrivateKey(block.Bytes)

	if error != nil {
		return nil, error
	}

	privateKey, ok := key.(*rsa.PrivateKey)

	if !ok {
		return nil, errors.New("oauth2 private key: not an RSA key")
	}

	return privateKey, nil
}

const ReasonTokenRequestFailed = "TokenRequestFailed"

var tokenHTTPClient = &http.Client{Timeout: 10 * time.Second}

// requestToken proves that DXP has the OAuth 2 application with this client
// ID and public key: it requests a client credentials token with a
// private_key_jwt client assertion (RFC 7523).
func requestToken(context context.Context, tokenURL string, clientID string, privateKey *rsa.PrivateKey) error {
	header, error := json.Marshal(map[string]string{"alg": "RS256", "kid": jwkThumbprint(&privateKey.PublicKey), "typ": "JWT"})

	if error != nil {
		return error
	}

	now := time.Now().Unix()

	claims, error := json.Marshal(map[string]any{
		"aud": tokenURL,
		"exp": now + 60,
		"iat": now,
		"iss": clientID,
		"jti": uuid.NewString(),
		"sub": clientID,
	})

	if error != nil {
		return error
	}

	input := base64URL(header) + "." + base64URL(claims)

	digest := sha256.Sum256([]byte(input))

	signature, error := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])

	if error != nil {
		return error
	}

	form := url.Values{
		"client_assertion":      {input + "." + base64URL(signature)},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
		"client_id":             {clientID},
		"grant_type":            {"client_credentials"},
	}

	request, error := http.NewRequestWithContext(
		context, http.MethodPost, tokenURL, strings.NewReader(form.Encode()),
	)

	if error != nil {
		return error
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, error := tokenHTTPClient.Do(request)

	if error != nil {
		return error
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))

		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

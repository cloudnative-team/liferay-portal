package cx

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"

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

func (clientExtensionReconciler *ClientExtensionReconciler) ensureOAuth2PrivateKey(
	clientExtension *cxv1alpha1.ClientExtension,
	context context.Context,
) (*rsa.PrivateKey, error) {
	name := types.NamespacedName{Name: oAuth2PrivateKeySecretName(clientExtension), Namespace: clientExtension.Namespace}

	var secret corev1.Secret

	error := clientExtensionReconciler.APIReader.Get(context, name, &secret)

	if error == nil {
		return parseOAuth2PrivateKey(secret.Data[OAuth2PrivateKeySecretKey])
	}

	if !apierrors.IsNotFound(error) {
		return nil, error
	}

	privateKey, error := rsa.GenerateKey(rand.Reader, 2048)

	if error != nil {
		return nil, error
	}

	pkcs8, error := x509.MarshalPKCS8PrivateKey(privateKey)

	if error != nil {
		return nil, error
	}

	secret = corev1.Secret{
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

	if error := controllerutil.SetControllerReference(
		clientExtension, &secret, clientExtensionReconciler.Scheme(),
	); error != nil {
		return nil, error
	}

	if error := clientExtensionReconciler.Create(context, &secret); error != nil {
		if apierrors.IsAlreadyExists(error) {
			return clientExtensionReconciler.ensureOAuth2PrivateKey(clientExtension, context)
		}

		return nil, error
	}

	clientExtensionReconciler.Recorder.Eventf(
		clientExtension, corev1.EventTypeNormal, "OAuth2PrivateKeyCreated",
		"Created OAuth 2 private key %s in Secret %s", jwkThumbprint(&privateKey.PublicKey), name.Name,
	)

	return privateKey, nil
}

func injectPrivateKeyJWT(
	configs map[string]cxv1alpha1.Configuration,
	publicKey *rsa.PublicKey,
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

		properties["clientAuthenticationMethod"] = cxv1alpha1.ClientAuthenticationMethodPrivateKeyJWT
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

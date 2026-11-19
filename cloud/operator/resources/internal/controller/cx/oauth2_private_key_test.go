package cx

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestInjectPrivateKeyJWT(test *testing.T) {
	privateKey, error := rsa.GenerateKey(rand.Reader, 2048)

	if error != nil {
		test.Fatal(error)
	}

	headlessServerPID := pidPrefixOAuth2HeadlessServer + "cx-oahs"
	userAgentPID := "com.liferay.oauth2.provider.configuration.OAuth2ProviderApplicationUserAgentConfiguration~cx-oaua"

	configs := map[string]cxv1alpha1.Configuration{
		headlessServerPID: {JSON: apiextensionsv1.JSON{Raw: []byte(`{"scopes":["a"]}`)}},
		userAgentPID:      {JSON: apiextensionsv1.JSON{Raw: []byte(`{"scopes":["b"]}`)}},
	}

	injectedConfigs, error := injectPrivateKeyJWT(
		configs, &privateKey.PublicKey, map[string]string{"cx-oahs": "id-1"},
	)

	if error != nil {
		test.Fatal(error)
	}

	var headlessServer map[string]any

	if error := json.Unmarshal(injectedConfigs[headlessServerPID].Raw, &headlessServer); error != nil {
		test.Fatal(error)
	}

	if headlessServer["clientAuthenticationMethod"] != cxv1alpha1.ClientAuthenticationMethodPrivateKeyJWT {
		test.Errorf("clientAuthenticationMethod = %v", headlessServer["clientAuthenticationMethod"])
	}

	if headlessServer["clientId"] != "id-1" {
		test.Errorf("clientId = %v", headlessServer["clientId"])
	}

	if externalReferenceCodes := headlessServerExternalReferenceCodes(configs); !slices.Equal(
		externalReferenceCodes, []string{"cx-oahs"},
	) {
		test.Errorf("externalReferenceCodes = %v", externalReferenceCodes)
	}

	var jwks struct {
		Keys []jsonWebKey `json:"keys"`
	}

	if error := json.Unmarshal([]byte(headlessServer["jwks"].(string)), &jwks); error != nil {
		test.Fatal(error)
	}

	if (len(jwks.Keys) != 1) || (jwks.Keys[0].KeyID != jwkThumbprint(&privateKey.PublicKey)) {
		test.Errorf("jwks = %+v", jwks)
	}

	if string(injectedConfigs[userAgentPID].Raw) != `{"scopes":["b"]}` {
		test.Errorf("user agent configuration changed: %s", injectedConfigs[userAgentPID].Raw)
	}

	if string(configs[headlessServerPID].Raw) != `{"scopes":["a"]}` {
		test.Errorf("spec configuration changed: %s", configs[headlessServerPID].Raw)
	}
}

func TestJWKThumbprint(test *testing.T) {

	// RFC 7638 section 3.1

	modulus, error := base64.RawURLEncoding.DecodeString(
		"0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
	)

	if error != nil {
		test.Fatal(error)
	}

	publicKey := rsa.PublicKey{E: 65537, N: new(big.Int).SetBytes(modulus)}

	if thumbprint := jwkThumbprint(&publicKey); thumbprint != "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs" {
		test.Errorf("thumbprint = %s", thumbprint)
	}
}

func TestExtInitRequiredIdentifiers(test *testing.T) {
	clientExtension := cxv1alpha1.ClientExtension{
		Spec: cxv1alpha1.ClientExtensionSpec{
			Configs: map[string]cxv1alpha1.Configuration{
				pidPrefixOAuth2HeadlessServer + "cx-oahs": {JSON: apiextensionsv1.JSON{Raw: []byte(`{}`)}},
				"com.liferay.oauth2.provider.configuration.OAuth2ProviderApplicationUserAgentConfiguration~cx-oaua": {
					JSON: apiextensionsv1.JSON{Raw: []byte(`{}`)},
				},
			},
		},
	}

	if identifiers := extInitRequiredIdentifiers(&clientExtension); len(identifiers) != 2 {
		test.Errorf("client_secret_post: %v", identifiers)
	}

	clientExtension.Spec.OAuth2 = &cxv1alpha1.OAuth2{
		ClientAuthenticationMethod: cxv1alpha1.ClientAuthenticationMethodPrivateKeyJWT,
	}

	if identifiers := extInitRequiredIdentifiers(&clientExtension); len(identifiers) != 0 {
		test.Errorf("private_key_jwt: %v", identifiers)
	}
}

func TestRequestToken(test *testing.T) {
	privateKey, error := rsa.GenerateKey(rand.Reader, 2048)

	if error != nil {
		test.Fatal(error)
	}

	var tokenURL string

	server := httptest.NewServer(
		http.HandlerFunc(
			func(responseWriter http.ResponseWriter, request *http.Request) {
				if error := request.ParseForm(); error != nil {
					test.Error(error)
				}

				parts := strings.Split(request.PostForm.Get("client_assertion"), ".")

				digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))

				signature, _ := base64.RawURLEncoding.DecodeString(parts[2])

				var claims map[string]any

				payload, _ := base64.RawURLEncoding.DecodeString(parts[1])

				_ = json.Unmarshal(payload, &claims)

				if (rsa.VerifyPKCS1v15(&privateKey.PublicKey, crypto.SHA256, digest[:], signature) != nil) ||
					(claims["aud"] != tokenURL) || (claims["sub"] != "id-1") ||
					(request.PostForm.Get("client_id") != "id-1") {

					responseWriter.WriteHeader(http.StatusUnauthorized)

					return
				}

				_, _ = responseWriter.Write([]byte(`{"access_token":"x"}`))
			},
		),
	)

	defer server.Close()

	tokenURL = server.URL + "/o/oauth2/token"

	if error := requestToken(test.Context(), tokenURL, "id-1", privateKey); error != nil {
		test.Errorf("valid assertion: %v", error)
	}

	if error := requestToken(test.Context(), tokenURL, "id-2", privateKey); error == nil {
		test.Error("wrong client ID was accepted")
	}
}

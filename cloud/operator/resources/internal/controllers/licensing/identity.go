package licensing

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
)

const (
	identityPrivateKeyDataKey = "private.pem"
	identityPublicKeyDataKey  = "public.pem"
	identitySecretSuffix      = "-identity"
	privateKeySize            = 2048
)

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) createIdentityKey(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) (*rsa.PrivateKey, error) {
	privateKey, error := rsa.GenerateKey(rand.Reader, privateKeySize)

	if error != nil {
		return nil, error
	}

	secret, error := newIdentitySecret(liferayEnvironment, privateKey)

	if error != nil {
		return nil, error
	}

	if error := controllerruntime.SetControllerReference(
		liferayEnvironment,
		secret,
		liferayEnvironmentReconciler.Scheme(),
	); error != nil {
		return nil, error
	}

	if error := liferayEnvironmentReconciler.Create(context, secret); error != nil {
		return nil, error
	}

	return privateKey, nil
}

func encodePrivateKeyPEM(privateKey *rsa.PrivateKey) ([]byte, error) {
	privateKeyBytes, error := x509.MarshalPKCS8PrivateKey(privateKey)

	if error != nil {
		return nil, error
	}

	return pem.EncodeToMemory(
		&pem.Block{
			Bytes: privateKeyBytes,
			Type:  "PRIVATE KEY",
		},
	), nil
}

func encodePublicKeyBase64(privateKey *rsa.PrivateKey) (string, error) {
	publicKeyBytes, error := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)

	if error != nil {
		return "", error
	}

	return base64.StdEncoding.EncodeToString(publicKeyBytes), nil
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) getIdentityKey(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) (*rsa.PrivateKey, error) {
	secret := &corev1.Secret{}

	namespacedName := types.NamespacedName{
		Name:      resolveIdentitySecretName(liferayEnvironment),
		Namespace: liferayEnvironment.Namespace,
	}

	if error := liferayEnvironmentReconciler.Get(context, namespacedName, secret); error != nil {
		if errors.IsNotFound(error) {
			return nil, nil
		}

		return nil, error
	}

	return parsePrivateKeyPEM(secret.Data[identityPrivateKeyDataKey])
}

func (liferayEnvironmentReconciler *LiferayEnvironmentReconciler) getOrCreateIdentityKey(
	context context.Context,
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
) (*rsa.PrivateKey, error) {
	privateKey, error := liferayEnvironmentReconciler.getIdentityKey(context, liferayEnvironment)

	if error != nil {
		return nil, error
	}

	if privateKey != nil {
		return privateKey, nil
	}

	return liferayEnvironmentReconciler.createIdentityKey(context, liferayEnvironment)
}

func newIdentitySecret(
	liferayEnvironment *licensingv1alpha1.LiferayEnvironment,
	privateKey *rsa.PrivateKey,
) (*corev1.Secret, error) {
	privateKeyPEM, error := encodePrivateKeyPEM(privateKey)

	if error != nil {
		return nil, error
	}

	publicKeyBase64, error := encodePublicKeyBase64(privateKey)

	if error != nil {
		return nil, error
	}

	return &corev1.Secret{
		Data: map[string][]byte{
			identityPrivateKeyDataKey: privateKeyPEM,
			identityPublicKeyDataKey:  []byte(publicKeyBase64),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    map[string]string{"controller-watched": "yes"},
			Name:      resolveIdentitySecretName(liferayEnvironment),
			Namespace: liferayEnvironment.Namespace,
		},
	}, nil
}

func parsePrivateKeyPEM(privateKeyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyPEM)

	if block == nil {
		return nil, fmt.Errorf("identity secret: no PEM block in %s", identityPrivateKeyDataKey)
	}

	parsedKey, error := x509.ParsePKCS8PrivateKey(block.Bytes)

	if error != nil {
		return nil, error
	}

	privateKey, ok := parsedKey.(*rsa.PrivateKey)

	if !ok {
		return nil, fmt.Errorf("identity secret: not an RSA private key")
	}

	return privateKey, nil
}

func resolveIdentitySecretName(liferayEnvironment *licensingv1alpha1.LiferayEnvironment) string {
	return liferayEnvironment.Name + identitySecretSuffix
}

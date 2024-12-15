package tools

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"time"
)

// Generates a self-signed certificate
func EnsureCertificate(certPath, keyPath string) error {
	// Validate the certificate and key files exist
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)

	// If both files exist, check the certificate's validity
	if certErr == nil && keyErr == nil {
		certData, err := os.ReadFile(certPath)
		if err != nil {
			return err
		}
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return err
		}

		cert, err := tls.X509KeyPair(certData, keyData)
		if err != nil {
			return err
		}

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return err
		}

		// Check if the certificate is still valid
		now := time.Now()
		if now.After(x509Cert.NotBefore) && now.Before(x509Cert.NotAfter) {
			// Certificate is valid, no need to regenerate
			return nil
		}
	}

	// Either the certificate/key files don't exist, or the certificate is invalid; generate a new one
	return generateSelfSignedCertificate(certPath, keyPath)
}

func generateSelfSignedCertificate(certPath, keyPath string) error {
	// Generate a private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Create a certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Ztkent"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create a self-signed certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}

	// Encode and save the private key
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyFile.Close()

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	if err := pem.Encode(keyFile, privateKeyPEM); err != nil {
		return err
	}

	// Encode and save the certificate
	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()

	certPEM := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	if err := pem.Encode(certFile, certPEM); err != nil {
		return err
	}

	return nil
}

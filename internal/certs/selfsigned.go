// Package certs generates self-signed ECDSA P-256 certificates for
// WebTransport, which requires certificates with at most 14-day validity.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"time"
)

const maxValidity = 14 * 24 * time.Hour // WebTransport requires ≤14 days

// CertInfo holds a TLS certificate and its SHA-256 fingerprint.
type CertInfo struct {
	TLSCert     tls.Certificate
	Fingerprint [32]byte
	NotAfter    time.Time
}

// FingerprintBase64 returns the SHA-256 fingerprint as base64.
func (c *CertInfo) FingerprintBase64() string {
	return base64.StdEncoding.EncodeToString(c.Fingerprint[:])
}

// Generate creates a new self-signed ECDSA P-256 certificate valid for the
// given duration (capped at 14 days for WebTransport compatibility).
func Generate(validity time.Duration) (*CertInfo, error) {
	if validity > maxValidity || validity <= 0 {
		validity = maxValidity
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}

	now := time.Now()
	notBefore := now.Add(-1 * time.Minute) // slight backdate for clock skew
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "prism"},
		NotBefore:    notBefore,
		NotAfter:     notBefore.Add(validity), // total validity must be ≤14 days (Chrome enforces strictly)
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	fingerprint := sha256.Sum256(certDER)

	tlsCert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	return &CertInfo{
		TLSCert:     tlsCert,
		Fingerprint: fingerprint,
		NotAfter:    template.NotAfter,
	}, nil
}

package certs

import (
	"crypto/sha256"
	"crypto/x509"
	"testing"
	"time"
)

func TestGenerate(t *testing.T) {
	t.Parallel()
	cert, err := Generate(14 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify TLS certificate exists
	if len(cert.TLSCert.Certificate) == 0 {
		t.Fatal("no certificate data")
	}

	// Parse the cert
	x509Cert, err := x509.ParseCertificate(cert.TLSCert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse cert: %v", err)
	}

	// Verify validity period
	validity := x509Cert.NotAfter.Sub(x509Cert.NotBefore)
	if validity > 14*24*time.Hour+2*time.Minute {
		t.Errorf("validity too long: %v", validity)
	}

	// Verify not expired
	if x509Cert.NotAfter.Before(time.Now()) {
		t.Error("cert is already expired")
	}

	// Verify fingerprint matches
	expectedFingerprint := sha256.Sum256(cert.TLSCert.Certificate[0])
	if cert.Fingerprint != expectedFingerprint {
		t.Error("fingerprint mismatch")
	}

	// Verify FingerprintBase64 is non-empty
	fp := cert.FingerprintBase64()
	if fp == "" {
		t.Error("FingerprintBase64 returned empty string")
	}

	// Verify DNS name
	found := false
	for _, name := range x509Cert.DNSNames {
		if name == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected localhost in DNS names")
	}
}

func TestGenerateMaxValidity(t *testing.T) {
	t.Parallel()
	// Requesting more than 14 days should cap at 14 days
	cert, err := Generate(30 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	x509Cert, err := x509.ParseCertificate(cert.TLSCert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse cert: %v", err)
	}

	validity := x509Cert.NotAfter.Sub(x509Cert.NotBefore)
	if validity > 14*24*time.Hour+2*time.Minute {
		t.Errorf("validity should be capped at 14 days, got: %v", validity)
	}
}

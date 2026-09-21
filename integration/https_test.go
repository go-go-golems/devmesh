package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// selfSignedCert writes a short-lived self-signed certificate for
// api-checkout.test and 127.0.0.1 and returns the cert/key file paths.
func selfSignedCert(t *testing.T) (string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "devmesh-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"api-checkout.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func TestHTTPSProxyWithExistingCertificate(t *testing.T) {
	certFile, keyFile := selfSignedCert(t)
	h := startHarnessTLS(t, 5*time.Second, certFile, keyFile)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "TLS")
	}))
	defer backend.Close()
	reg := registerHTTP(t, h, "checkout.api", "api-checkout.test", strings.TrimPrefix(backend.URL, "http://"))
	if want := "https://api-checkout.test:" + strings.TrimPrefix(h.httpsAddr, "127.0.0.1:"); reg.Frontend.URL != want {
		t.Fatalf("advertised HTTPS URL = %q, want %q", reg.Frontend.URL, want)
	}

	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // self-signed test certificate
	}
	deadline := time.Now().Add(5 * time.Second)
	var body string
	var status int
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", "https://"+h.httpsAddr+"/", nil)
		req.Host = "api-checkout.test"
		resp, err := client.Do(req)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			status, body = resp.StatusCode, string(b)
			if status == 200 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != 200 || body != "TLS" {
		t.Fatalf("https request: status=%d body=%q, want 200/TLS", status, body)
	}
}

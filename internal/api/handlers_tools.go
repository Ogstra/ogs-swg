package api

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RealityKeysResponse struct {
	PrivateKey string   `json:"private_key"`
	PublicKey  string   `json:"public_key"`
	ShortId    []string `json:"short_id"`
}

type SelfSignedCertRequest struct {
	Tag        string `json:"tag"`
	CommonName string `json:"common_name"`
}

type SelfSignedCertResponse struct {
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

type RandBase64Request struct {
	KeyLength int `json:"key_length"`
}

type RandBase64Response struct {
	Value string `json:"value"`
}

func (s *Server) handleGenerateRealityKeys(w http.ResponseWriter, r *http.Request) {
	// Generate X25519 Key Pair
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate private key: "+err.Error())
		return
	}

	publicKey := privateKey.PublicKey()

	// Encode to Base64 (RawURL Encoding for Sing-box/Xray)
	privStr := base64.RawURLEncoding.EncodeToString(privateKey.Bytes())
	pubStr := base64.RawURLEncoding.EncodeToString(publicKey.Bytes())

	// Generate ShortID (8 bytes hex is common, 16 chars)
	// Sing-box short_id is list of hex strings.
	shortIdBytes := make([]byte, 8)
	if _, err := rand.Read(shortIdBytes); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate short_id: "+err.Error())
		return
	}
	shortIdStr := hex.EncodeToString(shortIdBytes)

	resp := RealityKeysResponse{
		PrivateKey: privStr,
		PublicKey:  pubStr,
		ShortId:    []string{shortIdStr},
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGenerateSelfSignedCert(w http.ResponseWriter, r *http.Request) {
	var req SelfSignedCertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	commonName := strings.TrimSpace(req.CommonName)
	if commonName == "" {
		commonName = "localhost"
	}

	baseDir := filepath.Dir(s.config.SingboxConfigPath)
	if baseDir == "" {
		baseDir = "."
	}
	certDir := filepath.Join(baseDir, "certs")
	if err := os.MkdirAll(certDir, 0700); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to create cert directory: "+err.Error())
		return
	}

	tag := sanitizeFileToken(req.Tag)
	if tag == "" {
		tag = "inbound"
	}
	ts := time.Now().UTC().Format("20060102-150405")
	certPath := filepath.Join(certDir, "selfsigned_"+tag+"_"+ts+".crt")
	keyPath := filepath.Join(certDir, "selfsigned_"+tag+"_"+ts+".key")

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate private key: "+err.Error())
		return
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate serial: "+err.Error())
		return
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(825 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(commonName); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{commonName}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to create certificate: "+err.Error())
		return
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to write certificate: "+err.Error())
		return
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to encode certificate: "+err.Error())
		return
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to write key: "+err.Error())
		return
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)}); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to encode key: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, SelfSignedCertResponse{CertPath: certPath, KeyPath: keyPath})
}

func (s *Server) handleGenerateRandBase64(w http.ResponseWriter, r *http.Request) {
	perms := getPermissions(r)
	if perms == nil || (!perms.CanWriteConfig && !perms.CanWriteUsers) {
		writeErr(w, http.StatusForbidden, "Forbidden")
		return
	}

	var req RandBase64Request
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	if req.KeyLength <= 0 {
		writeErr(w, http.StatusBadRequest, "key_length must be a positive integer")
		return
	}

	buf := make([]byte, req.KeyLength)
	if _, err := rand.Read(buf); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate random base64: "+err.Error())
		return
	}
	value := base64.StdEncoding.EncodeToString(buf)

	writeJSON(w, http.StatusOK, RandBase64Response{Value: value})
}

func sanitizeFileToken(input string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(input) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

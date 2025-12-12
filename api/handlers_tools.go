package api

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type RealityKeysResponse struct {
	PrivateKey string   `json:"private_key"`
	PublicKey  string   `json:"public_key"`
	ShortId    []string `json:"short_id"`
}

func (s *Server) handleGenerateRealityKeys(w http.ResponseWriter, r *http.Request) {
	// Generate X25519 Key Pair
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, "Failed to generate private key: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to generate short_id: "+err.Error(), http.StatusInternalServerError)
		return
	}
	shortIdStr := hex.EncodeToString(shortIdBytes)

	resp := RealityKeysResponse{
		PrivateKey: privStr,
		PublicKey:  pubStr,
		ShortId:    []string{shortIdStr},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

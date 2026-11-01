package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

const (
	totpIssuer         = "OGS-SWG"
	recoveryCodeCount  = 10
	recoveryCodeLength = 10
)

// otpRequiredHeader tells the login page to ask for a second factor.
const otpRequiredHeader = "X-OTP-Required"

func hashRecoveryCode(code string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func newRecoveryCodes() (codes []string, hashes []string, err error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, recoveryCodeLength)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		for j := range raw {
			raw[j] = alphabet[int(raw[j])%len(alphabet)]
		}
		code := string(raw[:5]) + "-" + string(raw[5:])
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// checkSecondFactor enforces TOTP for users that enabled it. It writes the
// response and returns ok=false when login must stop; used reports that a
// second factor was checked.
func (s *Server) checkSecondFactor(w http.ResponseWriter, username, otp, userKey, ipKey string) (ok bool, used bool) {
	state, err := s.store.GetPanelUserTOTP(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Authentication error")
		return false, false
	}
	if !state.Enabled {
		return true, false
	}
	if strings.TrimSpace(otp) == "" {
		w.Header().Set(otpRequiredHeader, "true")
		writeErr(w, http.StatusUnauthorized, "Two-factor code required")
		return false, false
	}

	accepted := false
	if step, ok := core.VerifyTOTP(state.Secret, otp, time.Now()); ok {
		fresh, err := s.store.ConsumePanelUserTOTPStep(username, step)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Authentication error")
			return false, false
		}
		accepted = fresh
	} else if strings.Contains(otp, "-") {
		used, err := s.store.ConsumePanelUserRecoveryCode(username, hashRecoveryCode(otp))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Authentication error")
			return false, false
		}
		accepted = used
	}
	if !accepted {
		s.loginLimiter.recordFailure(userKey)
		s.loginLimiter.recordFailure(ipKey)
		w.Header().Set(otpRequiredHeader, "true")
		writeErr(w, http.StatusUnauthorized, "Invalid two-factor code")
		return false, false
	}
	return true, true
}

type twoFactorStatusResponse struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleTwoFactorStatus(w http.ResponseWriter, r *http.Request) {
	username, ok := currentPanelUsername(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	state, err := s.store.GetPanelUserTOTP(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to read two-factor state")
		return
	}
	writeJSON(w, http.StatusOK, twoFactorStatusResponse{Enabled: state.Enabled})
}

type twoFactorSetupResponse struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// handleTwoFactorSetup creates a pending secret for the caller. It is not
// enforced until handleTwoFactorEnable sees a valid code from it.
func (s *Server) handleTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	username, ok := currentPanelUsername(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	state, err := s.store.GetPanelUserTOTP(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to read two-factor state")
		return
	}
	if state.Enabled {
		writeErr(w, http.StatusConflict, "Two-factor authentication is already enabled")
		return
	}
	secret, err := core.GenerateTOTPSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate secret")
		return
	}
	if err := s.store.SetPanelUserTOTPPending(username, secret); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to store secret")
		return
	}
	writeJSON(w, http.StatusOK, twoFactorSetupResponse{
		Secret: secret,
		URI:    core.TOTPProvisioningURI(totpIssuer, username, secret),
	})
}

type twoFactorCodeRequest struct {
	Code string `json:"code"`
}

type twoFactorEnableResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (s *Server) handleTwoFactorEnable(w http.ResponseWriter, r *http.Request) {
	username, ok := currentPanelUsername(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req twoFactorCodeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	state, err := s.store.GetPanelUserTOTP(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to read two-factor state")
		return
	}
	if state.Enabled || state.Secret == "" {
		writeErr(w, http.StatusConflict, "No pending two-factor setup")
		return
	}
	step, valid := core.VerifyTOTP(state.Secret, req.Code, time.Now())
	if !valid {
		writeErr(w, http.StatusBadRequest, "Invalid code")
		return
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate recovery codes")
		return
	}
	if err := s.store.EnablePanelUserTOTP(username, hashes, step); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to enable two-factor authentication")
		return
	}
	s.cache.Del(cacheKeyAllPanelUsers)
	s.insertAuditEntry(r, "auth", "2fa_enable", username, "")
	writeJSON(w, http.StatusOK, twoFactorEnableResponse{RecoveryCodes: codes})
}

type twoFactorDisableRequest struct {
	CurrentPassword string `json:"current_password"`
	Code            string `json:"code"`
}

// handleTwoFactorDisable needs both the password and a current code, so a
// stolen session alone cannot turn the second factor off.
func (s *Server) handleTwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	username, ok := currentPanelUsername(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var req twoFactorDisableRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	perms, err := s.store.VerifyPanelUser(username, req.CurrentPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Verification error")
		return
	}
	if perms == nil {
		// 403, not 401: the client treats 401 as an expired session and signs out.
		writeErr(w, http.StatusForbidden, "Invalid current password")
		return
	}
	state, err := s.store.GetPanelUserTOTP(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to read two-factor state")
		return
	}
	if !state.Enabled {
		writeErr(w, http.StatusConflict, "Two-factor authentication is not enabled")
		return
	}
	step, valid := core.VerifyTOTP(state.Secret, req.Code, time.Now())
	if !valid {
		writeErr(w, http.StatusBadRequest, "Invalid code")
		return
	}
	if fresh, err := s.store.ConsumePanelUserTOTPStep(username, step); err != nil || !fresh {
		writeErr(w, http.StatusBadRequest, "Invalid code")
		return
	}
	if err := s.store.DisablePanelUserTOTP(username); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to disable two-factor authentication")
		return
	}
	s.cache.Del(cacheKeyAllPanelUsers)
	s.insertAuditEntry(r, "auth", "2fa_disable", username, "")
	w.WriteHeader(http.StatusNoContent)
}

// handleResetPanelUserTwoFactor lets an admin clear another user's second
// factor, which is the way back in after a lost device and lost recovery codes.
func (s *Server) handleResetPanelUserTwoFactor(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		writeErr(w, http.StatusBadRequest, "username is required")
		return
	}
	if err := s.store.DisablePanelUserTOTP(username); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to reset two-factor authentication")
		return
	}
	s.cache.Del(cacheKeyAllPanelUsers)
	w.WriteHeader(http.StatusNoContent)
}

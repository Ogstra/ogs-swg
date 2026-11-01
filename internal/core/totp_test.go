package core

import (
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA1 secret "12345678901234567890", truncated to 6 digits.
func TestVerifyTOTP_RFC6238Vectors(t *testing.T) {
	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))
	for _, tc := range []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	} {
		step, ok := VerifyTOTP(secret, tc.code, time.Unix(tc.unix, 0))
		if !ok || step != tc.unix/30 {
			t.Fatalf("t=%d code=%s: ok=%v step=%d", tc.unix, tc.code, ok, step)
		}
	}
}

func TestVerifyTOTP_RejectsWrongAndStaleCodes(t *testing.T) {
	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))
	now := time.Unix(1111111109, 0)
	if _, ok := VerifyTOTP(secret, "000000", now); ok {
		t.Fatal("accepted a wrong code")
	}
	if _, ok := VerifyTOTP(secret, "081804", now.Add(5*time.Minute)); ok {
		t.Fatal("accepted a code outside the skew window")
	}
	if _, ok := VerifyTOTP(secret, "08180", now); ok {
		t.Fatal("accepted a short code")
	}
	if _, ok := VerifyTOTP(secret, "081 804", now); !ok {
		t.Fatal("rejected a code with a space")
	}
}

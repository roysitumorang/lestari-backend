package middlewares

import "testing"

// RFC 6238 appendix B (SHA1 seed "12345678901234567890"), truncated to 6 digits.
func TestTOTPVectors(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // base32 of the RFC seed
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		got, err := totpAt(secret, unix/30)
		if err != nil || got != want {
			t.Errorf("T=%d: got %s (%v), want %s", unix, got, err, want)
		}
	}
}

func TestRandomDigits(t *testing.T) {
	s := RandomDigits(32)
	if len(s) != 32 {
		t.Fatalf("length %d", len(s))
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("non-digit in %q", s)
		}
	}
}

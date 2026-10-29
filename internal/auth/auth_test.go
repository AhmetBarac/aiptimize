package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$600000$") {
		t.Fatalf("unexpected hash format: %q", hash)
	}
	ok, err := VerifyPassword(hash, "correct horse battery")
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(valid) = %v, %v", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong password")
	if err != nil || ok {
		t.Fatalf("VerifyPassword(invalid) = %v, %v", ok, err)
	}
}

func TestHashIsSalted(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestHashRejectsShortPassword(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	cases := []string{"", "plain", "pbkdf2-sha256$abc$x$y", "bcrypt$1$x$y", "pbkdf2-sha256$600000$!!!$!!!"}
	for _, c := range cases {
		if _, err := VerifyPassword(c, "whatever"); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestTokens(t *testing.T) {
	raw, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || hash == "" || raw == hash {
		t.Fatalf("bad token pair: %q %q", raw, hash)
	}
	if HashToken(raw) != hash {
		t.Fatal("token hash mismatch")
	}
	raw2, _, _ := NewToken()
	if raw2 == raw {
		t.Fatal("tokens must be unique")
	}
	csrf, err := NewCSRFToken()
	if err != nil || len(csrf) < 32 {
		t.Fatalf("csrf token: %q, %v", csrf, err)
	}
}

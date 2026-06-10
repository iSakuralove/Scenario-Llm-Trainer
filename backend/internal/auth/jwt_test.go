package auth

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestJWTSignAndValidate(t *testing.T) {
	manager := NewManager("unit-secret", time.Minute)
	token, err := manager.SignWithVersion("user-1", "student", "access", time.Minute, 3)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.Role != "student" || claims.Type != "access" || claims.TokenVersion != 3 {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestPasswordHash(t *testing.T) {
	withMinBcryptCost(t)

	hash := HashPassword("demo123")
	if hash == "" || !CheckPassword("demo123", hash) || CheckPassword("wrong", hash) {
		t.Fatal("password hash check failed")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}
	if hash == legacyHashPassword("demo123") {
		t.Fatal("new password hash should not use legacy SHA-256 format")
	}
}

func TestCheckPasswordAcceptsLegacySHA256Hash(t *testing.T) {
	hash := legacyHashPassword("demo123")
	if !CheckPassword("demo123", hash) {
		t.Fatal("legacy SHA-256 password hash should remain valid during bcrypt migration")
	}
	if CheckPassword("wrong", hash) {
		t.Fatal("wrong password should not match legacy SHA-256 hash")
	}
}

func TestValidatePassword(t *testing.T) {
	password, err := ValidatePassword("  demo123  ")
	if err != nil {
		t.Fatalf("expected password to validate, got %v", err)
	}
	if password != "demo123" {
		t.Fatalf("expected trimmed password, got %q", password)
	}
	for _, candidate := range []string{"", "   ", "12345"} {
		if _, err := ValidatePassword(candidate); err == nil {
			t.Fatalf("expected short password %q to be rejected", candidate)
		}
	}
}

func withMinBcryptCost(t *testing.T) {
	t.Helper()
	old := bcryptCost
	bcryptCost = bcrypt.MinCost
	t.Cleanup(func() {
		bcryptCost = old
	})
}

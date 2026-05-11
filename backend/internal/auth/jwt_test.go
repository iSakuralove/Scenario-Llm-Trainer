package auth

import (
	"testing"
	"time"
)

func TestJWTSignAndValidate(t *testing.T) {
	manager := NewManager("unit-secret", time.Minute)
	token, err := manager.Sign("user-1", "student", "access", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.Role != "student" || claims.Type != "access" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestPasswordHash(t *testing.T) {
	hash := HashPassword("demo123")
	if hash == "" || !CheckPassword("demo123", hash) || CheckPassword("wrong", hash) {
		t.Fatal("password hash check failed")
	}
}

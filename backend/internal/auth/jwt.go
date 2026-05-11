package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"
)

type Manager struct {
	secret []byte
	ttl    time.Duration
}

type Claims struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
	Exp     int64  `json:"exp"`
	Type    string `json:"typ"`
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

func HashPassword(password string) string {
	sum := sha256.Sum256([]byte("mvp-salt:" + password))
	return hex.EncodeToString(sum[:])
}

func CheckPassword(password, hash string) bool {
	return HashPassword(password) == hash
}

func (m *Manager) IssuePair(user *domain.User) (string, string, error) {
	access, err := m.Sign(user.ID, user.Role, "access", m.ttl)
	if err != nil {
		return "", "", err
	}
	refresh, err := m.Sign(user.ID, user.Role, "refresh", 7*24*time.Hour)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (m *Manager) Sign(subject, role, tokenType string, ttl time.Duration) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := Claims{
		Subject: subject,
		Role:    role,
		Exp:     time.Now().Add(ttl).Unix(),
		Type:    tokenType,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := b64(headerJSON) + "." + b64(claimsJSON)
	return unsigned + "." + m.signature(unsigned), nil
}

func (m *Manager) Validate(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token")
	}
	unsigned := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(m.signature(unsigned)), []byte(parts[2])) {
		return Claims{}, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, err
	}
	if claims.Exp < time.Now().Unix() {
		return Claims{}, errors.New("token expired")
	}
	return claims, nil
}

func (m *Manager) signature(unsigned string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func b64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func BearerToken(header string) (string, error) {
	if header == "" {
		return "", fmt.Errorf("missing authorization")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("invalid authorization")
	}
	return strings.TrimSpace(parts[1]), nil
}

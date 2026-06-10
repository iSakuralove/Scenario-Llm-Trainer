package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"situational-teaching/backend/internal/domain"

	"golang.org/x/crypto/bcrypt"
)

type Manager struct {
	secret []byte
	ttl    time.Duration
}

type Claims struct {
	Subject      string `json:"sub"`
	Role         string `json:"role"`
	Exp          int64  `json:"exp"`
	Type         string `json:"typ"`
	TokenVersion int    `json:"ver,omitempty"`
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

// legacyHashPassword reproduces the original SHA-256 scheme so that
// credentials stored before the bcrypt migration can still be verified.
func legacyHashPassword(password string) string {
	sum := sha256.Sum256([]byte("mvp-salt:" + password))
	return hex.EncodeToString(sum[:])
}

// bcryptCost controls the work factor used by HashPassword. It defaults to
// bcrypt.DefaultCost but can be lowered via BCRYPT_COST (e.g. in test/CI) to
// keep large suites fast. Values outside bcrypt's allowed range fall back to
// the default.
var bcryptCost = resolveBcryptCost()

func resolveBcryptCost() int {
	if raw := strings.TrimSpace(os.Getenv("BCRYPT_COST")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= bcrypt.MinCost && n <= bcrypt.MaxCost {
			return n
		}
	}
	return bcrypt.DefaultCost
}

// HashPassword hashes a plaintext password with bcrypt. If bcrypt is somehow
// unavailable it falls back to the legacy scheme so callers always get a
// non-empty hash; CheckPassword understands both formats.
func HashPassword(password string) string {
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return legacyHashPassword(password)
	}
	return string(digest)
}

func ValidatePassword(password string) (string, error) {
	normalized := strings.TrimSpace(password)
	if len([]rune(normalized)) < 6 {
		return "", errors.New("密码至少需要 6 位")
	}
	return normalized, nil
}

func IsLegacyPasswordHash(hash string) bool {
	return !(strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$"))
}

// CheckPassword verifies a plaintext password against either a bcrypt hash or
// a legacy SHA-256 hash, allowing accounts created under the old scheme to
// keep working until their hash is rewritten on next password change.
func CheckPassword(password, hash string) bool {
	if !IsLegacyPasswordHash(hash) {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return hmac.Equal([]byte(legacyHashPassword(password)), []byte(hash))
}

func (m *Manager) IssuePair(user *domain.User) (string, string, error) {
	access, err := m.SignWithVersion(user.ID, user.Role, "access", m.ttl, user.TokenVersion)
	if err != nil {
		return "", "", err
	}
	refresh, err := m.SignWithVersion(user.ID, user.Role, "refresh", 7*24*time.Hour, user.TokenVersion)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (m *Manager) Sign(subject, role, tokenType string, ttl time.Duration) (string, error) {
	return m.SignWithVersion(subject, role, tokenType, ttl, 0)
}

func (m *Manager) SignWithVersion(subject, role, tokenType string, ttl time.Duration, tokenVersion int) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := Claims{
		Subject:      subject,
		Role:         role,
		Exp:          time.Now().Add(ttl).Unix(),
		Type:         tokenType,
		TokenVersion: tokenVersion,
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

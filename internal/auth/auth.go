package auth

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Claims holds the JWT payload fields.
type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// HashPassword returns a bcrypt hash of the given plain-text password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether the plain-text password matches the stored hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken creates a signed JWT for the given user.
// ttl is the token lifetime in seconds; if 0, falls back to SESSION_TTL env var (default 86400).
func GenerateToken(userID, role string, ttl int) (string, error) {
	secret := jwtSecret()
	if secret == "" {
		return "", errors.New("SESSION_SECRET is not set")
	}
	if ttl == 0 {
		ttl = sessionTTL()
	}
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(ttl) * time.Second)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ValidateToken parses and validates a JWT, returning its Claims.
func ValidateToken(tokenStr string) (*Claims, error) {
	secret := jwtSecret()
	if secret == "" {
		return nil, errors.New("SESSION_SECRET is not set")
	}
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func jwtSecret() string {
	return os.Getenv("SESSION_SECRET")
}

func sessionTTL() int {
	s := os.Getenv("SESSION_TTL")
	if s == "" {
		return 86400
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 86400
	}
	return n
}

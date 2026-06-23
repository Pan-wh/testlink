package auth

import (
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Service struct {
	jwtSecret  []byte
	adminToken string
}

func New(jwtSecret, adminToken string) *Service {
	return &Service{jwtSecret: []byte(jwtSecret), adminToken: adminToken}
}

// Login validates credentials and returns a JWT.
// For simplicity, use static admin token as password (internal tool).
func (s *Service) Login(token string) (string, error) {
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) != 1 {
		return "", fmt.Errorf("invalid token")
	}
	claims := jwt.MapClaims{
		"sub": "admin",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return jwtToken.SignedString(s.jwtSecret)
}

// Validate checks a JWT token.
func (s *Service) Validate(tokenString string) (*jwt.Token, error) {
	return jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
}

// ValidateToken checks a raw token against admin_token (for simple header auth).
func (s *Service) ValidateToken(raw string) bool {
	return subtle.ConstantTimeCompare([]byte(raw), []byte(s.adminToken)) == 1
}

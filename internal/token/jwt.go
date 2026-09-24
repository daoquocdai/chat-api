package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrSecretRequired  = errors.New("JWT secret is required")
	ErrInvalidLifetime = errors.New("JWT lifetime must be at least one second")
	ErrUserIDRequired  = errors.New("user ID is required")
	ErrInvalidToken    = errors.New("invalid token")
	ErrExpiredToken    = errors.New("token has expired")
)

type header struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type claims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type JWT struct {
	secret   []byte
	lifetime time.Duration
}

func NewJWT(secret string, lifetime time.Duration) (*JWT, error) {
	if secret == "" {
		return nil, ErrSecretRequired
	}
	if lifetime < time.Second {
		return nil, ErrInvalidLifetime
	}

	return &JWT{
		secret:   []byte(secret),
		lifetime: lifetime,
	}, nil
}

func (j *JWT) Create(userID string) (string, error) {
	if userID == "" {
		return "", ErrUserIDRequired
	}

	now := time.Now().UTC()
	headerJSON, err := json.Marshal(header{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims{
		Subject:   userID,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(j.lifetime).Unix(),
	})
	if err != nil {
		return "", err
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsignedToken := encodedHeader + "." + encodedClaims

	encodedSignature := base64.RawURLEncoding.EncodeToString(j.sign(unsignedToken))
	return unsignedToken + "." + encodedSignature, nil
}

func (j *JWT) Verify(value string) (string, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return "", ErrInvalidToken
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrInvalidToken
	}
	var tokenHeader header
	if err := json.Unmarshal(headerJSON, &tokenHeader); err != nil ||
		tokenHeader.Algorithm != "HS256" || tokenHeader.Type != "JWT" {
		return "", ErrInvalidToken
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", ErrInvalidToken
	}
	unsignedToken := parts[0] + "." + parts[1]
	if !hmac.Equal(signature, j.sign(unsignedToken)) {
		return "", ErrInvalidToken
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidToken
	}
	var tokenClaims claims
	if err := json.Unmarshal(claimsJSON, &tokenClaims); err != nil ||
		tokenClaims.Subject == "" || tokenClaims.IssuedAt <= 0 ||
		tokenClaims.ExpiresAt <= tokenClaims.IssuedAt {
		return "", ErrInvalidToken
	}
	if tokenClaims.ExpiresAt <= time.Now().UTC().Unix() {
		return "", ErrExpiredToken
	}

	return tokenClaims.Subject, nil
}

func (j *JWT) sign(value string) []byte {
	mac := hmac.New(sha256.New, j.secret)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

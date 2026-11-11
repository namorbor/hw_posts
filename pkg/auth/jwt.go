package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid or expired token")

const issuer = "blog-api"

type claims struct {
	UserID int `json:"user_id"`
	jwt.RegisteredClaims
}

type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTManager(secret string, ttl time.Duration) (*JWTManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT secret must contain at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("JWT lifetime must be positive")
	}
	return &JWTManager{secret: []byte(secret), ttl: ttl}, nil
}

func (m *JWTManager) GenerateToken(userID int) (string, time.Time, error) {
	if userID <= 0 {
		return "", time.Time{}, errors.New("user ID must be positive")
	}
	now := time.Now().UTC()
	expiresAt := now.Add(m.ttl).Truncate(time.Second)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.Itoa(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})
	signed, err := token.SignedString(m.secret)
	return signed, expiresAt, err
}

func (m *JWTManager) ValidateToken(tokenString string) (int, error) {
	c := &claims{}
	token, err := jwt.ParseWithClaims(tokenString, c, func(token *jwt.Token) (interface{}, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(), jwt.WithIssuer(issuer), jwt.WithIssuedAt())
	if err != nil || !token.Valid || c.UserID <= 0 || c.Subject != strconv.Itoa(c.UserID) || c.IssuedAt == nil {
		return 0, ErrInvalidToken
	}
	return c.UserID, nil
}

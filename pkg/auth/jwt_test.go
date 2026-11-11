package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestTokenRoundTripAndTampering(t *testing.T) {
	m, err := NewJWTManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, expiresAt, err := m.GenerateToken(42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.ValidateToken(token)
	if err != nil || id != 42 || time.Until(expiresAt) < 59*time.Minute {
		t.Fatalf("invalid round trip: id=%d expiry=%s err=%v", id, expiresAt, err)
	}
	parts := strings.Split(token, ".")
	if parts[2][0] == 'A' {
		parts[2] = "B" + parts[2][1:]
	} else {
		parts[2] = "A" + parts[2][1:]
	}
	if _, err := m.ValidateToken(strings.Join(parts, ".")); err == nil {
		t.Fatal("accepted altered signature")
	}
	other, _ := NewJWTManager(strings.Repeat("z", 32), time.Hour)
	if _, err := other.ValidateToken(token); err == nil {
		t.Fatal("accepted token signed with a different key")
	}
}

func TestTokenValidation(t *testing.T) {
	m, _ := NewJWTManager(testSecret, time.Hour)
	for _, name := range []string{"expired", "missing expiry", "future issued at", "future not before", "missing issued at", "wrong issuer", "invalid user", "wrong subject", "wrong algorithm"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			c := claims{UserID: 42, RegisteredClaims: jwt.RegisteredClaims{
				Issuer: issuer, Subject: "42", IssuedAt: jwt.NewNumericDate(now),
				NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			}}
			var method jwt.SigningMethod = jwt.SigningMethodHS256
			switch name {
			case "expired":
				c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
			case "missing expiry":
				c.ExpiresAt = nil
			case "future issued at":
				c.IssuedAt = jwt.NewNumericDate(now.Add(time.Hour))
			case "future not before":
				c.NotBefore = jwt.NewNumericDate(now.Add(time.Hour))
			case "missing issued at":
				c.IssuedAt = nil
			case "wrong issuer":
				c.Issuer = "another-app"
			case "invalid user":
				c.UserID = 0
			case "wrong subject":
				c.Subject = "43"
			case "wrong algorithm":
				method = jwt.SigningMethodHS384
			}
			token, err := jwt.NewWithClaims(method, c).SignedString([]byte(testSecret))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.ValidateToken(token); err == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
	for _, token := range []string{"", "garbage", "a.b.c"} {
		if _, err := m.ValidateToken(token); err == nil {
			t.Fatal("accepted malformed token")
		}
	}
}

func TestTokenConfiguration(t *testing.T) {
	if _, err := NewJWTManager("short", time.Hour); err == nil {
		t.Fatal("accepted short secret")
	}
	if _, err := NewJWTManager(testSecret, 0); err == nil {
		t.Fatal("accepted zero lifetime")
	}
	m, _ := NewJWTManager(testSecret, time.Hour)
	if _, _, err := m.GenerateToken(0); err == nil {
		t.Fatal("accepted zero user ID")
	}
}

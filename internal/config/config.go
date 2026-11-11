package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port      string
	DataDir   string
	LogFile   string
	JWTSecret string
	JWTTTL    time.Duration
}

func Load() (Config, error) {
	c := Config{
		Port:      env("PORT", "8080"),
		DataDir:   env("DATA_DIR", "data"),
		LogFile:   env("LOG_FILE", "log.txt"),
		JWTSecret: os.Getenv("JWT_SECRET"),
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be between 1 and 65535")
	}
	hours, err := strconv.Atoi(env("JWT_TTL_HOURS", "24"))
	if err != nil || hours < 1 || hours > 720 {
		return Config{}, fmt.Errorf("JWT_TTL_HOURS must be between 1 and 720")
	}
	c.JWTTTL = time.Duration(hours) * time.Hour
	if c.JWTSecret == "" {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return Config{}, fmt.Errorf("generate JWT secret: %w", err)
		}
		c.JWTSecret = hex.EncodeToString(key[:])
		log.Print("JWT_SECRET is empty: using a temporary key; tokens will expire after restart")
	}
	if len(c.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must contain at least 32 bytes")
	}
	return c, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

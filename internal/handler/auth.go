package handler

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"blog-api/internal/model"
	"blog-api/internal/storage"
	"blog-api/pkg/auth"
)

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && len(email) <= 254
}

func validUsername(username string) bool {
	length := utf8.RuneCountInString(username)
	if length < 3 || length > 50 {
		return false
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var input registerRequest
	if !readJSON(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	switch {
	case !validUsername(input.Username):
		writeError(w, http.StatusBadRequest, "username must be 3-50 letters, digits, dots, underscores or hyphens")
		return
	case !validEmail(input.Email):
		writeError(w, http.StatusBadRequest, "valid email required")
		return
	case utf8.RuneCountInString(input.Password) < 8 || len(input.Password) > 72 || strings.TrimSpace(input.Password) == "":
		writeError(w, http.StatusBadRequest, "password must contain at least 8 characters and at most 72 bytes")
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		log.Printf("hash password: %v", err)
		writeError(w, http.StatusInternalServerError, "could not register user")
		return
	}
	user, err := a.store.CreateUser(input.Username, input.Email, hash)
	if err != nil {
		a.storageError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user.ToResponse())
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if !validEmail(input.Email) || input.Password == "" || len(input.Password) > 72 {
		writeError(w, http.StatusBadRequest, "valid email and password required")
		return
	}
	user, err := a.store.UserByEmail(input.Email)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		a.storageError(w, err)
		return
	}
	if err != nil || !auth.CheckPassword(input.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, expiresAt, err := a.tokens.GenerateToken(user.ID)
	if err != nil {
		log.Printf("generate token: %v", err)
		writeError(w, http.StatusInternalServerError, "could not generate token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Token     string             `json:"token"`
		ExpiresAt time.Time          `json:"expires_at"`
		User      model.UserResponse `json:"user"`
	}{token, expiresAt, user.ToResponse()})
}

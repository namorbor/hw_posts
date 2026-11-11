package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"blog-api/internal/storage"
	"blog-api/pkg/auth"
)

type EventLogger interface {
	LogEvent(string) error
}

type API struct {
	store  *storage.Store
	tokens *auth.JWTManager
	events EventLogger
}

func New(store *storage.Store, tokens *auth.JWTManager, events EventLogger) *API {
	return &API{store: store, tokens: tokens, events: events}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	rw := &responseWriter{ResponseWriter: w}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("panic while handling %s %q: %v", r.Method, r.URL.Path, recovered)
			if rw.status == 0 {
				writeError(rw, http.StatusInternalServerError, "internal server error")
			}
		}
		log.Printf("%s %q status=%d duration=%s", r.Method, r.URL.Path, rw.status, time.Since(started).Round(time.Microsecond))
	}()
	a.route(rw, r)
}

func (a *API) route(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		if allowMethod(w, r, http.MethodGet) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
		return
	case "/register":
		if allowMethod(w, r, http.MethodPost) {
			a.register(w, r)
		}
		return
	case "/login":
		if allowMethod(w, r, http.MethodPost) {
			a.login(w, r)
		}
		return
	case "/posts":
		if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, a.store.Posts())
		} else {
			a.createPost(w, r)
		}
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if (len(parts) != 2 && len(parts) != 3) || parts[0] != "posts" || (len(parts) == 3 && parts[2] != "comments") {
		writeError(w, http.StatusNotFound, "route not found")
		return
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "post id must be a positive integer")
		return
	}
	if len(parts) == 2 {
		if allowMethod(w, r, http.MethodGet) {
			post, err := a.store.PostByID(id)
			if err != nil {
				a.storageError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, post)
		}
		return
	}
	if !allowMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if r.Method == http.MethodPost {
		a.createComment(w, r, id)
		return
	}
	comments, err := a.store.CommentsByPost(id)
	if err != nil {
		a.storageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func allowMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func (a *API) storageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "resource not found")
	case errors.Is(err, storage.ErrConflict):
		writeError(w, http.StatusConflict, "email or username already exists")
	default:
		log.Printf("storage error: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save or load data")
	}
}

func (a *API) userID(w http.ResponseWriter, r *http.Request) (int, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "bearer token required")
		return 0, false
	}
	id, err := a.tokens.ValidateToken(parts[1])
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return 0, false
	}
	if _, err := a.store.UserByID(id); err != nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return 0, false
	}
	return id, true
}

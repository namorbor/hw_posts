package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"blog-api/internal/handler"
	"blog-api/internal/model"
	"blog-api/internal/storage"
	"blog-api/pkg/auth"
)

const testSecret = "test-secret-with-at-least-32-bytes-long"

type eventRecorder struct {
	mu     sync.Mutex
	events []string
}

func (e *eventRecorder) LogEvent(event string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	return nil
}

func newAPI(t *testing.T, dir string, events *eventRecorder) http.Handler {
	t.Helper()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.NewJWTManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return handler.New(store, tokens, events)
}

func request(t *testing.T, api http.Handler, method, path, body, token string, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	checkResponse(t, rec, wantStatus)
	return rec
}

func checkResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("response is not JSON: %q", rec.Header().Get("Content-Type"))
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("invalid JSON response: %s", rec.Body.String())
	}
	if wantStatus >= 400 {
		var response struct {
			Error string `json:"error"`
		}
		decode(t, rec, &response)
		if response.Error == "" {
			t.Fatal("error response has no message")
		}
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, value any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), value); err != nil {
		t.Fatal(err)
	}
}

func registerAndLogin(t *testing.T, api http.Handler) string {
	t.Helper()
	request(t, api, "POST", "/register", `{"username":"alice","email":"alice@example.com","password":"password123"}`, "", 201)
	return login(t, api)
}

func login(t *testing.T, api http.Handler) string {
	t.Helper()
	rec := request(t, api, "POST", "/login", `{"email":"alice@example.com","password":"password123"}`, "", 200)
	var response struct {
		Token     string             `json:"token"`
		ExpiresAt time.Time          `json:"expires_at"`
		User      model.UserResponse `json:"user"`
	}
	decode(t, rec, &response)
	if response.Token == "" || !response.ExpiresAt.After(time.Now()) || response.User.ID != 1 {
		t.Fatalf("invalid login response: %s", rec.Body.String())
	}
	assertNoPassword(t, rec)
	return response.Token
}

func assertNoPassword(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, text := range []string{"password", "$2a$", "$2b$"} {
		if strings.Contains(rec.Body.String(), text) {
			t.Fatalf("response exposes password data: %s", rec.Body.String())
		}
	}
}

func TestBlogLifecycleAndRestart(t *testing.T) {
	dir := t.TempDir()
	events := &eventRecorder{}
	api := newAPI(t, dir, events)

	health := request(t, api, "GET", "/health", "", "", 200)
	var healthBody map[string]string
	decode(t, health, &healthBody)
	if !reflect.DeepEqual(healthBody, map[string]string{"status": "ok"}) {
		t.Fatalf("unexpected health response: %s", health.Body.String())
	}
	empty := request(t, api, "GET", "/posts", "", "", 200)
	if strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("empty collection should be [], got %s", empty.Body.String())
	}

	registered := request(t, api, "POST", "/register", `{"username":"alice","email":"alice@example.com","password":"password123"}`, "", 201)
	var user model.UserResponse
	decode(t, registered, &user)
	if user.ID != 1 || user.Username != "alice" || user.Email != "alice@example.com" || user.CreatedAt.IsZero() {
		t.Fatalf("unexpected registered user: %+v", user)
	}
	assertNoPassword(t, registered)
	token := login(t, api)

	created := request(t, api, "POST", "/posts", `{"title":"Первый пост","content":"Текст публикации"}`, token, 201)
	var post model.Post
	decode(t, created, &post)
	if post.ID != 1 || post.AuthorID != user.ID || post.Title != "Первый пост" || post.Content != "Текст публикации" || post.CreatedAt.IsZero() {
		t.Fatalf("unexpected post: %+v", post)
	}
	var fetched model.Post
	decode(t, request(t, api, "GET", "/posts/1", "", "", 200), &fetched)
	if !reflect.DeepEqual(fetched, post) {
		t.Fatalf("fetched post differs: %+v != %+v", fetched, post)
	}
	if rec := request(t, api, "GET", "/posts/1/comments", "", "", 200); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("new post has comments: %s", rec.Body.String())
	}
	var comment model.Comment
	decode(t, request(t, api, "POST", "/posts/1/comments", `{"text":"Первый комментарий"}`, token, 201), &comment)
	if comment.ID != 1 || comment.PostID != post.ID || comment.AuthorID != user.ID || comment.Text != "Первый комментарий" || comment.CreatedAt.IsZero() {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	if want := []string{"user 1 created post 1", "user 1 created comment 1"}; !reflect.DeepEqual(events.events, want) {
		t.Fatalf("events = %v, want %v", events.events, want)
	}

	// A new store and handler must load the same users, password hashes and records.
	api = newAPI(t, dir, &eventRecorder{})
	login(t, api)
	var posts []model.Post
	decode(t, request(t, api, "GET", "/posts", "", "", 200), &posts)
	if !reflect.DeepEqual(posts, []model.Post{post}) {
		t.Fatalf("posts were not persisted: %+v", posts)
	}
	var comments []model.Comment
	decode(t, request(t, api, "GET", "/posts/1/comments", "", "", 200), &comments)
	if !reflect.DeepEqual(comments, []model.Comment{comment}) {
		t.Fatalf("comments were not persisted: %+v", comments)
	}
	var second model.Post
	decode(t, request(t, api, "POST", "/posts", `{"title":"Второй пост","content":"Новый текст"}`, token, 201), &second)
	if second.ID != 2 {
		t.Fatalf("ID counter was not restored: %d", second.ID)
	}
}

func TestAuthenticationAndUniqueness(t *testing.T) {
	api := newAPI(t, t.TempDir(), &eventRecorder{})
	token := registerAndLogin(t, api)
	request(t, api, "POST", "/register", `{"username":"another","email":"ALICE@example.com","password":"password123"}`, "", 409)
	request(t, api, "POST", "/register", `{"username":"ALICE","email":"other@example.com","password":"password123"}`, "", 409)
	request(t, api, "POST", "/login", `{"email":"alice@example.com","password":"wrong-password"}`, "", 401)
	request(t, api, "POST", "/login", `{"email":"nobody@example.com","password":"password123"}`, "", 401)
	request(t, api, "POST", "/posts", `{"title":"Post","content":"Body"}`, "", 401)
	request(t, api, "POST", "/posts/1/comments", `{"text":"Comment"}`, "", 401)
	request(t, api, "POST", "/posts", `{"title":"Post","content":"Body"}`, "invalid-token", 401)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected JWT, got %q", token)
	}
	if parts[2][0] == 'a' {
		parts[2] = "b" + parts[2][1:]
	} else {
		parts[2] = "a" + parts[2][1:]
	}
	request(t, api, "POST", "/posts", `{"title":"Post","content":"Body"}`, strings.Join(parts, "."), 401)
	request(t, api, "POST", "/posts", `{"title":"Post","content":"Body"}`, token, 201)
	request(t, api, "POST", "/posts/999/comments", `{"text":"Comment"}`, token, 404)
	request(t, api, "GET", "/posts/999", "", "", 404)
	request(t, api, "GET", "/posts/999/comments", "", "", 404)
}

func TestInputValidation(t *testing.T) {
	api := newAPI(t, t.TempDir(), &eventRecorder{})
	token := registerAndLogin(t, api)
	request(t, api, "POST", "/posts", `{"title":"Post","content":"Body"}`, token, 201)
	tests := []struct {
		name string
		path string
		body string
	}{
		{"missing registration fields", "/register", `{}`},
		{"invalid email", "/register", `{"username":"bob","email":"not-an-email","password":"password123"}`},
		{"short password", "/register", `{"username":"bob","email":"bob@example.com","password":"123"}`},
		{"missing login password", "/login", `{"email":"alice@example.com"}`},
		{"whitespace title", "/posts", `{"title":"  \t\n","content":"body"}`},
		{"whitespace content", "/posts", `{"title":"title","content":"  \n"}`},
		{"whitespace comment", "/posts/1/comments", `{"text":"  \t"}`},
		{"missing comment", "/posts/1/comments", `{}`},
		{"unknown field", "/posts", `{"title":"title","content":"body","author_id":99}`},
		{"malformed JSON", "/posts", `{"title":`},
		{"multiple JSON values", "/posts", `{"title":"title","content":"body"} {}`},
		{"wrong field type", "/posts", `{"title":123,"content":"body"}`},
		{"null body", "/posts", `null`},
		{"array body", "/posts", `[]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request(t, api, "POST", tc.path, tc.body, token, 400)
		})
	}

	longPassword, _ := json.Marshal(map[string]string{"username": "bob", "email": "bob@example.com", "password": strings.Repeat("x", 73)})
	request(t, api, "POST", "/register", string(longPassword), "", 400)
	oversized, _ := json.Marshal(map[string]string{"title": "title", "content": strings.Repeat("x", 2<<20)})
	request(t, api, "POST", "/posts", string(oversized), token, 413)
	req := httptest.NewRequest("POST", "/posts", strings.NewReader(`{"title":"title","content":"body"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	checkResponse(t, rec, 415)
}

func TestRoutingErrorsAreJSON(t *testing.T) {
	api := newAPI(t, t.TempDir(), &eventRecorder{})
	request(t, api, "GET", "/not-found", "", "", 404)
	request(t, api, "GET", "/posts/1/unknown", "", "", 404)
	for _, path := range []string{"/health", "/register", "/login", "/posts", "/posts/1", "/posts/1/comments"} {
		t.Run(path, func(t *testing.T) {
			rec := request(t, api, "DELETE", path, "", "", 405)
			if rec.Header().Get("Allow") == "" {
				t.Fatal("405 response is missing Allow header")
			}
		})
	}
}

package storage_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"blog-api/internal/storage"
)

func newStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestPersistenceAndPublicUser(t *testing.T) {
	s, dir := newStore(t)
	user, err := s.CreateUser(" Alice ", " ALICE@example.com ", "stored-bcrypt-hash")
	if err != nil {
		t.Fatal(err)
	}
	post, err := s.CreatePost(user.ID, "Заголовок", "Содержимое")
	if err != nil {
		t.Fatal(err)
	}
	comment, err := s.CreateComment(user.ID, post.ID, "Комментарий")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{user, user.ToResponse()} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "hash") || strings.Contains(string(data), "password") {
			t.Fatalf("password leaked into public JSON: %s", data)
		}
	}
	reopened, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	loadedUser, err := reopened.UserByEmail("alice@EXAMPLE.com")
	if err != nil || loadedUser != user {
		t.Fatalf("user did not survive restart: %+v, %v", loadedUser, err)
	}
	loadedPost, err := reopened.PostByID(post.ID)
	if err != nil || loadedPost != post {
		t.Fatalf("post did not survive restart: %+v, %v", loadedPost, err)
	}
	comments, err := reopened.CommentsByPost(post.ID)
	if err != nil || len(comments) != 1 || comments[0] != comment {
		t.Fatalf("comments did not survive restart: %+v, %v", comments, err)
	}
	newUser, err := reopened.CreateUser("Bob", "bob@example.com", "another-hash")
	if err != nil || newUser.ID <= user.ID {
		t.Fatalf("incorrect user ID after restart: %+v, %v", newUser, err)
	}
	newPost, err := reopened.CreatePost(user.ID, "Next", "Text")
	if err != nil || newPost.ID <= post.ID {
		t.Fatalf("incorrect post ID after restart: %+v, %v", newPost, err)
	}
	newComment, err := reopened.CreateComment(user.ID, post.ID, "Next")
	if err != nil || newComment.ID <= comment.ID {
		t.Fatalf("incorrect comment ID after restart: %+v, %v", newComment, err)
	}
	posts := reopened.Posts()
	posts[0].Title = "changed outside storage"
	comments[0].Text = "changed outside storage"
	unchangedPost, _ := reopened.PostByID(post.ID)
	unchangedComments, _ := reopened.CommentsByPost(post.ID)
	if unchangedPost.Title != post.Title || unchangedComments[0].Text != comment.Text {
		t.Fatal("returned slices expose internal storage")
	}
}

func TestConcurrentCreatesAndUniqueness(t *testing.T) {
	s, dir := newStore(t)
	const count = 16
	var wg sync.WaitGroup
	var created atomic.Int32
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateUser("alice", fmt.Sprintf("alice%d@example.com", i), "hash")
			if err == nil {
				created.Add(1)
			} else if !errors.Is(err, storage.ErrConflict) {
				t.Errorf("unexpected registration error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("created %d users with the same username", created.Load())
	}
	user, err := s.UserByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("other", strings.ToUpper(user.Email), "hash"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("case-insensitive email uniqueness: %v", err)
	}
	if _, err := s.CreateUser(" ALICE ", "different@example.com", "hash"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("normalized username uniqueness: %v", err)
	}
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			post, err := s.CreatePost(user.ID, "Title", "Content")
			if err != nil {
				t.Errorf("create post: %v", err)
				return
			}
			if _, err := s.CreateComment(user.ID, post.ID, "Comment"); err != nil {
				t.Errorf("create comment: %v", err)
			}
			s.Posts()
			s.UserByEmail(user.Email)
		}()
	}
	wg.Wait()
	reopened, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	posts := reopened.Posts()
	if len(posts) != count {
		t.Fatalf("got %d posts, want %d", len(posts), count)
	}
	postIDs, commentIDs := map[int]bool{}, map[int]bool{}
	for _, post := range posts {
		comments, err := reopened.CommentsByPost(post.ID)
		if err != nil || len(comments) != 1 {
			t.Fatalf("comments of post %d: %+v, %v", post.ID, comments, err)
		}
		if postIDs[post.ID] || commentIDs[comments[0].ID] {
			t.Fatal("duplicate ID after concurrent writes")
		}
		postIDs[post.ID], commentIDs[comments[0].ID] = true, true
	}
}

func TestFailedWriteDoesNotChangeMemoryOrConsumeID(t *testing.T) {
	for _, filename := range []string{"users.json", "posts.json", "comments.json"} {
		t.Run(filename, func(t *testing.T) {
			s, dir := newStore(t)
			user, err := s.CreateUser("alice", "alice@example.com", "hash")
			if err != nil {
				t.Fatal(err)
			}
			post, err := s.CreatePost(user.ID, "Title", "Content")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateComment(user.ID, post.ID, "First"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, filename)
			backup := path + ".backup"
			if err := os.Rename(path, backup); err != nil {
				t.Fatal(err)
			}
			// Каталог вместо файла надежно вызывает ошибку rename, даже от root.
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			create := func() (int, error) {
				switch filename {
				case "users.json":
					value, err := s.CreateUser("bob", "bob@example.com", "hash")
					return value.ID, err
				case "posts.json":
					value, err := s.CreatePost(user.ID, "Second", "Text")
					return value.ID, err
				default:
					value, err := s.CreateComment(user.ID, post.ID, "Second")
					return value.ID, err
				}
			}
			if _, err := create(); err == nil {
				t.Fatal("expected persistence error")
			}
			if _, err := s.UserByEmail("bob@example.com"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("failed user create changed memory: %v", err)
			}
			comments, err := s.CommentsByPost(post.ID)
			if len(s.Posts()) != 1 || err != nil || len(comments) != 1 {
				t.Fatal("failed write changed memory")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(backup, path); err != nil {
				t.Fatal(err)
			}
			id, err := create()
			if err != nil || id != 2 {
				t.Fatalf("retry ID = %d, error = %v", id, err)
			}
			if _, err := storage.New(dir); err != nil {
				t.Fatalf("failed write damaged stored files: %v", err)
			}
			files, err := filepath.Glob(filepath.Join(dir, ".data-*.tmp"))
			if err != nil || len(files) != 0 {
				t.Fatalf("temporary files leaked: %v, %v", files, err)
			}
		})
	}
}

func TestStartupRejectsCorruptData(t *testing.T) {
	const user = `{"id":7,"username":"alice","email":"alice@example.com","password_hash":"hash","created_at":"2026-01-01T00:00:00Z"}`
	for _, tc := range []struct {
		name, filename, contents string
	}{
		{"broken JSON", "users.json", `[{`},
		{"empty file", "users.json", ""},
		{"null array", "users.json", `null`},
		{"trailing JSON", "users.json", `[] []`},
		{"duplicate user", "users.json", "[" + user + "," + user + "]"},
		{"missing author", "posts.json", `[{"id":1,"author_id":99,"title":"t","content":"c","created_at":"2026-01-01T00:00:00Z"}]`},
		{"missing post", "comments.json", `[{"id":1,"author_id":7,"post_id":99,"text":"c","created_at":"2026-01-01T00:00:00Z"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, dir := newStore(t)
			if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte("["+user+"]"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(path, []byte(tc.contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := storage.New(dir); err == nil {
				t.Fatal("corrupt data accepted")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != tc.contents {
				t.Fatal("startup overwrote corrupt data")
			}
		})
	}
}

func TestNextIDUsesMaximumStoredID(t *testing.T) {
	_, dir := newStore(t)
	files := map[string]string{
		"users.json":    `[{"id":7,"username":"alice","email":"alice@example.com","password_hash":"hash","created_at":"2026-01-01T00:00:00Z"}]`,
		"posts.json":    `[{"id":10,"author_id":7,"title":"t","content":"c","created_at":"2026-01-01T00:00:00Z"}]`,
		"comments.json": `[{"id":15,"author_id":7,"post_id":10,"text":"c","created_at":"2026-01-01T00:00:00Z"}]`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser("bob", "bob@example.com", "hash")
	if err != nil || user.ID != 8 {
		t.Fatalf("user ID = %d, error = %v", user.ID, err)
	}
	post, err := s.CreatePost(user.ID, "Title", "Content")
	if err != nil || post.ID != 11 {
		t.Fatalf("post ID = %d, error = %v", post.ID, err)
	}
	comment, err := s.CreateComment(user.ID, post.ID, "Text")
	if err != nil || comment.ID != 16 {
		t.Fatalf("comment ID = %d, error = %v", comment.ID, err)
	}
}

func TestReferencesAndEmptyLists(t *testing.T) {
	s, _ := newStore(t)
	if posts := s.Posts(); posts == nil || len(posts) != 0 {
		t.Fatalf("expected empty non-nil posts, got %+v", posts)
	}
	if _, err := s.CreatePost(1, "Title", "Content"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("accepted unknown author: %v", err)
	}
	user, err := s.CreateUser("alice", "alice@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(user.ID, 1, "Text"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("accepted unknown post: %v", err)
	}
	if _, err := s.CommentsByPost(1); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("comments for unknown post: %v", err)
	}
	post, err := s.CreatePost(user.ID, "Title", "Content")
	if err != nil {
		t.Fatal(err)
	}
	comments, err := s.CommentsByPost(post.ID)
	if err != nil || comments == nil || len(comments) != 0 {
		t.Fatalf("expected empty non-nil comments, got %+v, %v", comments, err)
	}
}

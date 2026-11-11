package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"blog-api/internal/model"
)

var (
	ErrConflict = errors.New("user already exists")
	ErrNotFound = errors.New("not found")
)

// Store рассчитан на один процесс сервера. Мьютекс защищает данные и запись файлов.
type Store struct {
	mu          sync.RWMutex
	dir         string
	users       []model.User
	posts       []model.Post
	comments    []model.Comment
	nextUser    int
	nextPost    int
	nextComment int
}

// Отдельная структура позволяет сохранить хеш на диске, не выдавая его в API.
type storedUser struct {
	model.User
	PasswordHash string `json:"password_hash"`
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	users, err := load[storedUser](filepath.Join(dir, "users.json"))
	if err != nil {
		return nil, err
	}
	posts, err := load[model.Post](filepath.Join(dir, "posts.json"))
	if err != nil {
		return nil, err
	}
	comments, err := load[model.Comment](filepath.Join(dir, "comments.json"))
	if err != nil {
		return nil, err
	}
	s := &Store{
		dir: dir, users: make([]model.User, len(users)), posts: posts, comments: comments,
		nextUser: 1, nextPost: 1, nextComment: 1,
	}
	for i, user := range users {
		s.users[i] = user.User
		s.users[i].PasswordHash = user.PasswordHash
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("invalid stored data: %w", err)
	}
	return s, nil
}

func load[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		empty := make([]T, 0)
		return empty, save(path, empty)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	var values []T
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if values == nil {
		return nil, fmt.Errorf("read %s: expected JSON array", path)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read %s: unexpected data after JSON array", path)
	}
	return values, nil
}

// Сначала полностью записываем временный файл, затем заменяем исходный.
func save(path string, value any) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".data-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary data file: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode data: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync data: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func normalized(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *Store) validate() error {
	users := make(map[int]bool)
	emails := make(map[string]bool)
	names := make(map[string]bool)
	for _, user := range s.users {
		email, name := normalized(user.Email), normalized(user.Username)
		if user.ID < 1 || users[user.ID] || email == "" || name == "" ||
			emails[email] || names[name] || user.PasswordHash == "" || user.CreatedAt.IsZero() {
			return errors.New("invalid or duplicate user")
		}
		users[user.ID], emails[email], names[name] = true, true, true
		if user.ID >= s.nextUser {
			s.nextUser = user.ID + 1
		}
	}
	posts := make(map[int]bool)
	for _, post := range s.posts {
		if post.ID < 1 || posts[post.ID] || !users[post.AuthorID] ||
			strings.TrimSpace(post.Title) == "" || strings.TrimSpace(post.Content) == "" || post.CreatedAt.IsZero() {
			return errors.New("invalid post or missing author")
		}
		posts[post.ID] = true
		if post.ID >= s.nextPost {
			s.nextPost = post.ID + 1
		}
	}
	comments := make(map[int]bool)
	for _, comment := range s.comments {
		if comment.ID < 1 || comments[comment.ID] || !users[comment.AuthorID] || !posts[comment.PostID] ||
			strings.TrimSpace(comment.Text) == "" || comment.CreatedAt.IsZero() {
			return errors.New("invalid comment or missing post/author")
		}
		comments[comment.ID] = true
		if comment.ID >= s.nextComment {
			s.nextComment = comment.ID + 1
		}
	}
	return nil
}

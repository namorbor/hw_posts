package storage

import (
	"path/filepath"
	"time"

	"blog-api/internal/model"
)

func (s *Store) CreatePost(authorID int, title, content string) (model.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.userByID(authorID); err != nil {
		return model.Post{}, err
	}
	post := model.Post{
		ID: s.nextPost, AuthorID: authorID, Title: title, Content: content, CreatedAt: time.Now().UTC(),
	}
	posts := append(append(make([]model.Post, 0, len(s.posts)+1), s.posts...), post)
	if err := save(filepath.Join(s.dir, "posts.json"), posts); err != nil {
		return model.Post{}, err
	}
	s.posts = posts
	s.nextPost++
	return post, nil
}

func (s *Store) Posts() []model.Post {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append(make([]model.Post, 0, len(s.posts)), s.posts...)
}

func (s *Store) PostByID(id int) (model.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.postByID(id)
}

func (s *Store) postByID(id int) (model.Post, error) {
	for _, post := range s.posts {
		if post.ID == id {
			return post, nil
		}
	}
	return model.Post{}, ErrNotFound
}

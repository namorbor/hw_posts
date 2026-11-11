package storage

import (
	"path/filepath"
	"time"

	"blog-api/internal/model"
)

func (s *Store) CreateComment(authorID, postID int, text string) (model.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.userByID(authorID); err != nil {
		return model.Comment{}, err
	}
	if _, err := s.postByID(postID); err != nil {
		return model.Comment{}, err
	}
	comment := model.Comment{
		ID: s.nextComment, PostID: postID, AuthorID: authorID, Text: text, CreatedAt: time.Now().UTC(),
	}
	comments := append(append(make([]model.Comment, 0, len(s.comments)+1), s.comments...), comment)
	if err := save(filepath.Join(s.dir, "comments.json"), comments); err != nil {
		return model.Comment{}, err
	}
	s.comments = comments
	s.nextComment++
	return comment, nil
}

func (s *Store) CommentsByPost(postID int) ([]model.Comment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.postByID(postID); err != nil {
		return nil, err
	}
	comments := make([]model.Comment, 0)
	for _, comment := range s.comments {
		if comment.PostID == postID {
			comments = append(comments, comment)
		}
	}
	return comments, nil
}

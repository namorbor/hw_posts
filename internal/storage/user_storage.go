package storage

import (
	"path/filepath"
	"strings"
	"time"

	"blog-api/internal/model"
)

func (s *Store) CreateUser(username, email, passwordHash string) (model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	username, email = strings.TrimSpace(username), normalized(email)
	for _, user := range s.users {
		if normalized(user.Email) == email || normalized(user.Username) == normalized(username) {
			return model.User{}, ErrConflict
		}
	}
	user := model.User{
		ID: s.nextUser, Username: username, Email: email,
		PasswordHash: passwordHash, CreatedAt: time.Now().UTC(),
	}
	users := make([]storedUser, 0, len(s.users)+1)
	for _, old := range s.users {
		users = append(users, storedUser{User: old, PasswordHash: old.PasswordHash})
	}
	users = append(users, storedUser{User: user, PasswordHash: user.PasswordHash})
	if err := save(filepath.Join(s.dir, "users.json"), users); err != nil {
		return model.User{}, err
	}
	s.users = append(s.users, user)
	s.nextUser++
	return user, nil
}

func (s *Store) UserByEmail(email string) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.users {
		if normalized(user.Email) == normalized(email) {
			return user, nil
		}
	}
	return model.User{}, ErrNotFound
}

func (s *Store) UserByID(id int) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userByID(id)
}

func (s *Store) userByID(id int) (model.User, error) {
	for _, user := range s.users {
		if user.ID == id {
			return user, nil
		}
	}
	return model.User{}, ErrNotFound
}

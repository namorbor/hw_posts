package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHash(t *testing.T) {
	password := "correct-horse-123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if hash == password || !CheckPassword(password, hash) || CheckPassword("wrong", hash) {
		t.Fatal("password hash verification failed")
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("unexpected bcrypt cost: %d, %v", cost, err)
	}
	if CheckPassword(password, "not-a-hash") {
		t.Fatal("accepted an invalid hash")
	}
	for _, value := range []string{"", strings.Repeat("x", 73)} {
		if _, err := HashPassword(value); err == nil {
			t.Fatal("accepted empty or oversized password")
		}
	}
}

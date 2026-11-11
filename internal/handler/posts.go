package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (a *API) createPost(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var input struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 200 {
		writeError(w, http.StatusBadRequest, "title must contain 1-200 characters")
		return
	}
	if input.Content == "" || utf8.RuneCountInString(input.Content) > 100000 {
		writeError(w, http.StatusBadRequest, "content must contain 1-100000 characters")
		return
	}
	post, err := a.store.CreatePost(userID, input.Title, input.Content)
	if err != nil {
		a.storageError(w, err)
		return
	}
	a.logEvent(fmt.Sprintf("user %d created post %d", userID, post.ID))
	w.Header().Set("Location", fmt.Sprintf("/posts/%d", post.ID))
	writeJSON(w, http.StatusCreated, post)
}

func (a *API) createComment(w http.ResponseWriter, r *http.Request, postID int) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" || utf8.RuneCountInString(input.Text) > 1000 {
		writeError(w, http.StatusBadRequest, "text must contain 1-1000 characters")
		return
	}
	comment, err := a.store.CreateComment(userID, postID, input.Text)
	if err != nil {
		a.storageError(w, err)
		return
	}
	a.logEvent(fmt.Sprintf("user %d created comment %d", userID, comment.ID))
	writeJSON(w, http.StatusCreated, comment)
}

func (a *API) logEvent(event string) {
	if err := a.events.LogEvent(event); err != nil {
		log.Printf("could not queue event %q: %v", event, err)
	}
}

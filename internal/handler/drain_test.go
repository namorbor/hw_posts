package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestShutdownWaitsForActiveHandler(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	eventSaved := make(chan struct{})
	api := NewTracked(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		close(eventSaved)
		writeJSON(w, http.StatusCreated, map[string]int{"id": 1})
	}))
	response := httptest.NewRecorder()
	go api.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/posts", nil))
	<-entered
	api.BeginShutdown()

	rejected := httptest.NewRecorder()
	api.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/posts", nil))
	var body map[string]string
	if rejected.Code != http.StatusServiceUnavailable || json.Unmarshal(rejected.Body.Bytes(), &body) != nil || body["error"] == "" {
		t.Fatalf("expected JSON 503 during shutdown: %d %s", rejected.Code, rejected.Body.String())
	}

	waiting := make(chan struct{})
	drained := make(chan struct{})
	go func() {
		close(waiting)
		api.Wait()
		close(drained)
	}()
	<-waiting
	select {
	case <-drained:
		t.Fatal("shutdown returned while a handler was still saving its event")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after the handler completed")
	}
	select {
	case <-eventSaved:
	default:
		t.Fatal("shutdown completed before the event was saved")
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("active request failed: %d", response.Code)
	}
}

func TestConcurrentRequestsAndShutdown(t *testing.T) {
	api := NewTracked(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			response := httptest.NewRecorder()
			api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
			if response.Code != http.StatusOK && response.Code != http.StatusServiceUnavailable {
				t.Errorf("unexpected status: %d", response.Code)
			}
		}()
	}
	close(start)
	api.Wait()
	wg.Wait()
	api.Wait()
}

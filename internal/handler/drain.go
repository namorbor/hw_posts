package handler

import (
	"net/http"
	"sync"
)

// Tracked не принимает новые запросы при остановке и учитывает уже запущенные.
type Tracked struct {
	next    http.Handler
	mu      sync.Mutex
	closing bool
	active  sync.WaitGroup
}

func NewTracked(next http.Handler) *Tracked {
	return &Tracked{next: next}
}

func (h *Tracked) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}
	h.active.Add(1)
	h.mu.Unlock()
	defer h.active.Done()
	h.next.ServeHTTP(w, r)
}

func (h *Tracked) BeginShutdown() {
	h.mu.Lock()
	h.closing = true
	h.mu.Unlock()
}

func (h *Tracked) Wait() {
	// Тот же мьютекс исключает Add после начала ожидания, даже если счётчик нулевой.
	h.BeginShutdown()
	h.active.Wait()
}

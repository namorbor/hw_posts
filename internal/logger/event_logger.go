package logger

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrStopped = errors.New("event logger is stopped")

type EventLogger struct {
	events chan string
	done   chan struct{}
	file   *os.File
	delay  time.Duration

	mu      sync.RWMutex
	stopped bool
	err     error // Записывается только воркером, читается после закрытия done.
}

func NewEventLogger(path string, delay time.Duration) (*EventLogger, error) {
	if delay <= 0 {
		return nil, errors.New("event logging delay must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	el := &EventLogger{
		events: make(chan string, 100),
		done:   make(chan struct{}),
		file:   file,
		delay:  delay,
	}
	go el.worker()
	return el, nil
}

func (el *EventLogger) LogEvent(event string) error {
	el.mu.RLock()
	defer el.mu.RUnlock()
	if el.stopped {
		return ErrStopped
	}
	// При заполнении канала ждём воркера: событие не должно потеряться.
	el.events <- event
	return nil
}

func (el *EventLogger) Stop() error {
	el.mu.Lock()
	if !el.stopped {
		el.stopped = true
		close(el.events)
	}
	el.mu.Unlock()
	<-el.done
	return el.err
}

func (el *EventLogger) worker() {
	defer close(el.done)
	defer func() {
		el.recordError(el.file.Sync())
		el.recordError(el.file.Close())
	}()
	for event := range el.events {
		batch := []string{event}
		// Собираем уже накопившиеся события, затем откладываем всю группу.
		// Новые события во время задержки попадут в следующую группу.
		for queued := len(el.events); queued > 0; queued-- {
			next, ok := <-el.events
			if !ok {
				break
			}
			batch = append(batch, next)
		}
		time.Sleep(el.delay)
		for _, message := range batch {
			_, err := fmt.Fprintf(el.file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), message)
			el.recordError(err)
		}
		el.recordError(el.file.Sync())
	}
}

func (el *EventLogger) recordError(err error) {
	if err != nil && el.err == nil {
		el.err = fmt.Errorf("write event log: %w", err)
		log.Printf("event logger: %v", err)
	}
}

package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDelayedWriteAndDrain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	delay := 80 * time.Millisecond
	el, err := NewEventLogger(path, delay)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = el.Stop() })
	started := time.Now()
	for i := 0; i < 20; i++ {
		if err := el.LogEvent(fmt.Sprintf("user 5 created post %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatalf("events were not delayed: %q, %v", data, err)
	}
	if err := el.Stop(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < delay {
		t.Fatal("stop skipped logging delay")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 20 {
		t.Fatalf("lost events: got %d lines", len(lines))
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, fmt.Sprintf("user 5 created post %d", i)) {
			t.Fatalf("wrong event order: %s", line)
		}
	}
	if err := el.LogEvent("too late"); !errors.Is(err, ErrStopped) {
		t.Fatalf("expected ErrStopped, got %v", err)
	}
}

func TestLogAppendAndConcurrentStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	if err := os.WriteFile(path, []byte("existing event\n"), 0600); err != nil {
		t.Fatal(err)
	}
	el, err := NewEventLogger(path, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := make(map[string]bool)
	for i := 0; i < 300; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			message := fmt.Sprintf("event %d", id)
			err := el.LogEvent(message)
			if err == nil {
				mu.Lock()
				accepted[message] = true
				mu.Unlock()
			} else if !errors.Is(err, ErrStopped) {
				t.Errorf("LogEvent: %v", err)
			}
		}(i)
	}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := el.Stop(); err != nil {
				t.Errorf("Stop: %v", err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines[0] != "existing event" || len(lines)-1 != len(accepted) {
		t.Fatalf("lost or overwritten events: lines=%d, accepted=%d", len(lines), len(accepted))
	}
	for _, line := range lines[1:] {
		_, message, ok := strings.Cut(line, " ")
		if !ok || !accepted[message] {
			t.Fatalf("unexpected or duplicate event: %q", line)
		}
		delete(accepted, message)
	}
}

func TestLogErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewEventLogger(dir, time.Millisecond); err == nil {
		t.Fatal("accepted directory as log file")
	}
	if _, err := NewEventLogger(filepath.Join(dir, "log.txt"), 0); err == nil {
		t.Fatal("accepted zero delay")
	}
	el, err := NewEventLogger(filepath.Join(dir, "log.txt"), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := el.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := el.LogEvent("will fail to write"); err != nil {
		t.Fatal(err)
	}
	if err := el.Stop(); err == nil {
		t.Fatal("write error was not reported")
	}
}

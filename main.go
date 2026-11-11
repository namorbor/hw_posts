package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"blog-api/internal/config"
	"blog-api/internal/handler"
	"blog-api/internal/logger"
	"blog-api/internal/storage"
	"blog-api/pkg/auth"

	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := storage.New(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	tokens, err := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		return err
	}
	events, err := logger.NewEventLogger(cfg.LogFile, time.Second)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer func() {
		if err := events.Stop(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close event log: %w", err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	api := handler.NewTracked(handler.New(store, tokens, events))
	// Обработчики должны закончить сохранение и отправку событий до events.Stop.
	defer api.Wait()
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	defer server.Close()
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Print("stopping server, waiting for active requests")
	}
	api.BeginShutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("HTTP shutdown: %w", err)
	}
	log.Print("HTTP server stopped, flushing event log")
	return nil
}

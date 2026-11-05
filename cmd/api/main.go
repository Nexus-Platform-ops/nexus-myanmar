package main

import (
	"context"
	"log/slog"
	"net/http"
	"nexus/internal/api"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := api.EnvConfig()
	s, err := api.New(ctx, cfg)
	if err != nil {
		slog.Error("startup_failed", "reason", err.Error())
		os.Exit(1)
	}
	defer s.Close()
	srv := &http.Server{Addr: cfg.Addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	slog.Info("api_listening", "address", cfg.Addr, "environment", cfg.Env)
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server_stopped")
		os.Exit(1)
	}
}

package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gollama/engine"
	"gollama/internal/config"
	"gollama/internal/handler"
	"gollama/internal/queue"

	"github.com/carmel/go-pkg/logger"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config yaml")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	logWriter := logger.NewLogWriter(cfg.Logger)
	log := logger.NewSlogger(logger.Level(cfg.Logger.LogLevel), logWriter)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eng, err := engine.New(cfg.Engine)
	if err != nil {
		slog.Error("engine init failed", slog.Any("error", err))
		os.Exit(1)
	}
	if err := eng.Start(ctx); err != nil {
		slog.Error("engine start failed", slog.Any("error", err))
		os.Exit(1)
	}

	sched := queue.New(cfg.Queue.MaxConcurrency, cfg.Queue.WaitTimeout)

	mux := http.NewServeMux()
	proxy, err := handler.NewProxyHandler(cfg, eng, sched)
	if err != nil {
		slog.Error("proxy init failed", slog.Any("error", err))
		os.Exit(1)
	}
	proxy.Register(mux)

	srv := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: mux,
	}

	go func() {
		slog.Info("gateway listening", slog.Any("addr", cfg.Server.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", slog.Any("error", err))
	}
	if err := eng.Stop(); err != nil {
		slog.Error("engine stop failed", slog.Any("error", err))
	}
}

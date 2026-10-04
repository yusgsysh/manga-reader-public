package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"manga-reader/internal/app"
	"manga-reader/internal/config"
	"manga-reader/internal/logx"
)

func parseLogLevel(value string) slog.Level {
	return logx.ParseLevel(value)
}

// setupLogger returns the logger and the setter that changes its level at
// runtime, which the settings page drives through app.WithLogSetter.
func setupLogger(level string) (*slog.Logger, func(string)) {
	// The base handler accepts everything; logx gates on the mutable level.
	dyn := logx.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}), parseLogLevel(level))
	logger := dyn.Logger()
	slog.SetDefault(logger)
	return logger, func(value string) { dyn.SetLevel(parseLogLevel(value)) }
}

func main() {
	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, setLevel := setupLogger(cfg.LogLevel)
	logger.Info("starting server",
		"port", cfg.Port,
		"environment", cfg.Environment,
		"db_driver", cfg.Database.Driver,
		"storage_driver", cfg.Storage.ResolvedDriver(),
	)

	a, err := app.New(cfg, logger, app.WithLogSetter(setLevel))
	if err != nil {
		logger.Error("application init failed", "error", err)
		return err
	}

	ln, err := a.Listen()
	if err != nil {
		a.Close()
		logger.Error("listen failed", "error", err)
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", ln.Addr().String())
		if err := a.Serve(ln); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		a.Close()
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		return err
	}
	return nil
}

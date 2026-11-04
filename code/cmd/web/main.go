// Command web runs the Kanban task management application.
//
// Usage:
//
//	kanban [-addr :8080] [-db kanban.db] [-secure-cookies] [-log-format text|json]
//
// Every flag can also be set with an environment variable (KANBAN_ADDR,
// KANBAN_DB, KANBAN_SECURE_COOKIES, KANBAN_LOG_FORMAT).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	kanban "kanban"
	"kanban/internal/database"
	"kanban/internal/web"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func run() error {
	secureDefault, _ := strconv.ParseBool(env("KANBAN_SECURE_COOKIES", "false"))
	addr := flag.String("addr", env("KANBAN_ADDR", ":8080"), "HTTP listen address")
	dbPath := flag.String("db", env("KANBAN_DB", "kanban.db"), "path to the SQLite database file (created if missing)")
	secure := flag.Bool("secure-cookies", secureDefault, "mark cookies Secure (enable when serving over HTTPS)")
	logFormat := flag.String("log-format", env("KANBAN_LOG_FORMAT", "text"), "log format: text or json")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("kanban", version)
		return nil
	}

	var handler slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if *logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	db, err := database.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := database.Migrate(ctx, db, kanban.Migrations, logger); err != nil {
		return err
	}

	app, err := web.New(web.Config{
		DB:            db,
		Logger:        logger,
		TemplatesFS:   kanban.Templates,
		StaticFS:      kanban.Static,
		SecureCookies: *secure,
	})
	if err != nil {
		return err
	}

	// Periodically remove expired sessions.
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if n, err := app.Auth().DeleteExpiredSessions(ctx); err != nil {
				logger.Error("session cleanup failed", "err", err)
			} else if n > 0 {
				logger.Info("expired sessions removed", "count", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(handler, slog.LevelError),
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting server", "addr", *addr, "db", *dbPath, "version", version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}

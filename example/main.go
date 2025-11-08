package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	logdyslog "github.com/kondr1/slog-logdy"
)

func main() {
	// Create Logdy handler
	handler, err := logdyslog.New(logdyslog.Options{
		BaseURL:    "http://localhost:8090",
		APIToken:   "some-big-random-string-here-change-me-please-in-production-12345-67890-abcde-fghij",
		Source:     "my-test-application",
		Level:      slog.LevelInfo,
		BatchSize:  10,
		BatchTimer: time.Second,
	})
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = handler.Close()
	}()

	// Create logger with Logdy handler
	logger := slog.New(handler)

	// Simple logging
	logger.Info("Application started")
	logger.Debug("This won't be logged (below INFO level)")

	// Structured logging
	logger.Info("User logged in",
		"user_id", 12345,
		"username", "john_doe",
		"ip", "192.168.1.1",
	)

	// With context
	ctx := context.Background()
	logger.InfoContext(ctx, "Processing request",
		slog.String("method", "GET"),
		slog.String("path", "/api/users"),
		slog.Int("status", 200),
		slog.Duration("duration", 45*time.Millisecond),
	)

	// With attributes
	userLogger := logger.With(
		slog.String("service", "user-service"),
		slog.String("version", "1.0.0"),
	)
	userLogger.Info("User created", "user_id", 67890)

	// With groups - simple
	logger.WithGroup("database").Info("Query executed",
		slog.String("query", "SELECT * FROM users"),
		slog.Duration("duration", 12*time.Millisecond),
		slog.Int("rows", 42),
	)

	// Nested groups
	logger.WithGroup("server").WithGroup("database").Info("Connection established",
		"host", "localhost",
		"port", 5432,
	)

	// WithGroup + With - critical test case
	logger.WithGroup("g1").With("k1", "v1").WithGroup("g2").With("k2", "v2").Info("Complex nesting test",
		"k3", "v3",
	)

	// With before WithGroup
	logger.With("app", "myapp", "version", "1.0.0").WithGroup("metrics").Info("Metrics collected",
		"cpu_percent", 45.2,
		"memory_mb", 512,
	)

	// Multiple groups at different levels
	baseLogger := logger.With("service", "api")
	requestLogger := baseLogger.WithGroup("request")
	dbLogger := requestLogger.WithGroup("database")
	dbLogger.Info("Database operation",
		"operation", "INSERT",
		"table", "users",
		"rows", 1,
	)

	// Error logging
	logger.Error("Operation failed",
		slog.String("operation", "someOperation"),
		slog.Any("error", errors.New("sample error message")),
	)

	// Different log levels
	logger.Debug("Debug message")
	logger.Info("Info message")
	logger.Warn("Warning message")
	logger.Error("Error message")

	// Complex structured data
	logger.Info("Complex event",
		slog.Group("user",
			slog.String("id", "user-123"),
			slog.String("name", "Alice"),
			slog.String("email", "alice@example.com"),
		),
		slog.Group("metadata",
			slog.Time("timestamp", time.Now()),
			slog.Bool("is_active", true),
			slog.Float64("score", 95.5),
		),
	)

	// Manual flush (if needed before Close)
	if err := handler.Flush(); err != nil {
		logger.Error("Failed to flush logs", slog.Any("error", err))
	}

	logger.Info("Application shutting down")

	// Close will flush remaining logs
	// handler.Close() is called via defer
}

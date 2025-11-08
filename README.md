# logdy-slog

A [slog.Handler](https://pkg.go.dev/log/slog#Handler) implementation that sends logs to [Logdy](https://logdy.dev/) via REST API.

## Installation

```bash
go get github.com/kondr1/logdy-slog
```

## Quick Start

```go
package main

import (
    "log/slog"
    "time"

    logdyslog "github.com/kondr1/logdy-slog"
)

func main() {
    handler, err := logdyslog.New(logdyslog.Options{
        BaseURL:          "http://localhost:8080",
        APIToken:         "your-api-token",
        Source:           "my-application",
        Level:            slog.LevelInfo,
        BatchSize:        10,
        MaxBatchSize:     1000,
        BatchTimer:       time.Second,
        OnSendingFailure: func(err error) {
			fmt.Println("Failed to send logs to Logdy:", err)
		}
    })
    if err != nil {
        panic(err)
    }
    defer handler.Close()

    logger := slog.New(handler)

    logger.Info("Application started")
    logger.Info("User logged in",
        "user_id", 12345,
        "username", "john_doe",
    )
}
```

## Configuration

### Options

The handler provides several configuration options to customize its behavior:

```go
type Options struct {
    // BaseURL is the base URL of the Logdy instance (required)
    // Example: "http://localhost:8080"
    BaseURL string

    // APIToken is the bearer token for authentication (required)
    APIToken string

    // Source is an optional identifier for the log source
    // This helps identify logs from different services in the Logdy UI
    // Example: "api-server", "worker-1", "payment-service"
    Source string

    // Level is the minimum log level to handle
    // Defaults to slog.LevelInfo
    Level slog.Leveler

    // HTTPClient is an optional custom HTTP client
    // Defaults to http.DefaultClient
    // Use this to configure timeouts, TLS settings, or custom transport
    HTTPClient *http.Client

    // BatchSize is the number of logs to batch before sending
    // Defaults to 10. Set to 1 to disable batching
    BatchSize int

    // MaxBatchSize is the maximum number of logs to keep in the buffer
    // Defaults to 1000
    // When the buffer exceeds this size (due to sending failures),
    // the oldest logs will be dropped to prevent memory growth
    MaxBatchSize int

    // BatchTimer is the maximum time to wait before sending a batch
    // Defaults to 1 second. Only applies when batching is enabled
    BatchTimer time.Duration

    // OnSendingFailure is called when sending logs to Logdy fails
    // Defaults to logging the error via log.Println
    // Use this to implement custom error handling, metrics, or fallback logging
    OnSendingFailure func(error)
}
```

## Batching

The handler supports batching to improve performance:

- Logs are buffered until `BatchSize` is reached
- Or until `BatchTimer` expires
- Whichever comes first

To disable batching, set `BatchSize` to 1:

```go
handler, err := logdyslog.New(logdyslog.Options{
    BaseURL:   "http://localhost:8080",
    APIToken:  "your-api-token",
    BatchSize: 1, // Send immediately
})
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

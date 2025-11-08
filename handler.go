// Package logdyslog provides a slog.Handler implementation that sends logs to Logdy via REST API.
package sloglogdy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"log"
)

// From https://github.com/golang/example/blob/master/slog-handler-guide/README.md
//
// groupOrAttrs holds either a group name or a list of slog.Attr.
type groupOrAttrs struct {
	group string
	attrs []slog.Attr
}

// Handler is a slog.Handler that sends logs to Logdy via REST API.
type Handler struct {
	level slog.Leveler
	goas  []groupOrAttrs
	buf   *buffer
}

// logEntry represents a single log entry to be sent to Logdy.
type logEntry struct {
	Ts  int64       `json:"ts"`
	Log interface{} `json:"log"`
}

// logRequest represents the request body for Logdy API.
type logRequest struct {
	Logs   []logEntry `json:"logs"`
	Source string     `json:"source,omitempty"`
}

// Options configures the Logdy handler.
type Options struct {
	// BaseURL is the base URL of the Logdy instance (e.g., "http://localhost:8080")
	BaseURL string

	// APIToken is the bearer token for authentication
	APIToken string

	// Source is an optional identifier for the log source
	Source string

	// Level is the minimum log level to handle (defaults to slog.LevelInfo)
	Level slog.Leveler

	// HTTPClient is an optional custom HTTP client (defaults to http.DefaultClient)
	HTTPClient *http.Client

	// BatchSize is the number of logs to batch before sending (defaults to 10)
	// Set to 1 to disable batching
	BatchSize int

	// MaxBatchSize is the maximum number of logs to keep in the buffer (defaults to 1000)
	// When the buffer exceeds this size, the oldest logs will be dropped
	MaxBatchSize int

	// BatchTimer is the maximum time to wait before sending a batch (defaults to 1 second)
	// Only applies when batching is enabled
	BatchTimer time.Duration

	// OnSendingFailure is called when sending errors to Logdy fails
	OnSendingFailure func(error)
}

// New creates a new Logdy handler with the given options.
func New(opts Options) (*Handler, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("BaseURL is required")
	}
	if opts.APIToken == "" {
		return nil, fmt.Errorf("APIToken is required")
	}

	if opts.Level == nil {
		opts.Level = slog.LevelInfo
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 10
	}
	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = 1000
	}
	if opts.BatchTimer <= 0 {
		opts.BatchTimer = time.Second
	}
	if opts.OnSendingFailure == nil {
		opts.OnSendingFailure = func(err error) {
			log.Println("Failed to send logs to Logdy:", err)
		}
	}

	h := &Handler{
		level: opts.Level,
		buf:   newBuffer(opts.BaseURL, opts.APIToken, opts.Source, opts.HTTPClient, opts.BatchSize, opts.MaxBatchSize, opts.BatchTimer, opts.OnSendingFailure),
	}

	return h, nil
}

// Enabled reports whether the handler handles records at the given level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle handles the log record.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	logData := make(map[string]interface{})
	logData[slog.TimeKey] = r.Time.Format(time.RFC3339Nano)
	logData[slog.LevelKey] = r.Level.String()
	logData[slog.MessageKey] = r.Message

	goas := h.goas

	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		addAttr(logData, slog.String(slog.SourceKey, fmt.Sprintf("%s:%d", f.File, f.Line)))
	}

	if r.NumAttrs() == 0 {
		for len(goas) > 0 && goas[len(goas)-1].group != "" {
			goas = goas[:len(goas)-1]
		}
	}

	if len(goas) > 0 {
		h.handleGroupsAndAttrs(logData, goas, r)
	} else {
		r.Attrs(func(attr slog.Attr) bool {
			addAttr(logData, attr)
			return true
		})
	}

	entry := logEntry{
		Ts:  r.Time.UnixMilli(),
		Log: logData,
	}

	return h.buf.Add(entry)
}

// handleGroupsAndAttrs processes the groups and attributes, building the nested structure
func (h *Handler) handleGroupsAndAttrs(logData map[string]interface{}, goas []groupOrAttrs, r slog.Record) {
	currentMap := logData

	for _, goa := range goas {
		if goa.group != "" {
			groupMap := make(map[string]interface{})
			currentMap[goa.group] = groupMap
			currentMap = groupMap
		} else {
			for _, attr := range goa.attrs {
				addAttr(currentMap, attr)
			}
		}
	}

	r.Attrs(func(attr slog.Attr) bool {
		addAttr(currentMap, attr)
		return true
	})
}

// WithAttrs returns a new Handler with the given attributes added.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

// WithGroup returns a new Handler with the given group added.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

// withGroupOrAttrs is a helper method that creates a new handler with an additional
// group or attrs entry. It ensures immutability by creating a shallow copy of the handler
// and a deep copy of the goas slice.
func (h *Handler) withGroupOrAttrs(goa groupOrAttrs) *Handler {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h2.goas)-1] = goa
	return &h2
}

// Close flushes any buffered logs and stops the batch timer.
// It should be called when the handler is no longer needed.
func (h *Handler) Close() error { return h.buf.Close() }

// Flush sends all buffered logs to Logdy immediately.
func (h *Handler) Flush() error { return h.buf.flush() }

// addAttr adds an attribute to the log data map.
func addAttr(data map[string]interface{}, attr slog.Attr) {
	if attr.Equal(slog.Attr{}) {
		return
	}

	key := attr.Key
	value := attr.Value.Resolve()

	switch value.Kind() {
	case slog.KindGroup:
		groupData := make(map[string]interface{})
		for _, groupAttr := range value.Group() {
			addAttr(groupData, groupAttr)
		}
		data[key] = groupData
	case slog.KindBool:
		data[key] = value.Bool()
	case slog.KindInt64:
		data[key] = value.Int64()
	case slog.KindUint64:
		data[key] = value.Uint64()
	case slog.KindFloat64:
		data[key] = value.Float64()
	case slog.KindString:
		data[key] = value.String()
	case slog.KindDuration:
		data[key] = value.Duration().String()
	case slog.KindTime:
		data[key] = value.Time().Format(time.RFC3339Nano)
	default:
		data[key] = value.String()
	}
}

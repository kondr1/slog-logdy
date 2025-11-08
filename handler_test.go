package sloglogdy

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWithGroupAndAttrs(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*slog.Logger)
		expected map[string]interface{}
	}{
		{
			name: "simple WithGroup",
			setup: func(logger *slog.Logger) {
				logger.WithGroup("g1").Info("test",
					"k1", "v1",
				)
			},
			expected: map[string]interface{}{
				"level": "INFO",
				"msg":   "test",
				"g1": map[string]interface{}{
					"k1": "v1",
				},
			},
		},
		{
			name: "nested WithGroup",
			setup: func(logger *slog.Logger) {
				logger.WithGroup("g1").WithGroup("g2").Info("test",
					"k1", "v1",
				)
			},
			expected: map[string]interface{}{
				"level": "INFO",
				"msg":   "test",
				"g1": map[string]interface{}{
					"g2": map[string]interface{}{
						"k1": "v1",
					},
				},
			},
		},
		{
			name: "WithGroup and With",
			setup: func(logger *slog.Logger) {
				logger.WithGroup("g1").With("k1", "v1").Info("test",
					"k2", "v2",
				)
			},
			expected: map[string]interface{}{
				"level": "INFO",
				"msg":   "test",
				"g1": map[string]interface{}{
					"k1": "v1",
					"k2": "v2",
				},
			},
		},
		{
			name: "complex nesting",
			setup: func(logger *slog.Logger) {
				logger.WithGroup("g1").With("k1", "v1").WithGroup("g2").With("k2", "v2").Info("test",
					"k3", "v3",
				)
			},
			expected: map[string]interface{}{
				"level": "INFO",
				"msg":   "test",
				"g1": map[string]interface{}{
					"k1": "v1",
					"g2": map[string]interface{}{
						"k2": "v2",
						"k3": "v3",
					},
				},
			},
		},
		{
			name: "With before WithGroup",
			setup: func(logger *slog.Logger) {
				logger.With("k1", "v1").WithGroup("g1").Info("test",
					"k2", "v2",
				)
			},
			expected: map[string]interface{}{
				"level": "INFO",
				"msg":   "test",
				"k1":    "v1",
				"g1": map[string]interface{}{
					"k2": "v2",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var receivedLogs []logEntry

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req logRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				mu.Lock()
				receivedLogs = append(receivedLogs, req.Logs...)
				mu.Unlock()

				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()

			handler, err := New(Options{
				BaseURL:    server.URL,
				APIToken:   "test-token",
				BatchSize:  1,
				BatchTimer: time.Hour,
			})
			if err != nil {
				t.Fatalf("Failed to create handler: %v", err)
			}
			defer handler.Close()

			logger := slog.New(handler)

			tt.setup(logger)

			if err := handler.Flush(); err != nil {
				t.Fatalf("Failed to flush: %v", err)
			}

			time.Sleep(100 * time.Millisecond)

			mu.Lock()
			defer mu.Unlock()

			if len(receivedLogs) != 1 {
				t.Fatalf("Expected 1 log, got %d", len(receivedLogs))
			}

			logData, ok := receivedLogs[0].Log.(map[string]interface{})
			if !ok {
				t.Fatalf("Log is not a map: %T", receivedLogs[0].Log)
			}

			for key, expectedValue := range tt.expected {
				if key == "time" {
					continue
				}

				actualValue, exists := logData[key]
				if !exists {
					t.Errorf("Expected key %q not found in log", key)
					continue
				}

				if !deepEqual(expectedValue, actualValue) {
					t.Errorf("Key %q: expected %v, got %v", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// deepEqual compares two values deeply, handling maps and slices
func deepEqual(expected, actual interface{}) bool {
	expectedJSON, _ := json.Marshal(expected)
	actualJSON, _ := json.Marshal(actual)
	return string(expectedJSON) == string(actualJSON)
}

// TestEmptyGroup tests that empty groups are removed when there are no attributes
func TestEmptyGroup(t *testing.T) {
	var mu sync.Mutex
	var receivedLogs []logEntry

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req logRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedLogs = append(receivedLogs, req.Logs...)
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:    server.URL,
		APIToken:   "test-token",
		BatchSize:  1,
		BatchTimer: time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger := slog.New(handler)

	logger.WithGroup("empty").Info("test")

	handler.Flush()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedLogs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(receivedLogs))
	}

	logData, ok := receivedLogs[0].Log.(map[string]interface{})
	if !ok {
		t.Fatalf("Log is not a map")
	}

	if _, exists := logData["empty"]; exists {
		t.Errorf("Empty group should not be present in log")
	}
}

// TestHandlerImmutability tests that WithGroup and WithAttrs return new handlers
func TestHandlerImmutability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:   server.URL,
		APIToken:  "test-token",
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger1 := slog.New(handler)
	logger2 := logger1.WithGroup("g1")
	logger3 := logger1.With("k1", "v1")

	logger1.Info("test1")
	logger2.Info("test2")
	logger3.Info("test3")

}

// TestContextLogging tests logging with context
func TestContextLogging(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:   server.URL,
		APIToken:  "test-token",
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger := slog.New(handler)

	ctx := context.Background()
	logger.InfoContext(ctx, "test message", "key", "value")

	handler.Flush()
}

// TestDifferentTypes tests logging with different data types
func TestDifferentTypes(t *testing.T) {
	var mu sync.Mutex
	var receivedLogs []logEntry

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req logRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedLogs = append(receivedLogs, req.Logs...)
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:    server.URL,
		APIToken:   "test-token",
		BatchSize:  1,
		BatchTimer: time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger := slog.New(handler)

	logger.Info("test",
		"bool", true,
		"int", int64(42),
		"uint", uint64(123),
		"float", 3.14,
		"string", "hello",
		"duration", time.Second,
		"time", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		slog.Group("nested",
			"inner", "value",
		),
	)

	if err := handler.Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedLogs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(receivedLogs))
	}

	logData, ok := receivedLogs[0].Log.(map[string]interface{})
	if !ok {
		t.Fatalf("Log is not a map")
	}

	if v, ok := logData["bool"].(bool); !ok || v != true {
		t.Errorf("Expected bool true, got %v (%T)", logData["bool"], logData["bool"])
	}

	if v, ok := logData["int"].(float64); !ok || v != 42 {
		t.Errorf("Expected int 42, got %v (%T)", logData["int"], logData["int"])
	}

	if v, ok := logData["float"].(float64); !ok || v != 3.14 {
		t.Errorf("Expected float 3.14, got %v (%T)", logData["float"], logData["float"])
	}

	if v, ok := logData["string"].(string); !ok || v != "hello" {
		t.Errorf("Expected string 'hello', got %v (%T)", logData["string"], logData["string"])
	}

	if v, ok := logData["duration"].(string); !ok || v != "1s" {
		t.Errorf("Expected duration '1s', got %v (%T)", logData["duration"], logData["duration"])
	}

	if nested, ok := logData["nested"].(map[string]interface{}); !ok {
		t.Errorf("Expected nested group, got %T", logData["nested"])
	} else if v, ok := nested["inner"].(string); !ok || v != "value" {
		t.Errorf("Expected nested value 'value', got %v (%T)", nested["inner"], nested["inner"])
	}
}

// TestBufferOverflow tests buffer overflow protection
func TestBufferOverflow(t *testing.T) {
	var errorCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:      server.URL,
		APIToken:     "test-token",
		BatchSize:    5,
		MaxBatchSize: 10,
		BatchTimer:   50 * time.Millisecond,
		OnSendingFailure: func(err error) {
			mu.Lock()
			errorCount++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger := slog.New(handler)

	for i := 0; i < 30; i++ {
		logger.Info("test", "index", i)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	errors := errorCount
	mu.Unlock()

	if errors == 0 {
		t.Error("Expected some errors due to server failures")
	}
}

// TestBatchTimer tests that batch timer triggers flush
func TestBatchTimer(t *testing.T) {
	var mu sync.Mutex
	var receivedCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req logRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedCount += len(req.Logs)
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:    server.URL,
		APIToken:   "test-token",
		BatchSize:  100,
		BatchTimer: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger := slog.New(handler)

	for i := 0; i < 5; i++ {
		logger.Info("test", "index", i)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 5 {
		t.Errorf("Expected 5 logs to be sent via timer, got %d", count)
	}
}

// TestConcurrentLogging tests concurrent logging from multiple goroutines
func TestConcurrentLogging(t *testing.T) {
	var mu sync.Mutex
	var receivedCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req logRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedCount += len(req.Logs)
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:    server.URL,
		APIToken:   "test-token",
		BatchSize:  10,
		BatchTimer: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}
	defer handler.Close()

	logger := slog.New(handler)

	const numGoroutines = 10
	const logsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < logsPerGoroutine; j++ {
				logger.Info("concurrent test", "goroutine", id, "index", j)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	expected := numGoroutines * logsPerGoroutine
	if count != expected {
		t.Errorf("Expected %d logs, got %d", expected, count)
	}
}

// TestHTTPErrors tests handling of various HTTP errors
func TestHTTPErrors(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		expectErr  bool
	}{
		{"success 202", http.StatusAccepted, false},
		{"error 500", http.StatusInternalServerError, true},
		{"error 400", http.StatusBadRequest, true},
		{"error 404", http.StatusNotFound, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var gotError bool
			var mu sync.Mutex

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			handler, err := New(Options{
				BaseURL:    server.URL,
				APIToken:   "test-token",
				BatchSize:  1,
				BatchTimer: time.Hour,
				OnSendingFailure: func(err error) {
					mu.Lock()
					gotError = true
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatalf("Failed to create handler: %v", err)
			}
			defer handler.Close()

			logger := slog.New(handler)
			logger.Info("test")

			time.Sleep(100 * time.Millisecond)

			mu.Lock()
			hasError := gotError
			mu.Unlock()

			if hasError != tc.expectErr {
				t.Errorf("Expected error=%v, got error=%v", tc.expectErr, hasError)
			}
		})
	}
}

// TestCloseFlush tests that Close flushes remaining logs
func TestCloseFlush(t *testing.T) {
	var mu sync.Mutex
	var receivedCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req logRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedCount += len(req.Logs)
		mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	handler, err := New(Options{
		BaseURL:    server.URL,
		APIToken:   "test-token",
		BatchSize:  100,
		BatchTimer: time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	logger := slog.New(handler)

	for i := 0; i < 5; i++ {
		logger.Info("test", "index", i)
	}

	if err := handler.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	if count != 5 {
		t.Errorf("Expected 5 logs to be flushed on close, got %d", count)
	}
}

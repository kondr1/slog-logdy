package sloglogdy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type buffer struct {
	OnSendingFailure func(error)
	baseURL          string
	apiToken         string
	source           string
	client           *http.Client
	batchSize        int
	maxBatchSize     int
	batchTimer       time.Duration
	buffer           []logEntry
	mu               sync.Mutex
	done             chan struct{}
	wg               sync.WaitGroup
}

func (b *buffer) Add(entry logEntry) error {
	b.mu.Lock()
	b.buffer = append(b.buffer, entry)
	shouldFlush := len(b.buffer) >= b.batchSize
	b.mu.Unlock()
	if shouldFlush {
		return b.flush()
	}

	return nil
}
func (b *buffer) flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buffer) == 0 {
		return nil
	}
	logs := make([]logEntry, len(b.buffer))
	copy(logs, b.buffer)

	err := b.sendLogs(logs)
	if err != nil {
		b.OnSendingFailure(err)
		if len(b.buffer) > b.maxBatchSize {
			dropped := len(b.buffer) - b.maxBatchSize
			copy(b.buffer, b.buffer[len(b.buffer)-b.maxBatchSize:])
			b.buffer = b.buffer[:b.maxBatchSize]
			b.OnSendingFailure(fmt.Errorf("buffer overflow: dropped %d oldest logs", dropped))
		}
		return err
	}

	b.buffer = b.buffer[:0]
	return nil
}

// batchTimerLoop periodically flushes buffered logs.
func (b *buffer) batchTimerLoop() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.batchTimer)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := b.flush()
			if err != nil {
				b.OnSendingFailure(err)
			}
		case <-b.done:
			return
		}
	}
}

// sendLogs sends logs to Logdy via REST API.
func (b *buffer) sendLogs(logs []logEntry) error {
	if len(logs) == 0 {
		return nil
	}

	req := logRequest{
		Logs:   logs,
		Source: b.source,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal logs: %w", err)
	}

	httpReq, err := http.NewRequest("POST", b.baseURL+"/api/log", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+b.apiToken)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send logs: %w", err)
	}
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			b.OnSendingFailure(fmt.Errorf("failed to close response body: %w", err))
		}
	}()

	if resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func (b *buffer) Close() error {
	close(b.done)
	b.wg.Wait()
	return b.flush()
}

func newBuffer(baseURL, apiToken, source string, client *http.Client, batchSize int, maxBatchSize int, batchTimer time.Duration, onSendingFailure func(error)) *buffer {
	buf := &buffer{
		baseURL:          baseURL,
		apiToken:         apiToken,
		source:           source,
		client:           client,
		batchSize:        batchSize,
		maxBatchSize:     maxBatchSize,
		batchTimer:       batchTimer,
		buffer:           make([]logEntry, 0, batchSize),
		done:             make(chan struct{}),
		OnSendingFailure: onSendingFailure,
	}
	if buf.batchSize > 1 {
		buf.wg.Add(1)
		go buf.batchTimerLoop()
	}

	return buf
}

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package lance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/execprio"
)

// Writer is a warm lance_helper.py serve client with batched upserts.
// Upsert blocks until the row has been flushed (batch full, timer, Flush, or Close).
type Writer struct {
	store     *Store
	batchSize int

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu   sync.Mutex // pending / waiters / closed
	ioMu sync.Mutex // stdin/stdout round-trips (held without mu during Lance I/O)

	pending []pendingRow
	waiters []chan error
	closed  bool
}

type pendingRow struct {
	TrackID int64     `json:"track_id"`
	Vector  []float64 `json:"vector"`
}

// StartWriter launches a persistent Lance helper. batchSize defaults to 8.
func (s *Store) StartWriter(batchSize int) (*Writer, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("lancedb not configured")
	}
	if batchSize <= 0 {
		batchSize = 8
	}
	if err := os.MkdirAll(s.DBPath, 0o755); err != nil {
		return nil, err
	}
	table := s.table()
	cmd := exec.Command(s.pythonBin(), s.HelperPath, "serve", s.DBPath, table)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("lance writer start: %w", err)
	}
	if cmd.Process != nil {
		execprio.Lower(cmd.Process)
	}
	w := &Writer{
		store:     s,
		batchSize: batchSize,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    bufio.NewReader(stdout),
	}
	if err := w.ping(); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("lance writer ping: %w", err)
	}
	return w, nil
}

func (w *Writer) ping() error {
	w.ioMu.Lock()
	defer w.ioMu.Unlock()
	resp, err := w.roundTripLocked(map[string]any{"op": "ping"})
	if err != nil {
		return err
	}
	if ok, _ := resp["ok"].(bool); !ok {
		return fmt.Errorf("%v", resp["error"])
	}
	return nil
}

func (w *Writer) roundTripLocked(req map[string]any) (map[string]any, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := w.stdin.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := w.stdout.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Upsert queues a row and waits until a flush acknowledges it.
func (w *Writer) Upsert(ctx context.Context, trackID int64, vec []float32) (string, error) {
	if w == nil {
		return "", fmt.Errorf("nil lance writer")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	lanceID := fmt.Sprintf("%s/%s#%d", w.store.DBPath, w.store.table(), trackID)

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return "", fmt.Errorf("lance writer closed")
	}
	w.pending = append(w.pending, pendingRow{
		TrackID: trackID,
		Vector:  float32To64(vec),
	})
	w.waiters = append(w.waiters, done)
	full := len(w.pending) >= w.batchSize
	w.mu.Unlock()

	if full {
		if err := w.flush(ctx); err != nil {
			// Waiter was signaled inside flush; prefer that error if present.
			select {
			case e := <-done:
				return lanceID, e
			default:
				return lanceID, err
			}
		}
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-done:
		return lanceID, err
	}
}

// Flush writes any buffered rows and unblocks waiters.
func (w *Writer) Flush(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return w.flush(ctx)
}

func (w *Writer) flush(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return nil
	}
	rows := w.pending
	waiters := w.waiters
	w.pending = nil
	w.waiters = nil
	w.mu.Unlock()

	if err := ctx.Err(); err != nil {
		for _, ch := range waiters {
			ch <- err
		}
		return err
	}

	start := time.Now()
	w.ioMu.Lock()
	resp, err := w.roundTripLocked(map[string]any{
		"op":   "upsert_batch",
		"rows": rows,
	})
	w.ioMu.Unlock()
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		log.Printf("lance writer flush n=%d took %s", len(rows), elapsed.Round(time.Millisecond))
	}

	if err != nil {
		err = fmt.Errorf("lance writer flush: %w", err)
	} else if ok, _ := resp["ok"].(bool); !ok {
		msg, _ := resp["error"].(string)
		if msg == "" {
			msg = "lance upsert_batch failed"
		}
		err = fmt.Errorf("lancedb upsert: %s", msg)
	}
	for _, ch := range waiters {
		ch <- err
	}
	return err
}

// Close flushes remaining rows and stops the helper.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()

	flushErr := w.flush(context.Background())

	w.ioMu.Lock()
	_, _ = w.stdin.Write([]byte(`{"op":"shutdown"}` + "\n"))
	_ = w.stdin.Close()
	w.ioMu.Unlock()

	waitCh := make(chan error, 1)
	go func() {
		if w.cmd != nil {
			waitCh <- w.cmd.Wait()
			return
		}
		waitCh <- nil
	}()
	select {
	case <-waitCh:
	case <-time.After(15 * time.Second):
		if w.cmd != nil && w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
		}
	}
	return flushErr
}

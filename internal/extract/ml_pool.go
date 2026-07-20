// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package extract

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/shirtbrotherlabs/LatentTone/internal/execprio"
)

// MLPool is a pool of warm ml_embed_helper.py serve processes (models stay loaded).
type MLPool struct {
	cfg     MLHelperConfig
	free    chan *mlWorker
	workers []*mlWorker
	nextID  atomic.Int64
	closed  atomic.Bool
}

type mlWorker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

type mlServeReq struct {
	ID     int64  `json:"id"`
	Cmd    string `json:"cmd"`
	Audio  string `json:"audio,omitempty"`
	Model  string `json:"model,omitempty"`
	Extra  string `json:"extra,omitempty"`
	RawF32 string `json:"raw_f32le,omitempty"`
}

type mlServeResp struct {
	ID       int64          `json:"id"`
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Features map[string]any `json:"features,omitempty"`
	Vector   []float64      `json:"vector,omitempty"`
	Pong     bool           `json:"pong,omitempty"`
}

// StartMLPool launches size warm helper processes. size is clamped to >=1.
func StartMLPool(cfg MLHelperConfig, size int) (*MLPool, error) {
	if size < 1 {
		size = 1
	}
	p := &MLPool{
		cfg:  cfg,
		free: make(chan *mlWorker, size),
	}
	for i := 0; i < size; i++ {
		w, err := startMLWorker(cfg)
		if err != nil {
			_ = p.Close()
			return nil, err
		}
		p.workers = append(p.workers, w)
		p.free <- w
	}
	return p, nil
}

func startMLWorker(cfg MLHelperConfig) (*mlWorker, error) {
	cmd := exec.Command(cfg.pythonBin(), cfg.helper(), "serve")
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
		return nil, fmt.Errorf("ml pool start: %w", err)
	}
	if cmd.Process != nil {
		execprio.Lower(cmd.Process)
	}
	w := &mlWorker{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}
	// Warm ping so first track does not pay connect-only latency surprises.
	if err := w.ping(); err != nil {
		_ = w.close()
		return nil, fmt.Errorf("ml pool ping: %w", err)
	}
	return w, nil
}

func (w *mlWorker) ping() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	req := mlServeReq{ID: 0, Cmd: "ping"}
	b, _ := json.Marshal(req)
	if _, err := w.stdin.Write(append(b, '\n')); err != nil {
		return err
	}
	line, err := w.stdout.ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp mlServeResp
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (w *mlWorker) call(ctx context.Context, req mlServeReq) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := w.stdin.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("ml pool write: %w", err)
	}
	type readResult struct {
		line []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := w.stdout.ReadBytes('\n')
		ch <- readResult{line, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case rr := <-ch:
		if rr.err != nil {
			return nil, fmt.Errorf("ml pool read: %w", rr.err)
		}
		var resp mlServeResp
		if err := json.Unmarshal(rr.line, &resp); err != nil {
			return nil, fmt.Errorf("ml pool parse: %w", err)
		}
		if !resp.OK {
			msg := resp.Error
			if msg == "" {
				msg = "ml helper failed"
			}
			return nil, fmt.Errorf("ml helper: %s", msg)
		}
		return mlResultFromVector(resp.Features, resp.Vector)
	}
}

func (w *mlWorker) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.stdin.Write([]byte(`{"cmd":"shutdown"}` + "\n"))
	_ = w.stdin.Close()
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Wait()
	}
	return nil
}

// Call runs cmd (yamnet|musicnn) on a pooled warm worker.
func (p *MLPool) Call(ctx context.Context, cmd, audio, model, extra, rawF32le string) (*Result, error) {
	if p == nil || p.closed.Load() {
		return nil, fmt.Errorf("ml pool closed")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case w := <-p.free:
		defer func() { p.free <- w }()
		req := mlServeReq{
			ID:     p.nextID.Add(1),
			Cmd:    cmd,
			Audio:  audio,
			Model:  model,
			Extra:  extra,
			RawF32: rawF32le,
		}
		return w.call(ctx, req)
	}
}

// Close shuts down all warm helpers. Callers must not Call after Close.
func (p *MLPool) Close() error {
	if p == nil || !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	for range p.workers {
		w := <-p.free
		_ = w.close()
	}
	return nil
}

func mlResultFromVector(features map[string]any, vector []float64) (*Result, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("ml helper returned empty vector")
	}
	vec := make([]float32, len(vector))
	for i, x := range vector {
		vec[i] = float32(x)
	}
	if len(vec) != mlBlockDim {
		fixed := make([]float32, mlBlockDim)
		n := len(vec)
		if n > mlBlockDim {
			n = mlBlockDim
		}
		copy(fixed, vec[:n])
		L2Normalize(fixed)
		vec = fixed
	} else {
		L2Normalize(vec)
	}
	if features == nil {
		features = map[string]any{}
	}
	return &Result{Features: features, Vector: vec}, nil
}

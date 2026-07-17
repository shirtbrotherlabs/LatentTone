// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package stream

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Max concurrent progressive (non-HLS) FFmpeg encodes. Rapid skip + browser
// retries otherwise pile up encodes and starve the API under Opus prefs.
const maxProgressiveEncodes = 2

// Manager runs FFmpeg HLS generation under /data/hls/{session_id}.
type Manager struct {
	HLSRoot     string
	LibraryRoot string
	FFmpegPath  string
	TTL         time.Duration
	Log         *log.Logger

	mu      sync.Mutex
	procs   map[string]*exec.Cmd
	progSem chan struct{}
}

// NewManager constructs an HLS manager.
func NewManager(hlsRoot, libraryRoot, ffmpeg string, ttl time.Duration) *Manager {
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	return &Manager{
		HLSRoot:     hlsRoot,
		LibraryRoot: libraryRoot,
		FFmpegPath:  ffmpeg,
		TTL:         ttl,
		Log:         log.Default(),
		procs:       make(map[string]*exec.Cmd),
		progSem:     make(chan struct{}, maxProgressiveEncodes),
	}
}

// SessionDir returns /data/hls/{sessionID}.
func (m *Manager) SessionDir(sessionID string) string {
	return filepath.Join(m.HLSRoot, sessionID)
}

// EnsureHLS starts (or refreshes) HLS packaging for absPath into the session dir.
// Optional EncodeOpts control audio codec/bitrate (defaults: AAC 192k).
func (m *Manager) EnsureHLS(sessionID, relTrackPath string, opts ...EncodeOpts) error {
	abs := filepath.Join(m.LibraryRoot, filepath.FromSlash(relTrackPath))
	if err := assertUnderRoot(m.LibraryRoot, abs); err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("media: %w", err)
	}
	dir := m.SessionDir(sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var enc EncodeOpts
	if len(opts) > 0 {
		enc = opts[0]
	}

	m.mu.Lock()
	if old, ok := m.procs[sessionID]; ok && old.Process != nil {
		_ = old.Process.Kill()
		delete(m.procs, sessionID)
	}
	m.mu.Unlock()

	// Clean previous segments
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}

	playlist := filepath.Join(dir, "index.m3u8")
	segPattern := filepath.Join(dir, "seg%03d.ts")
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-y",
		"-i", abs,
	}
	args = append(args, HLSAudioArgs(enc)...)
	args = append(args,
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "0",
		"-hls_segment_filename", segPattern,
		playlist,
	)
	cmd := exec.Command(m.FFmpegPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	m.mu.Lock()
	m.procs[sessionID] = cmd
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if cur, ok := m.procs[sessionID]; ok && cur == cmd {
			delete(m.procs, sessionID)
		}
		m.mu.Unlock()
		if err != nil && m.Log != nil {
			m.Log.Printf("ffmpeg session %s: %v", sessionID, err)
		}
	}()

	// Do not block callers on FFmpeg spin-up. Progressive playback covers the gap;
	// serveHLS waits briefly when a client actually requests the playlist.
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(playlist); err == nil && st.Size() > 0 {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil
}

// Stop kills FFmpeg and optionally removes the session dir.
func (m *Manager) Stop(sessionID string, removeDir bool) {
	m.mu.Lock()
	if cmd, ok := m.procs[sessionID]; ok && cmd.Process != nil {
		_ = cmd.Process.Kill()
		delete(m.procs, sessionID)
	}
	m.mu.Unlock()
	if removeDir {
		_ = os.RemoveAll(m.SessionDir(sessionID))
	}
}

// SweepTTL removes session dirs older than TTL (by mtime).
func (m *Manager) SweepTTL() {
	entries, err := os.ReadDir(m.HLSRoot)
	if err != nil {
		return
	}
	cut := time.Now().Add(-m.TTL)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(m.HLSRoot, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.ModTime().Before(cut) {
			m.Stop(e.Name(), true)
		}
	}
}

// ResolveMediaPath joins library root and relative track path safely.
func ResolveMediaPath(libraryRoot, rel string) (string, error) {
	abs := filepath.Join(libraryRoot, filepath.FromSlash(rel))
	if err := assertUnderRoot(libraryRoot, abs); err != nil {
		return "", err
	}
	return abs, nil
}

func assertUnderRoot(root, abs string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	fileAbs, err := filepath.Abs(abs)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, fileAbs)
	if err != nil || len(rel) >= 2 && rel[:2] == ".." {
		return fmt.Errorf("path escapes library root")
	}
	return nil
}

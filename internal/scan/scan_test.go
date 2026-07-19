// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-18

package scan_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/scan"
)

func writeTinyWAV(t *testing.T, path string) {
	t.Helper()
	// Minimal 8-sample mono 16-bit PCM WAV (44.1 kHz).
	const sampleRate = 44100
	const numSamples = 8
	dataSize := numSamples * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1) // mono
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFullIncrementalSkipAndForce(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	root := t.TempDir()
	trackPath := filepath.Join(root, "Artist", "Album", "01 - Song.wav")
	writeTinyWAV(t, trackPath)

	cfg := &config.Config{
		LibraryRoot: root,
		Concurrency: 2,
		Extensions:  []string{"wav"},
		CoverNames:  []string{"cover.jpg"},
		FFmpegPath:  "ffmpeg",
	}
	sc := &scan.Scanner{Cfg: cfg, DB: catalog}

	res1, err := sc.Full("test")
	if err != nil {
		t.Fatal(err)
	}
	if res1.Seen != 1 || res1.Upserted != 1 {
		t.Fatalf("first scan: %#v", res1)
	}
	if res1.Skipped != 0 {
		t.Fatalf("first scan should not skip: %#v", res1)
	}

	res2, err := sc.Full("test")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Seen != 1 {
		t.Fatalf("second scan seen=%d", res2.Seen)
	}
	if res2.Skipped != 1 {
		t.Fatalf("second scan want skipped=1 got %#v", res2)
	}
	if res2.Upserted != 0 {
		t.Fatalf("second scan should not upsert: %#v", res2)
	}

	res3, err := sc.FullOpts("test", scan.Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res3.Upserted != 1 || res3.Skipped != 0 {
		t.Fatalf("force scan: %#v", res3)
	}

	// Touch mtime → dirty again.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(trackPath, future, future); err != nil {
		t.Fatal(err)
	}
	res4, err := sc.Full("test")
	if err != nil {
		t.Fatal(err)
	}
	if res4.Upserted != 1 || res4.Skipped != 0 {
		t.Fatalf("after mtime change: %#v", res4)
	}
}

func TestFullRejectsConcurrent(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	root := t.TempDir()
	for i := 0; i < 100; i++ {
		writeTinyWAV(t, filepath.Join(root, "batch", "t"+itoa(i)+".wav"))
	}
	cfg := &config.Config{
		LibraryRoot: root,
		Concurrency: 1,
		Extensions:  []string{"wav"},
	}
	sc := &scan.Scanner{Cfg: cfg, DB: catalog}

	errCh := make(chan error, 2)
	go func() { _, err := sc.Full("a"); errCh <- err }()
	go func() { _, err := sc.Full("b"); errCh <- err }()
	err1 := <-errCh
	err2 := <-errCh
	ok := 0
	busy := 0
	for _, err := range []error{err1, err2} {
		if err == nil {
			ok++
			continue
		}
		if err == scan.ErrAlreadyRunning {
			busy++
			continue
		}
		t.Fatalf("unexpected: %v", err)
	}
	if ok < 1 || busy < 1 {
		t.Fatalf("want one success and one ErrAlreadyRunning; ok=%d busy=%d", ok, busy)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

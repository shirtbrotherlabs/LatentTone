// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-18

package extract

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNeedsSharedDecode(t *testing.T) {
	if NeedsSharedDecode([]string{"catalog", "essentia"}) {
		t.Fatal("essentia alone should not need shared decode")
	}
	if NeedsSharedDecode([]string{"yamnet"}) {
		t.Fatal("single ML extractor should skip shared decode")
	}
	if !NeedsSharedDecode([]string{"catalog", "yamnet", "musicnn"}) {
		t.Fatal("yamnet+musicnn should share decode")
	}
}

func TestDecodeSharedAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "t.wav")
	writeTinyWAV(t, wavPath)

	sa, err := DecodeSharedAudio(context.Background(), "ffmpeg", wavPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sa.Cleanup()
	fi, err := os.Stat(sa.Path)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("pcm missing: %v size=%v", err, fi)
	}
	if sa.SampleRate != 16000 {
		t.Fatalf("sr=%d", sa.SampleRate)
	}
}

func writeTinyWAV(t *testing.T, path string) {
	t.Helper()
	const sampleRate = 44100
	const numSamples = 1600
	dataSize := numSamples * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

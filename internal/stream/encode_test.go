// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package stream

import (
	"strings"
	"testing"
)

func TestIsTranscodeRangeProbe(t *testing.T) {
	cases := []struct {
		h    string
		want bool
	}{
		{"", false},
		{"bytes=0-", false},
		{"bytes=0-2047", true},
		{"bytes=100-200", true},
		{"bytes=500-", false},
		{"bytes=0-1, bytes=2-3", true},
	}
	for _, tc := range cases {
		if got := IsTranscodeRangeProbe(tc.h); got != tc.want {
			t.Fatalf("IsTranscodeRangeProbe(%q)=%v want %v", tc.h, got, tc.want)
		}
	}
}

func TestNeedsTranscodeOpus(t *testing.T) {
	if !NeedsTranscode("a.flac", "flac", EncodeOpts{Format: "opus", BitrateKbps: 160}) {
		t.Fatal("opus pref should always transcode")
	}
	if NeedsTranscode("a.flac", "flac", EncodeOpts{Format: "original"}) {
		t.Fatal("browser-safe flac should not transcode for original")
	}
}

func TestResolveEncodeTargetOpus(t *testing.T) {
	codec, format, ctype, br := ResolveEncodeTarget(EncodeOpts{Format: "opus", BitrateKbps: 128})
	if codec != "libopus" || format != "ogg" || ctype != "audio/ogg" || br != "128k" {
		t.Fatalf("got %s %s %s %s", codec, format, ctype, br)
	}
}

func TestHLSAudioArgsOpusUsesAAC(t *testing.T) {
	args := HLSAudioArgs(EncodeOpts{Format: "opus", BitrateKbps: 192})
	if len(args) < 4 || args[0] != "-vn" || args[2] != "aac" {
		t.Fatalf("want -vn + aac HLS fallback, got %#v", args)
	}
}

func TestHLSAudioArgsDropsVideo(t *testing.T) {
	for _, format := range []string{"original", "aac", "mp3", "opus"} {
		args := HLSAudioArgs(EncodeOpts{Format: format, BitrateKbps: 192})
		if len(args) == 0 || args[0] != "-vn" {
			t.Fatalf("format %q missing -vn: %#v", format, args)
		}
	}
}

func TestProgressiveFFmpegArgsLowLatency(t *testing.T) {
	args, ctype := ProgressiveFFmpegArgs("/music/a.flac", EncodeOpts{Format: "aac", BitrateKbps: 192})
	if ctype != "audio/aac" {
		t.Fatalf("ctype=%s", ctype)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-nostdin",
		"-map 0:a:0",
		"-vn",
		"-threads 1",
		"-flush_packets 1",
		"-c:a aac",
		"-f adts",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %#v", want, args)
		}
	}
}

func TestResolveEffectiveStream(t *testing.T) {
	orig := ResolveEffectiveStream("x.flac", "flac", 900, EncodeOpts{Format: "original"})
	if orig.Codec != "flac" || orig.BitrateKbps != 900 || orig.Transcoding {
		t.Fatalf("original flac %#v", orig)
	}
	opus := ResolveEffectiveStream("x.flac", "flac", 900, EncodeOpts{Format: "opus", BitrateKbps: 160})
	if opus.Codec != "opus" || opus.BitrateKbps != 160 || !opus.Transcoding {
		t.Fatalf("opus %#v", opus)
	}
	unsafe := ResolveEffectiveStream("x.wma", "wma", 320, EncodeOpts{Format: "original", BitrateKbps: 192})
	if unsafe.Codec != "mp3" || unsafe.BitrateKbps != 192 || !unsafe.Transcoding {
		t.Fatalf("unsafe original %#v", unsafe)
	}
}

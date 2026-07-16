// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package stream

import "testing"

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
	if len(args) < 2 || args[1] != "aac" {
		t.Fatalf("want aac HLS fallback, got %#v", args)
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

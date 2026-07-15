// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package tags

import "testing"

func TestPathFallbacks(t *testing.T) {
	artist, album, title := PathFallbacks("AC_DC/Highway to Hell/01 - Highway to Hell.mp3")
	if artist != "AC_DC" {
		t.Fatalf("artist=%q", artist)
	}
	if album != "Highway to Hell" {
		t.Fatalf("album=%q", album)
	}
	if title != "Highway to Hell" {
		t.Fatalf("title=%q", title)
	}
}

func TestPathFallbacksFlat(t *testing.T) {
	artist, album, title := PathFallbacks("loose-track.flac")
	if artist != "" || album != "" {
		t.Fatalf("expected empty artist/album, got %q %q", artist, album)
	}
	if title != "loose-track" {
		t.Fatalf("title=%q", title)
	}
}

func TestSplitArtists(t *testing.T) {
	got := splitArtists("A feat. B")
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("%v", got)
	}
}

func TestStripTrackPrefix(t *testing.T) {
	cases := map[string]string{
		"01 - Song":   "Song",
		"02. Song":    "Song",
		"3) Song":     "Song",
		"Song":        "Song",
		"12 - X - Y":  "X - Y",
	}
	for in, want := range cases {
		if got := stripTrackPrefix(in); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
	}
}

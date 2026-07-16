// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package tags

import (
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

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
		"01 - Song":  "Song",
		"02. Song":   "Song",
		"3) Song":    "Song",
		"Song":       "Song",
		"12 - X - Y": "X - Y",
	}
	for in, want := range cases {
		if got := stripTrackPrefix(in); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
	}
}

func TestApplyProbeJSONFillsMissingDurationAndYear(t *testing.T) {
	in := db.TrackInput{}
	err := applyProbeJSON([]byte(`{
		"format": {
			"duration": "243.456000",
			"tags": {"DATE": "2004-10-04"}
		}
	}`), &in)
	if err != nil {
		t.Fatal(err)
	}
	if in.DurationMS == nil || *in.DurationMS != 243456 {
		t.Fatalf("duration=%v", in.DurationMS)
	}
	if in.Year == nil || *in.Year != 2004 {
		t.Fatalf("year=%v", in.Year)
	}
	if in.AlbumYear == nil || *in.AlbumYear != 2004 {
		t.Fatalf("album year=%v", in.AlbumYear)
	}
}

func TestApplyProbeJSONPreservesNativeValues(t *testing.T) {
	duration := int64(1000)
	year := 1999
	in := db.TrackInput{DurationMS: &duration, Year: &year}
	if err := applyProbeJSON([]byte(`{
		"format": {"duration": "20.0", "tags": {"year": "2020"}}
	}`), &in); err != nil {
		t.Fatal(err)
	}
	if *in.DurationMS != 1000 || *in.Year != 1999 {
		t.Fatalf("native values overwritten: duration=%d year=%d", *in.DurationMS, *in.Year)
	}
}

func TestParseYear(t *testing.T) {
	for raw, want := range map[string]int{
		"2004":          2004,
		"2004-10-04":    2004,
		"released 1971": 1971,
	} {
		got, ok := parseYear(raw)
		if !ok || got != want {
			t.Errorf("parseYear(%q)=(%d,%v), want %d", raw, got, ok, want)
		}
	}
	if _, ok := parseYear("unknown"); ok {
		t.Fatal("unexpected year from unknown")
	}
}

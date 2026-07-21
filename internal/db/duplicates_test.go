// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db_test

import (
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Hello, World!", "hello world"},
		{"Where's Your Head At", "wheres your head at"},
		{"Boy From School (Album Version)", "boy from school album version"},
		{"", ""},
	}
	for _, c := range cases {
		if got := db.NormalizeTag(c.in); got != c.want {
			t.Fatalf("NormalizeTag(%q)=%q want %q", c.in, got, c.want)
		}
	}
	if db.DuplicateKey("A", "B", "C") != db.DuplicateKey("a!", "b.", "c?") {
		t.Fatal("keys should match ignoring punctuation/case")
	}
}

func TestListDuplicateGroups(t *testing.T) {
	d, _ := dbtest.Open(t)
	ms := int64(162682)
	msFar := int64(300000)
	year := 1997

	_, err := d.UpsertTrack(db.TrackInput{
		Path: "va/ap/soul.mp3", FileMtime: 1, FileSize: 100,
		Title: "Soul Bossa Nova", Album: "Austin Powers", AlbumArtist: "Various Artists",
		Artists: []string{"Various Artists"}, DurationMS: &ms, Format: "mp3", Year: &year, AlbumYear: &year,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.UpsertTrack(db.TrackInput{
		Path: "va/ap/soul (1).mp3", FileMtime: 1, FileSize: 101,
		Title: "Soul Bossa Nova!", Album: "Austin Powers", AlbumArtist: "various artists",
		Artists: []string{"various artists"}, DurationMS: &ms, Format: "mp3", Year: &year, AlbumYear: &year,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Same tags but duration 2+ minutes apart — must not join the group.
	_, err = d.UpsertTrack(db.TrackInput{
		Path: "va/ap/soul-long.mp3", FileMtime: 1, FileSize: 102,
		Title: "Soul Bossa Nova", Album: "Austin Powers", AlbumArtist: "Various Artists",
		Artists: []string{"Various Artists"}, DurationMS: &msFar, Format: "mp3", Year: &year, AlbumYear: &year,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Unrelated singleton
	ms2 := int64(180000)
	_, err = d.UpsertTrack(db.TrackInput{
		Path: "other/x.mp3", FileMtime: 1, FileSize: 50,
		Title: "Unique Song", Album: "Solo", AlbumArtist: "Solo Artist",
		Artists: []string{"Solo Artist"}, DurationMS: &ms2, Format: "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}

	groups, err := d.ListDuplicateGroups(50)
	if err != nil {
		t.Fatal(err)
	}
	var match *db.DuplicateGroup
	for i := range groups {
		if db.NormalizeTag(groups[i].Title) == "soul bossa nova" {
			match = &groups[i]
			break
		}
	}
	if match == nil {
		t.Fatalf("expected Soul Bossa Nova group, got %#v", groups)
	}
	if len(match.Tracks) != 2 {
		t.Fatalf("want 2 near-duration dupes, got %d (%+v)", len(match.Tracks), match.Tracks)
	}
	for _, tr := range match.Tracks {
		if tr.DurationMS == msFar {
			t.Fatal("far-duration copy should not be in near group")
		}
	}
}

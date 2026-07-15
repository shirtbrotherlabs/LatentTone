// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"path/filepath"
	"testing"
)

func TestOpenMigrateUpsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tn := 1
	year := 1979
	ms := int64(208000)
	id, err := d.UpsertTrack(TrackInput{
		Path:        "AC_DC/Highway to Hell/01 - Highway to Hell.mp3",
		FileMtime:   1,
		FileSize:    100,
		Title:       "Highway to Hell",
		Album:       "Highway to Hell",
		AlbumArtist: "AC/DC",
		Artists:     []string{"AC/DC"},
		Genres:      []string{"Rock"},
		TrackNumber: &tn,
		DurationMS:  &ms,
		Format:      "mp3",
		Year:        &year,
		AlbumYear:   &year,
		CoverPath:   "AC_DC/Highway to Hell/cover.jpg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("expected id")
	}

	// Idempotent second upsert
	id2, err := d.UpsertTrack(TrackInput{
		Path:        "AC_DC/Highway to Hell/01 - Highway to Hell.mp3",
		FileMtime:   2,
		FileSize:    101,
		Title:       "Highway to Hell",
		Album:       "Highway to Hell",
		AlbumArtist: "AC/DC",
		Artists:     []string{"AC/DC"},
		Format:      "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Fatalf("id changed %d → %d", id, id2)
	}

	artists, albums, tracks, err := d.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if artists < 1 || albums < 1 || tracks != 1 {
		t.Fatalf("counts artists=%d albums=%d tracks=%d", artists, albums, tracks)
	}

	seen := map[string]struct{}{"other/path.mp3": {}}
	missing, err := d.MarkMissingExcept(seen)
	if err != nil {
		t.Fatal(err)
	}
	if missing != 1 {
		t.Fatalf("missing=%d", missing)
	}
	_, _, tracks, err = d.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if tracks != 0 {
		t.Fatalf("expected 0 non-missing, got %d", tracks)
	}
}

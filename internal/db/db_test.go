// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db_test

import (
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestOpenMigrateUpsert(t *testing.T) {
	d, _ := dbtest.Open(t)

	tn := 1
	year := 1979
	ms := int64(208000)
	id, err := d.UpsertTrack(db.TrackInput{
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

	batchResults, err := d.UpsertTracks([]db.TrackInput{
		{
			Path:        "AC_DC/Highway to Hell/02 - Girls Got Rhythm.mp3",
			FileMtime:   1,
			FileSize:    200,
			Title:       "Girls Got Rhythm",
			Album:       "Highway to Hell",
			AlbumArtist: "AC/DC",
			Artists:     []string{"AC/DC"},
			Format:      "mp3",
			Year:        &year,
			AlbumYear:   &year,
		},
		{
			Path:        "AC_DC/Highway to Hell/03 - Walk All Over You.mp3",
			FileMtime:   1,
			FileSize:    201,
			Title:       "Walk All Over You",
			Album:       "Highway to Hell",
			AlbumArtist: "AC/DC",
			Artists:     []string{"AC/DC"},
			Format:      "mp3",
			Year:        &year,
			AlbumYear:   &year,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batchResults) != 2 || batchResults[0].Err != nil || batchResults[1].Err != nil {
		t.Fatalf("batch upsert: %+v", batchResults)
	}

	cached := db.TrackInput{
		Path:      "AC_DC/Highway to Hell/01 - Highway to Hell.mp3",
		FileMtime: 1,
		FileSize:  100,
	}
	found, err := d.ReuseScanMetadata(&cached)
	if err != nil {
		t.Fatal(err)
	}
	if !found || cached.DurationMS == nil || *cached.DurationMS != ms ||
		cached.Year == nil || *cached.Year != year {
		t.Fatalf("cached metadata not reused: found=%v duration=%v year=%v", found, cached.DurationMS, cached.Year)
	}

	// Idempotent second upsert
	id2, err := d.UpsertTrack(db.TrackInput{
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
	track, err := d.GetTrack(id)
	if err != nil {
		t.Fatal(err)
	}
	// The second upsert intentionally omitted track year; catalog reads should
	// fall back to the retained album year.
	if track == nil || !track.Year.Valid || track.Year.Int64 != int64(year) {
		t.Fatalf("album year fallback missing: track=%+v", track)
	}

	artists, albums, tracks, err := d.Counts()
	if err != nil {
		t.Fatal(err)
	}
	if artists < 1 || albums < 1 || tracks != 3 {
		t.Fatalf("counts artists=%d albums=%d tracks=%d", artists, albums, tracks)
	}

	seen := map[string]struct{}{"other/path.mp3": {}}
	missing, err := d.MarkMissingExcept(seen)
	if err != nil {
		t.Fatal(err)
	}
	if missing != 3 {
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

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package playlist

import (
	"context"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestCreateFromSeed(t *testing.T) {
	catalog, _ := dbtest.Open(t)

	mk := func(path, title string, vec []float32) int64 {
		tn := 1
		id, err := catalog.UpsertTrack(db.TrackInput{
			Path: path, Title: title, Album: "Alb", AlbumArtist: "Art",
			Artists: []string{"Art"}, Format: "mp3", TrackNumber: &tn, FileMtime: 1, FileSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catalog.EnsureVectorRows("t", `{}`); err != nil {
			t.Fatal(err)
		}
		if err := catalog.MarkVectorReady(id, "t", `{}`, vec, 1, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}

	seed := mk("a/s.mp3", "Seed", []float32{1, 0, 0})
	near := mk("a/n.mp3", "Near", []float32{0.9, 0.1, 0})
	far := mk("a/f.mp3", "Far", []float32{0, 1, 0})
	_ = far

	res, err := CreateFromSeed(context.Background(), catalog, nil, seed, Options{Length: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Playlist == nil || !res.Playlist.SeedTrackID.Valid || res.Playlist.SeedTrackID.Int64 != seed {
		t.Fatalf("bad playlist %+v", res.Playlist)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("want 3 entries got %d", len(res.Entries))
	}
	if res.Entries[0].TrackID != seed {
		t.Fatalf("seed first got %d", res.Entries[0].TrackID)
	}
	if res.Entries[1].TrackID != near {
		t.Fatalf("near second got %d", res.Entries[1].TrackID)
	}
}

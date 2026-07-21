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

func TestSearchSuggestAndSeeds(t *testing.T) {
	d, _ := dbtest.Open(t)
	ms := int64(200000)
	year := 2000
	_, err := d.UpsertTrack(db.TrackInput{
		Path: "ArtistX/AlbumY/01 SongZ.mp3", FileMtime: 1, FileSize: 10,
		Title: "SongZ Unique", Album: "AlbumY", AlbumArtist: "ArtistX",
		Artists: []string{"ArtistX"}, Genres: []string{"Indie Rock"},
		DurationMS: &ms, Format: "mp3", Year: &year, AlbumYear: &year,
	})
	if err != nil {
		t.Fatal(err)
	}

	hits, err := d.SearchSuggest("SongZ", 12)
	if err != nil {
		t.Fatal(err)
	}
	foundTrack := false
	for _, h := range hits {
		if h.Kind == "track" && h.Label == "SongZ Unique" {
			foundTrack = true
		}
	}
	if !foundTrack {
		t.Fatalf("expected track hit, got %#v", hits)
	}

	genres, err := d.ListGenres(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(genres) < 1 || genres[0].Name != "Indie Rock" {
		t.Fatalf("genres %#v", genres)
	}

	artists, err := d.ListArtists()
	if err != nil || len(artists) < 1 {
		t.Fatalf("artists %v %v", artists, err)
	}
	seed, err := d.PickSeedTrackID(artists[0].ID, 0, 0, "")
	if err != nil || seed <= 0 {
		t.Fatalf("artist seed %d %v", seed, err)
	}
	seed2, err := d.PickSeedTrackID(0, genres[0].ID, 0, "")
	if err != nil || seed2 <= 0 {
		t.Fatalf("genre seed %d %v", seed2, err)
	}
}

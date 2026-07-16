// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package session_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/session"
)

func TestFillQueueArtistCooldown(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	u, err := catalog.CreateUser("u", "hash")
	if err != nil {
		t.Fatal(err)
	}
	seed, err := catalog.UpsertTrack(db.TrackInput{
		Path: "a/seed.mp3", Title: "Seed", Album: "A", AlbumArtist: "Alpha",
		Artists: []string{"Alpha"}, Format: "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	same, err := catalog.UpsertTrack(db.TrackInput{
		Path: "a/same.mp3", Title: "Same", Album: "A", AlbumArtist: "Alpha",
		Artists: []string{"Alpha"}, Format: "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := catalog.UpsertTrack(db.TrackInput{
		Path: "b/other.mp3", Title: "Other", Album: "B", AlbumArtist: "Beta",
		Artists: []string{"Beta"}, Format: "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}

	w := session.NewWorker(catalog, nil, 4, 1)
	w.Rand = rand.New(rand.NewSource(1))
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return []affinity.Neighbor{
			{TrackID: same, Score: 0.99},
			{TrackID: other, Score: 0.5},
		}, nil
	}

	live, err := w.Create(context.Background(), u.ID, seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Queue) == 0 {
		t.Fatal("empty queue")
	}
	if live.Queue[0] != other {
		t.Fatalf("cooldown should prefer Beta, got queue=%v", live.Queue)
	}
}

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package session

import (
	"context"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

func TestFillQueueBridgeCadence(t *testing.T) {
	dir := t.TempDir()
	catalog, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
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
	n1, err := catalog.UpsertTrack(db.TrackInput{
		Path: "a/n1.mp3", Title: "N1", Album: "A", AlbumArtist: "Alpha",
		Artists: []string{"Alpha"}, Format: "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	liked, err := catalog.UpsertTrack(db.TrackInput{
		Path: "c/liked.mp3", Title: "Liked", Album: "C", AlbumArtist: "Gamma",
		Artists: []string{"Gamma"}, Format: "mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.UpsertAffinity(u.ID, liked, 0.8); err != nil {
		t.Fatal(err)
	}

	w := NewWorker(catalog, nil, 4, 1)
	w.Rand = rand.New(rand.NewSource(3))
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return []affinity.Neighbor{{TrackID: n1, Score: 0.9}}, nil
	}

	live := &Live{
		ID:                "s1",
		UserID:            u.ID,
		SeedTrackID:       seed,
		Status:            db.SessionStatusPlaying,
		NowPlayingID:      seed,
		Pinned:            map[int64]struct{}{},
		Sources:           map[int64]string{},
		Recent:            []int64{seed},
		ArtistPenalties:   map[string]float64{},
		TracksSinceBridge: 6,
		BridgeInterval:    6,
	}
	if err := w.fillQueue(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	if len(live.Queue) == 0 || live.Queue[0] != liked {
		t.Fatalf("want bridge track first, got %v", live.Queue)
	}
	if live.Sources[liked] != "radio_bridge" {
		t.Fatalf("source=%q", live.Sources[liked])
	}
	if !live.BridgeQueued {
		t.Fatal("BridgeQueued should be set")
	}
}

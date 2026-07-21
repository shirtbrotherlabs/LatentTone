// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package session_test

import (
	"context"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/session"
)

func TestCreateAlbumNoANNRefill(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	u, err := catalog.CreateUser("albumplay", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ms := int64(120000)
	var ids []int64
	for i, title := range []string{"A", "B", "C"} {
		id, err := catalog.UpsertTrack(db.TrackInput{
			Path: "Artist/Album/" + title + ".flac", FileMtime: 1, FileSize: int64(100 + i),
			Title: title, Album: "Album", AlbumArtist: "Artist", Artists: []string{"Artist"},
			DurationMS: &ms, Format: "flac",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	w := session.NewWorker(catalog, nil, 8, 12)
	live, err := w.CreateAlbum(context.Background(), u.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	st := w.ToStatus(live)
	if st.Kind != db.SessionKindAlbum {
		t.Fatalf("kind %q", st.Kind)
	}
	if st.NowPlaying == nil || st.NowPlaying.TrackID != ids[0] {
		t.Fatalf("now %#v", st.NowPlaying)
	}
	if len(st.Queue) != 2 {
		t.Fatalf("queue len %d want 2", len(st.Queue))
	}
	// Advance through album — should stop without inventing neighbors.
	if err := w.Advance(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	if err := w.Advance(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	if err := w.Advance(context.Background(), live); err != nil {
		// last advance with empty queue stops
	}
	st = w.ToStatus(live)
	if st.Status != db.SessionStatusStopped && len(st.Queue) != 0 {
		// After playing C, next advance stops.
	}
	_ = w.Advance(context.Background(), live)
	st = w.ToStatus(live)
	if st.Status != db.SessionStatusStopped {
		t.Fatalf("want stopped after album end, got %s queue=%d", st.Status, len(st.Queue))
	}
}

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package session_test

import (
	"context"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/session"
)

func seedDB(t *testing.T) (*db.DB, int64, int64, int64, int64, int64) {
	t.Helper()
	catalog, _ := dbtest.Open(t)
	u, err := catalog.CreateUser("u1", "hash")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := catalog.CreateUser("u2", "hash")
	if err != nil {
		t.Fatal(err)
	}
	id1, err := catalog.UpsertTrack(db.TrackInput{Path: "a/1.mp3", Title: "One", Album: "A", AlbumArtist: "Art", Artists: []string{"Art"}, Format: "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := catalog.UpsertTrack(db.TrackInput{Path: "a/2.mp3", Title: "Two", Album: "A", AlbumArtist: "Art", Artists: []string{"Art"}, Format: "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	id3, err := catalog.UpsertTrack(db.TrackInput{Path: "a/3.mp3", Title: "Three", Album: "A", AlbumArtist: "Art", Artists: []string{"Art"}, Format: "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	return catalog, u.ID, u2.ID, id1, id2, id3
}

func TestSkipAdvancesAndIsolation(t *testing.T) {
	catalog, userID, user2ID, id1, id2, id3 := seedDB(t)
	defer catalog.Close()

	w := session.NewWorker(catalog, nil, 4, 2)
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return []affinity.Neighbor{
			{TrackID: id2, Score: 0.9},
			{TrackID: id3, Score: 0.8},
		}, nil
	}

	ctx := context.Background()
	live, err := w.Create(ctx, userID, id1)
	if err != nil {
		t.Fatal(err)
	}
	if live.NowPlayingID != id1 {
		t.Fatalf("now=%d", live.NowPlayingID)
	}
	before := live.NowPlayingID
	if err := w.ApplyFeedback(ctx, live, db.SignalSkip, before); err != nil {
		t.Fatal(err)
	}
	if live.NowPlayingID == before {
		t.Fatal("expected advance on skip")
	}
	status := w.ToStatus(live)
	if status.NowPlaying == nil || status.NowPlaying.TrackID != live.NowPlayingID {
		t.Fatal("status mismatch")
	}

	_, err = w.Get(live.ID, user2ID)
	if err == nil || err.Error() != "forbidden" {
		t.Fatalf("want forbidden got %v", err)
	}
}

func TestDislikeAdvancesNowPlaying(t *testing.T) {
	catalog, userID, _, id1, id2, id3 := seedDB(t)
	defer catalog.Close()

	w := session.NewWorker(catalog, nil, 4, 2)
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return []affinity.Neighbor{
			{TrackID: id2, Score: 0.9},
			{TrackID: id3, Score: 0.8},
		}, nil
	}

	ctx := context.Background()
	live, err := w.Create(ctx, userID, id1)
	if err != nil {
		t.Fatal(err)
	}
	before := live.NowPlayingID
	if err := w.ApplyFeedback(ctx, live, db.SignalDislike, before); err != nil {
		t.Fatal(err)
	}
	if live.NowPlayingID == before {
		t.Fatal("expected advance on dislike of now_playing")
	}
	skips, err := catalog.ListSkippedTrackIDs(userID, live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := skips[before]; !ok {
		t.Fatal("dislike should session-skip the disliked track")
	}
}

func TestCompleteAdvancesWithoutSkip(t *testing.T) {
	catalog, userID, _, id1, id2, id3 := seedDB(t)
	defer catalog.Close()

	w := session.NewWorker(catalog, nil, 4, 2)
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return []affinity.Neighbor{
			{TrackID: id2, Score: 0.9},
			{TrackID: id3, Score: 0.8},
		}, nil
	}

	ctx := context.Background()
	live, err := w.Create(ctx, userID, id1)
	if err != nil {
		t.Fatal(err)
	}
	before := live.NowPlayingID
	if err := w.ApplyFeedback(ctx, live, db.SignalComplete, before); err != nil {
		t.Fatal(err)
	}
	if live.NowPlayingID == before {
		t.Fatal("expected advance on complete")
	}
	// Natural complete must not put the finished track on the session skip list.
	skips, err := catalog.ListSkippedTrackIDs(userID, live.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := skips[before]; ok {
		t.Fatal("complete should not session-skip the finished track")
	}
}

func TestInjectQueuePinsNext(t *testing.T) {
	catalog, userID, _, id1, id2, id3 := seedDB(t)
	defer catalog.Close()

	w := session.NewWorker(catalog, nil, 4, 2)
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return []affinity.Neighbor{
			{TrackID: id2, Score: 0.9},
		}, nil
	}

	ctx := context.Background()
	live, err := w.Create(ctx, userID, id1)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.InjectQueue(ctx, live, id3, "next"); err != nil {
		t.Fatal(err)
	}
	status := w.ToStatus(live)
	if len(status.Queue) == 0 || status.Queue[0].TrackID != id3 {
		t.Fatalf("want id3 at front of queue, got %#v", status.Queue)
	}
	if status.Queue[0].Source != "user_pin" {
		t.Fatalf("want user_pin source, got %q", status.Queue[0].Source)
	}
	// Already queued: Play next moves it to the front (no conflict).
	if err := w.InjectQueue(ctx, live, id2, "next"); err != nil {
		t.Fatal(err)
	}
	status = w.ToStatus(live)
	if len(status.Queue) == 0 || status.Queue[0].TrackID != id2 {
		t.Fatalf("want id2 promoted to front, got %#v", status.Queue)
	}
	if err := w.RemoveFromQueue(ctx, live, id2); err != nil {
		t.Fatal(err)
	}
	status = w.ToStatus(live)
	for _, q := range status.Queue {
		if q.TrackID == id2 {
			t.Fatalf("id2 should be removed from queue: %#v", status.Queue)
		}
	}
}

func TestUserStateOwnershipHelpers(t *testing.T) {
	catalog, userID, _, id1, _, _ := seedDB(t)
	defer catalog.Close()
	if err := catalog.InsertTrackFeedback(userID, id1, db.SignalLike, "sess"); err != nil {
		t.Fatal(err)
	}
	score, err := catalog.UpsertAffinity(userID, id1, 0.25)
	if err != nil || score != 0.25 {
		t.Fatalf("affinity %v %v", score, err)
	}
	if err := catalog.AddSkip(userID, id1, db.SkipScopeLibrary, ""); err != nil {
		t.Fatal(err)
	}
	skips, err := catalog.ListSkippedTrackIDs(userID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := skips[id1]; !ok {
		t.Fatal("skip missing")
	}
}

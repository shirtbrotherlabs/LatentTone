// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-21

package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestReclaimIdleStopsStaleSession(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	u, err := catalog.CreateUser("idleuser", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ms := int64(120000)
	tid, err := catalog.UpsertTrack(db.TrackInput{
		Path: "A/B/c.flac", FileMtime: 1, FileSize: 10,
		Title: "C", Album: "B", AlbumArtist: "A", Artists: []string{"A"},
		DurationMS: &ms, Format: "flac",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(catalog, nil, 8, 4)
	w.IdleTTL = 50 * time.Millisecond
	live, err := w.Create(context.Background(), u.ID, tid)
	if err != nil {
		t.Fatal(err)
	}
	live.mu.Lock()
	live.LastActive = time.Now().UTC().Add(-time.Second)
	live.mu.Unlock()
	n := w.ReclaimIdle()
	if n < 1 {
		t.Fatalf("expected reclaim, got %d", n)
	}
	st := w.ToStatus(live)
	if st.Status != db.SessionStatusStopped {
		t.Fatalf("status %q", st.Status)
	}
}

func TestEnsureCapacityEvictsOldestForUser(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	u, err := catalog.CreateUser("capuser", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ms := int64(120000)
	var ids []int64
	for i := 0; i < 4; i++ {
		id, err := catalog.UpsertTrack(db.TrackInput{
			Path: fmt.Sprintf("Cap/Album/t%d.flac", i), FileMtime: 1, FileSize: int64(10 + i),
			Title: fmt.Sprintf("T%d", i), Album: "Album", AlbumArtist: "Cap", Artists: []string{"Cap"},
			DurationMS: &ms, Format: "flac",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	w := NewWorker(catalog, nil, 64, 2)
	w.MaxPerUser = 2
	w.IdleTTL = time.Hour

	first, err := w.Create(context.Background(), u.ID, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	second, err := w.Create(context.Background(), u.ID, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	third, err := w.Create(context.Background(), u.ID, ids[2])
	if err != nil {
		t.Fatal(err)
	}
	if w.ToStatus(first).Status != db.SessionStatusStopped {
		t.Fatalf("oldest should be evicted, got %s", w.ToStatus(first).Status)
	}
	if w.ToStatus(second).Status == db.SessionStatusStopped {
		t.Fatal("second should still be live")
	}
	if w.ToStatus(third).Status == db.SessionStatusStopped {
		t.Fatal("newest should be live")
	}
}

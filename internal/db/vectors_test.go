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

func TestVectorQueueAndFeatures(t *testing.T) {
	d, _ := dbtest.Open(t)

	tn := 1
	id, err := d.UpsertTrack(db.TrackInput{
		Path: "A/B/c.mp3", Title: "Song", Album: "Alb", AlbumArtist: "Art",
		Artists: []string{"Art"}, Format: "mp3", TrackNumber: &tn, FileMtime: 10, FileSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := d.EnsureVectorRows("catalog+filesig", `{"catalog":"1"}`)
	if err != nil || n != 1 {
		t.Fatalf("ensure n=%d err=%v", n, err)
	}
	ids, err := d.ClaimVectorWork(10, "all", 0)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("claim %v err=%v", ids, err)
	}
	if err := d.SaveTrackFeatures(id, "catalog", "1", `{"x":1}`, 32); err != nil {
		t.Fatal(err)
	}
	vec := []float32{1, 0, 0}
	if err := d.MarkVectorReady(id, "catalog+filesig", `{"catalog":"1"}`, vec, 10, ""); err != nil {
		t.Fatal(err)
	}
	v, err := d.GetTrackVector(id)
	if err != nil || v == nil || v.Status != db.VecReady || len(v.Embedding) != 3 {
		t.Fatalf("%+v err=%v", v, err)
	}
	feats, err := d.ListTrackFeatures(id)
	if err != nil || len(feats) != 1 {
		t.Fatalf("%v err=%v", feats, err)
	}
	counts, err := d.ExtractorFeatureCounts([]string{"catalog", "essentia", "yamnet"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["catalog"] != 1 || counts["essentia"] != 0 || counts["yamnet"] != 0 {
		t.Fatalf("feature counts: %#v", counts)
	}

	// mtime drift → stale → pending
	_, _ = d.SQL.Exec(`UPDATE tracks SET file_mtime = 99 WHERE id = ?`, id)
	if _, err := d.MarkStaleByConfig("catalog+filesig", `{"catalog":"1"}`); err != nil {
		t.Fatal(err)
	}
	v, _ = d.GetTrackVector(id)
	if v.Status != db.VecPending {
		t.Fatalf("want pending after stale, got %s", v.Status)
	}
}

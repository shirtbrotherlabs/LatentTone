// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package affinity

import (
	"context"
	"math"
	"math/rand"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestCosineOrthonormal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	if s := Cosine(a, b); math.Abs(s) > 1e-6 {
		t.Fatalf("got %v", s)
	}
	if s := Cosine(a, a); math.Abs(s-1) > 1e-6 {
		t.Fatalf("self %v", s)
	}
}

func TestCosineKnownAngle(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{float32(math.Cos(math.Pi / 4)), float32(math.Sin(math.Pi / 4))}
	s := Cosine(a, b)
	want := math.Cos(math.Pi / 4)
	if math.Abs(s-want) > 1e-5 {
		t.Fatalf("got %v want %v", s, want)
	}
}

func TestNeighborsByVectorJitterChangesRanking(t *testing.T) {
	catalog, _ := dbtest.Open(t)

	mk := func(path, title string, vec []float32) int64 {
		t.Helper()
		id, err := catalog.UpsertTrack(db.TrackInput{
			Path: path, Title: title, Album: "A", AlbumArtist: "Art",
			Artists: []string{"Art"}, Format: "mp3",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catalog.EnsureVectorRows("test", `{}`); err != nil {
			t.Fatal(err)
		}
		if err := catalog.MarkVectorReady(id, "test", `{}`, vec, 1, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	seed := mk("a/seed.mp3", "Seed", []float32{1, 0, 0})
	_ = mk("a/near.mp3", "Near", []float32{0.99, 0.1, 0})
	far := mk("a/far.mp3", "Far", []float32{0, 1, 0})

	base, err := NeighborsWithStore(context.Background(), catalog, nil, seed, 2)
	if err != nil || len(base) == 0 {
		t.Fatalf("base neighbors: %v %#v", err, base)
	}
	rng := rand.New(rand.NewSource(99))
	seedVec, err := catalog.GetTrackVector(seed)
	if err != nil || seedVec == nil {
		t.Fatal(err)
	}
	// Strong jitter toward the far axis should surface Far among results.
	q := JitterVector(seedVec.Embedding, 0, rng)
	q[0] = 0.1
	q[1] = 0.9
	jittered, err := NeighborsByVector(context.Background(), catalog, nil, q, seed, 2)
	if err != nil {
		t.Fatal(err)
	}
	foundFar := false
	for _, n := range jittered {
		if n.TrackID == far {
			foundFar = true
		}
	}
	if !foundFar {
		t.Fatalf("jittered query should find Far, got %#v (base=%#v)", jittered, base)
	}
}

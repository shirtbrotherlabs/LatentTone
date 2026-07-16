// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package affinity

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/lance"
)

// Neighbor is a similar track with cosine score.
type Neighbor struct {
	TrackID int64
	Score   float64
}

// Cosine returns cosine similarity of two equal-length vectors.
func Cosine(a, b []float32) float64 {
	n := len(a)
	if n == 0 || n != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Similarity loads two ready embeddings and returns cosine.
func Similarity(catalog *db.DB, aID, bID int64) (float64, error) {
	a, err := catalog.GetTrackVector(aID)
	if err != nil {
		return 0, err
	}
	b, err := catalog.GetTrackVector(bID)
	if err != nil {
		return 0, err
	}
	if a == nil || b == nil || a.Status != db.VecReady || b.Status != db.VecReady {
		return 0, fmt.Errorf("both tracks must have ready embeddings")
	}
	return Cosine(a.Embedding, b.Embedding), nil
}

// Neighbors returns top-k cosine neighbors for trackID (excludes self).
// When store is enabled, LanceDB ANN is tried first; falls back to a flat
// in-process cosine scan over catalog-stored embeddings.
func Neighbors(catalog *db.DB, trackID int64, k int) ([]Neighbor, error) {
	return NeighborsWithStore(context.Background(), catalog, nil, trackID, k)
}

// NeighborsWithStore is Neighbors with an optional LanceDB store.
func NeighborsWithStore(ctx context.Context, catalog *db.DB, store *lance.Store, trackID int64, k int) ([]Neighbor, error) {
	if k <= 0 {
		k = 10
	}
	seed, err := catalog.GetTrackVector(trackID)
	if err != nil {
		return nil, err
	}
	if seed == nil || seed.Status != db.VecReady || len(seed.Embedding) == 0 {
		return nil, fmt.Errorf("track %d has no ready embedding", trackID)
	}
	return NeighborsByVector(ctx, catalog, store, seed.Embedding, trackID, k)
}

// NeighborsByVector returns top-k cosine neighbors for an arbitrary query vector.
// Used for Radio query jittering (V_query = V_seed + α·ε) without mutating LanceDB.
func NeighborsByVector(ctx context.Context, catalog *db.DB, store *lance.Store, query []float32, excludeTrackID int64, k int) ([]Neighbor, error) {
	if k <= 0 {
		k = 10
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("empty query vector")
	}

	if store != nil && store.Enabled() {
		hits, err := store.Search(ctx, query, k, excludeTrackID)
		if err == nil && len(hits) > 0 {
			out := make([]Neighbor, 0, len(hits))
			for _, h := range hits {
				out = append(out, Neighbor{TrackID: h.TrackID, Score: h.Score})
			}
			return out, nil
		}
		// fall through to the flat catalog scan on empty / error
	}

	all, err := catalog.ListReadyEmbeddings()
	if err != nil {
		return nil, err
	}
	var out []Neighbor
	for _, e := range all {
		if e.TrackID == excludeTrackID {
			continue
		}
		if len(e.Vector) != len(query) {
			continue
		}
		out = append(out, Neighbor{TrackID: e.TrackID, Score: Cosine(query, e.Vector)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

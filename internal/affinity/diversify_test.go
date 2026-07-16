// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package affinity

import (
	"math"
	"math/rand"
	"testing"
)

func TestJitterVectorChangesQuery(t *testing.T) {
	seed := []float32{1, 0, 0, 0}
	rng := rand.New(rand.NewSource(42))
	j := JitterVector(seed, 0.1, rng)
	if len(j) != len(seed) {
		t.Fatalf("len %d", len(j))
	}
	same := true
	for i := range seed {
		if j[i] != seed[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("expected jitter to change vector")
	}
	// seed must be unchanged
	if seed[0] != 1 {
		t.Fatal("seed mutated")
	}
	zero := JitterVector(seed, 0, rng)
	if zero[0] != 1 || zero[1] != 0 {
		t.Fatalf("alpha=0 should copy: %#v", zero)
	}
}

func TestCooldownSkipsSameArtist(t *testing.T) {
	pool := []Candidate{
		{TrackID: 1, Artist: "Alpha", BaseScore: 0.99},
		{TrackID: 2, Artist: "Alpha", BaseScore: 0.98},
		{TrackID: 3, Artist: "Beta", BaseScore: 0.5},
	}
	rng := rand.New(rand.NewSource(1))
	res := SelectDiversified(pool, SelectOpts{
		Need:            1,
		ArtistCooldown:  true,
		CooldownWindow:  3,
		RecentArtists:   []string{"Alpha"},
		BoundedRandom:   false,
		ArtistPenalty:   false,
		RNG:             rng,
	})
	if len(res.Picks) != 1 || res.Picks[0].TrackID != 3 {
		t.Fatalf("want Beta track, got %#v", res.Picks)
	}
}

func TestArtistPenaltyRanksOtherArtistAhead(t *testing.T) {
	pool := []Candidate{
		{TrackID: 1, Artist: "Alpha", BaseScore: 0.9},
		{TrackID: 2, Artist: "Beta", BaseScore: 0.55},
	}
	penalties := map[string]float64{"alpha": 0.5}
	ApplyArtistPenalties(pool, penalties)
	if pool[0].Score >= pool[1].Score {
		t.Fatalf("after penalty Alpha=%v should be behind Beta=%v", pool[0].Score, pool[1].Score)
	}
	rng := rand.New(rand.NewSource(1))
	res := SelectDiversified([]Candidate{
		{TrackID: 1, Artist: "Alpha", BaseScore: 0.9},
		{TrackID: 2, Artist: "Beta", BaseScore: 0.55},
	}, SelectOpts{
		Need:            1,
		ArtistPenalty:   true,
		ArtistPenalties: map[string]float64{"alpha": 0.5},
		BoundedRandom:   false,
		ArtistCooldown:  false,
		RNG:             rng,
	})
	if len(res.Picks) != 1 || res.Picks[0].TrackID != 2 {
		t.Fatalf("want Beta, got %#v", res.Picks)
	}
}

func TestOnArtistPlayedDecay(t *testing.T) {
	p := map[string]float64{"alpha": 0.5}
	p = OnArtistPlayed(p, "Beta", 0.5, 0.1)
	if math.Abs(p["alpha"]-0.4) > 1e-9 {
		t.Fatalf("alpha decay: %v", p["alpha"])
	}
	if math.Abs(p["beta"]-0.5) > 1e-9 {
		t.Fatalf("beta boost: %v", p["beta"])
	}
	for i := 0; i < 5; i++ {
		p = OnArtistPlayed(p, "Gamma", 0.5, 0.1)
	}
	if _, ok := p["alpha"]; ok {
		t.Fatalf("alpha should have decayed to 0, got %v", p)
	}
}

func TestBridgeCadenceWhenEnabled(t *testing.T) {
	pool := []Candidate{
		{TrackID: 10, Artist: "Cluster", BaseScore: 0.9},
		{TrackID: 11, Artist: "Cluster", BaseScore: 0.88},
	}
	bridges := []Candidate{
		{TrackID: 99, Artist: "LikedOther", BaseScore: 0.8},
	}
	rng := rand.New(rand.NewSource(7))
	res := SelectDiversified(pool, SelectOpts{
		Need:              1,
		BridgeEnabled:     true,
		TracksSinceBridge: 6,
		BridgeInterval:    6,
		BridgeCandidates:  bridges,
		ArtistCooldown:    false,
		ArtistPenalty:     false,
		BoundedRandom:     false,
		RNG:               rng,
	})
	if !res.InjectedBridge || len(res.Picks) != 1 || res.Picks[0].TrackID != 99 {
		t.Fatalf("want bridge pick, got %#v injected=%v", res.Picks, res.InjectedBridge)
	}
	resOff := SelectDiversified(pool, SelectOpts{
		Need:              1,
		BridgeEnabled:     false,
		TracksSinceBridge: 6,
		BridgeInterval:    6,
		BridgeCandidates:  bridges,
		BoundedRandom:     false,
		RNG:               rng,
	})
	if resOff.InjectedBridge || resOff.Picks[0].TrackID != 10 {
		t.Fatalf("bridge disabled should take neighbor, got %#v", resOff.Picks)
	}
}

func TestBoundedRandomUsesTopN(t *testing.T) {
	pool := []Candidate{
		{TrackID: 1, Artist: "A", BaseScore: 1.0},
		{TrackID: 2, Artist: "B", BaseScore: 0.99},
		{TrackID: 3, Artist: "C", BaseScore: 0.98},
	}
	counts := map[int64]int{}
	for seed := int64(1); seed <= 80; seed++ {
		rng := rand.New(rand.NewSource(seed))
		res := SelectDiversified(pool, SelectOpts{
			Need:           1,
			BoundedRandom:  true,
			TopN:           3,
			ArtistCooldown: false,
			ArtistPenalty:  false,
			RNG:            rng,
		})
		counts[res.Picks[0].TrackID]++
	}
	if len(counts) < 2 {
		t.Fatalf("expected some diversity from weighted pick, got %v", counts)
	}
}

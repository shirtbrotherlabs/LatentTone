// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package affinity

import (
	"math"
	"math/rand"
	"sort"
	"strings"
)

// Diversification defaults (post-ANN selection; LanceDB schema unchanged).
const (
	DefaultPoolSize       = 80
	DefaultTopN           = 15
	DefaultCooldownWindow = 3
	DefaultPenaltyBoost   = 0.5
	DefaultPenaltyDecay   = 0.1
	DefaultJitterAlpha    = 0.05
	BridgeCadenceMin      = 5
	BridgeCadenceMax      = 7
)

// Candidate is a neighbor with catalog artist for slotted selection.
type Candidate struct {
	TrackID   int64
	Artist    string
	BaseScore float64 // cosine + user affinity (before artist penalty)
	Score     float64 // working score (BaseScore − penalties); set by SelectDiversified
	Source    string  // optional: "neighbor" | "radio_bridge"
}

// SelectOpts controls post-query diversification.
type SelectOpts struct {
	Need             int
	CooldownWindow   int // recent artist slots to avoid (e.g. 3)
	ArtistCooldown   bool
	ArtistPenalty    bool
	BoundedRandom    bool
	TopN             int
	PenaltyBoost     float64
	PenaltyDecay     float64
	RecentArtists    []string           // oldest→newest artists of recent plays
	ArtistPenalties  map[string]float64 // mutable working copy; updated as picks are made
	Exclude          map[int64]struct{}
	RNG              *rand.Rand
	BridgeEnabled    bool
	TracksSinceBridge int
	BridgeInterval   int // songs between bridges; 0 → pick in [5,7]
	BridgeCandidates []Candidate
}

// SelectResult is the diversified pick list plus updated penalty / bridge state.
type SelectResult struct {
	Picks             []Candidate
	ArtistPenalties   map[string]float64
	TracksSinceBridge int
	BridgeInterval    int
	InjectedBridge    bool
}

// JitterVector returns V_query = V_seed + α * ε with small Gaussian noise.
// Does not mutate seed.
func JitterVector(seed []float32, alpha float64, rng *rand.Rand) []float32 {
	if len(seed) == 0 {
		return nil
	}
	if alpha <= 0 {
		out := make([]float32, len(seed))
		copy(out, seed)
		return out
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	out := make([]float32, len(seed))
	for i := range seed {
		out[i] = seed[i] + float32(alpha*rng.NormFloat64())
	}
	return out
}

// NormalizeArtistKey canonicalizes artist strings for cooldown / penalty maps.
func NormalizeArtistKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ApplyArtistPenalties subtracts temporary cosine-distance penalties from scores.
func ApplyArtistPenalties(cands []Candidate, penalties map[string]float64) {
	if len(penalties) == 0 {
		return
	}
	for i := range cands {
		key := NormalizeArtistKey(cands[i].Artist)
		if p, ok := penalties[key]; ok && p > 0 {
			cands[i].Score -= p
		}
	}
}

// OnArtistPlayed updates exploding artist penalties after a different-artist track plays.
// Played artist gets PenaltyBoost; others decay by PenaltyDecay toward 0.
func OnArtistPlayed(penalties map[string]float64, artist string, boost, decay float64) map[string]float64 {
	if penalties == nil {
		penalties = map[string]float64{}
	}
	if boost <= 0 {
		boost = DefaultPenaltyBoost
	}
	if decay <= 0 {
		decay = DefaultPenaltyDecay
	}
	key := NormalizeArtistKey(artist)
	for k, v := range penalties {
		if k == key {
			continue
		}
		v -= decay
		if v <= 0 {
			delete(penalties, k)
		} else {
			penalties[k] = v
		}
	}
	if key != "" {
		penalties[key] = boost
	}
	return penalties
}

// SelectDiversified picks next tracks from a large neighbor pool with cooldown,
// exploding artist penalties, optional Radio Bridge, and bounded random choice.
func SelectDiversified(pool []Candidate, opts SelectOpts) SelectResult {
	need := opts.Need
	if need <= 0 {
		need = 1
	}
	topN := opts.TopN
	if topN <= 0 {
		topN = DefaultTopN
	}
	coolWin := opts.CooldownWindow
	if coolWin <= 0 {
		coolWin = DefaultCooldownWindow
	}
	boost := opts.PenaltyBoost
	if boost <= 0 {
		boost = DefaultPenaltyBoost
	}
	decay := opts.PenaltyDecay
	if decay <= 0 {
		decay = DefaultPenaltyDecay
	}
	rng := opts.RNG
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}

	penalties := map[string]float64{}
	for k, v := range opts.ArtistPenalties {
		penalties[k] = v
	}

	exclude := map[int64]struct{}{}
	for id := range opts.Exclude {
		exclude[id] = struct{}{}
	}

	remaining := make([]Candidate, 0, len(pool))
	for _, c := range pool {
		if _, bad := exclude[c.TrackID]; bad {
			continue
		}
		if c.Source == "" {
			c.Source = "neighbor"
		}
		if c.BaseScore == 0 && c.Score != 0 {
			c.BaseScore = c.Score
		}
		remaining = append(remaining, c)
	}

	cooldown := artistCooldownSet(opts.RecentArtists, coolWin)
	sinceBridge := opts.TracksSinceBridge
	bridgeInterval := opts.BridgeInterval
	if bridgeInterval <= 0 {
		bridgeInterval = BridgeCadenceMin + rng.Intn(BridgeCadenceMax-BridgeCadenceMin+1)
	}
	injectedBridge := false

	// Working penalties simulate queue stacking only; callers persist real penalties on Advance.
	workPenalties := map[string]float64{}
	for k, v := range penalties {
		workPenalties[k] = v
	}

	rescore := func() {
		for i := range remaining {
			remaining[i].Score = remaining[i].BaseScore
		}
		if opts.ArtistPenalty {
			ApplyArtistPenalties(remaining, workPenalties)
		}
	}
	rescore()

	var picks []Candidate
	for len(picks) < need && len(remaining) > 0 {
		// Radio Bridge: when play cadence is due, inject one liked track from elsewhere.
		if opts.BridgeEnabled && !injectedBridge && sinceBridge >= bridgeInterval {
			if b, ok := pickBridge(opts.BridgeCandidates, exclude, cooldown, opts.RecentArtists, remaining, rng); ok {
				b.Source = "radio_bridge"
				if b.BaseScore == 0 {
					b.BaseScore = b.Score
				}
				picks = append(picks, b)
				exclude[b.TrackID] = struct{}{}
				injectedBridge = true
				if opts.ArtistPenalty {
					workPenalties = OnArtistPlayed(workPenalties, b.Artist, boost, decay)
					rescore()
				}
				cooldown = pushCooldown(cooldown, coolWin, NormalizeArtistKey(b.Artist))
				continue
			}
		}

		slot := filterCooldown(remaining, cooldown, opts.ArtistCooldown)
		if len(slot) == 0 {
			break
		}
		sort.Slice(slot, func(i, j int) bool { return slot[i].Score > slot[j].Score })
		pick := slot[0]
		if opts.BoundedRandom {
			pick = weightedPickTopN(slot, topN, rng)
		}
		picks = append(picks, pick)
		exclude[pick.TrackID] = struct{}{}
		remaining = removeTrack(remaining, pick.TrackID)
		if opts.ArtistPenalty {
			workPenalties = OnArtistPlayed(workPenalties, pick.Artist, boost, decay)
			rescore()
		}
		cooldown = pushCooldown(cooldown, coolWin, NormalizeArtistKey(pick.Artist))
	}

	return SelectResult{
		Picks:             picks,
		ArtistPenalties:   penalties, // unchanged; Advance owns durable session penalties
		TracksSinceBridge: sinceBridge,
		BridgeInterval:    bridgeInterval,
		InjectedBridge:    injectedBridge,
	}
}

func artistCooldownSet(recent []string, window int) map[string]struct{} {
	out := map[string]struct{}{}
	if window <= 0 || len(recent) == 0 {
		return out
	}
	start := len(recent) - window
	if start < 0 {
		start = 0
	}
	for _, a := range recent[start:] {
		if k := NormalizeArtistKey(a); k != "" {
			out[k] = struct{}{}
		}
	}
	return out
}

func pushCooldown(cur map[string]struct{}, window int, artist string) map[string]struct{} {
	// Rebuild is callers' responsibility via RecentArtists on next fill;
	// within one fill we track a rolling set of up to window artists.
	if artist == "" {
		return cur
	}
	if cur == nil {
		cur = map[string]struct{}{}
	}
	cur[artist] = struct{}{}
	if window > 0 && len(cur) > window {
		// Drop arbitrary extras; fillQueue rebuilds from Recent on next call.
		for k := range cur {
			if len(cur) <= window {
				break
			}
			if k != artist {
				delete(cur, k)
			}
		}
	}
	return cur
}

func filterCooldown(cands []Candidate, cooldown map[string]struct{}, enabled bool) []Candidate {
	if !enabled || len(cooldown) == 0 {
		out := make([]Candidate, len(cands))
		copy(out, cands)
		return out
	}
	var filtered []Candidate
	for _, c := range cands {
		if _, hot := cooldown[NormalizeArtistKey(c.Artist)]; hot {
			continue
		}
		filtered = append(filtered, c)
	}
	if len(filtered) == 0 {
		// Soft fail: if every neighbor shares a cooldown artist, allow them.
		out := make([]Candidate, len(cands))
		copy(out, cands)
		return out
	}
	return filtered
}

func weightedPickTopN(sorted []Candidate, topN int, rng *rand.Rand) Candidate {
	n := topN
	if n > len(sorted) {
		n = len(sorted)
	}
	if n <= 1 {
		return sorted[0]
	}
	slice := sorted[:n]
	weights := make([]float64, n)
	var sum float64
	for i, c := range slice {
		w := c.Score
		if w < 0.01 {
			w = 0.01
		}
		// Softmax-ish: emphasize higher scores without collapsing to argmax.
		w = math.Exp(w * 3)
		weights[i] = w
		sum += w
	}
	r := rng.Float64() * sum
	var acc float64
	for i, w := range weights {
		acc += w
		if r <= acc {
			return slice[i]
		}
	}
	return slice[n-1]
}

func removeTrack(cands []Candidate, id int64) []Candidate {
	out := cands[:0]
	for _, c := range cands {
		if c.TrackID != id {
			out = append(out, c)
		}
	}
	return out
}

func pickBridge(bridges []Candidate, exclude map[int64]struct{}, cooldown map[string]struct{}, recentArtists []string, pool []Candidate, rng *rand.Rand) (Candidate, bool) {
	poolArtists := map[string]struct{}{}
	for _, c := range pool {
		if k := NormalizeArtistKey(c.Artist); k != "" {
			poolArtists[k] = struct{}{}
		}
	}
	recentSet := artistCooldownSet(recentArtists, len(recentArtists))

	var eligible []Candidate
	for _, b := range bridges {
		if _, bad := exclude[b.TrackID]; bad {
			continue
		}
		key := NormalizeArtistKey(b.Artist)
		if key == "" {
			continue
		}
		if _, hot := recentSet[key]; hot {
			continue
		}
		if _, hot := cooldown[key]; hot {
			continue
		}
		// Prefer bridges from a different neighborhood (not dominant pool artists).
		if _, inPool := poolArtists[key]; inPool && len(bridges) > 3 {
			continue
		}
		eligible = append(eligible, b)
	}
	if len(eligible) == 0 {
		// Relax pool-neighborhood filter.
		for _, b := range bridges {
			if _, bad := exclude[b.TrackID]; bad {
				continue
			}
			key := NormalizeArtistKey(b.Artist)
			if key == "" {
				continue
			}
			if _, hot := recentSet[key]; hot {
				continue
			}
			eligible = append(eligible, b)
		}
	}
	if len(eligible) == 0 {
		return Candidate{}, false
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].Score > eligible[j].Score })
	return weightedPickTopN(eligible, DefaultTopN, rng), true
}

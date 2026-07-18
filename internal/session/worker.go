// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/lance"
	"github.com/shirtbrotherlabs/LatentTone/internal/stream"
)

// NeighborFn selects similar tracks for the seed (injectable for tests).
type NeighborFn func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error)

// VectorNeighborFn selects similar tracks for an arbitrary query vector (jitter path).
type VectorNeighborFn func(ctx context.Context, query []float32, excludeTrackID int64, k int) ([]affinity.Neighbor, error)

// Live holds in-memory trajectory for an active listening session.
type Live struct {
	ID           string
	UserID       int64
	SeedTrackID  int64
	Status       string
	NowPlayingID int64
	Queue        []int64
	Pinned       map[int64]struct{} // user_pin track ids (ADR-007 V5b)
	Sources      map[int64]string   // track_id → source tag (user_pin | radio_bridge)
	Recent       []int64
	// History is previously played/skipped tracks (most recent at end). Used by Back.
	History      []int64
	LastFeedback FeedbackAck

	// Radio diversification state (in-memory; resets if process reloads session from the catalog DB).
	ArtistPenalties   map[string]float64
	TracksSinceBridge int
	BridgeInterval    int
	BridgeQueued      bool
	// refillGen coalesces async queue refills so rapid skips don't stack ANN work.
	refillGen uint64

	mu sync.Mutex
}

// FeedbackAck is echoed on short-poll.
type FeedbackAck struct {
	Signal  string `json:"signal"`
	TrackID int64  `json:"track_id"`
	At      string `json:"at"`
}

// StatusDTO is the short-poll JSON shape.
type StatusDTO struct {
	ID          string    `json:"id"`
	UserID      int64     `json:"user_id"`
	Status      string    `json:"status"`
	SeedTrackID int64     `json:"seed_track_id"`
	NowPlaying  *TrackRef `json:"now_playing"`
	// History is recently played/skipped tracks (oldest → newest), capped for SPA.
	History        []TrackRef   `json:"history,omitempty"`
	Queue          []TrackRef   `json:"queue"`
	LastFeedback   *FeedbackAck `json:"last_feedback,omitempty"`
	HLSURL         string       `json:"hls_url"`
	ProgressiveURL string       `json:"progressive_url"`
	CanGoBack      bool         `json:"can_go_back"`
	// StreamCodec / StreamBitrateKbps describe progressive delivery for now-playing.
	StreamTrackID     int64  `json:"stream_track_id,omitempty"`
	StreamCodec       string `json:"stream_codec,omitempty"`
	StreamBitrateKbps int    `json:"stream_bitrate_kbps,omitempty"`
	StreamTranscoding bool   `json:"stream_transcoding,omitempty"`
}

// maxHistoryExposed is how many past tracks appear on session status for Now Playing.
const maxHistoryExposed = 8

// TrackRef is a minimal track pointer for API clients.
type TrackRef struct {
	TrackID   int64   `json:"track_id"`
	Score     float64 `json:"score,omitempty"`
	Source    string  `json:"source,omitempty"`     // "user_pin" when manually injected
	Feedback  string  `json:"feedback,omitempty"`   // latest like|dislike for this user
	PlayCount int64   `json:"play_count,omitempty"` // global playback_events count
}

// ErrNoHistory is returned by Back when there is no previous track to restore.
var ErrNoHistory = fmt.Errorf("no history")

const maxSessionHistory = 40

// Worker manages live sessions and persistence.
type Worker struct {
	DB              *db.DB
	Store           *lance.Store
	Neighbors       NeighborFn
	VectorNeighbors VectorNeighborFn
	QueuePrefetch   int
	MaxConcurrent   int
	NeighborPool    int // ANN / flat pool size before diversification (default 80)
	OnAdvance       func(sessionID string, trackID int64)
	Rand            *rand.Rand // optional; tests inject a seed

	mu       sync.Mutex
	sessions map[string]*Live
}

// NewWorker constructs a session worker.
func NewWorker(catalog *db.DB, store *lance.Store, maxConcurrent, prefetch int) *Worker {
	if prefetch <= 0 {
		prefetch = 12
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	w := &Worker{
		DB:            catalog,
		Store:         store,
		QueuePrefetch: prefetch,
		MaxConcurrent: maxConcurrent,
		NeighborPool:  affinity.DefaultPoolSize,
		sessions:      make(map[string]*Live),
	}
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return affinity.NeighborsWithStore(ctx, catalog, store, seedTrackID, k)
	}
	w.VectorNeighbors = func(ctx context.Context, query []float32, excludeTrackID int64, k int) ([]affinity.Neighbor, error) {
		return affinity.NeighborsByVector(ctx, catalog, store, query, excludeTrackID, k)
	}
	return w
}

func (w *Worker) rng() *rand.Rand {
	if w.Rand != nil {
		return w.Rand
	}
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// Create starts a listening session for user from seedTrackID.
func (w *Worker) Create(ctx context.Context, userID, seedTrackID int64) (*Live, error) {
	seed, err := w.DB.GetTrack(seedTrackID)
	if err != nil {
		return nil, err
	}
	if seed == nil || seed.MissingAt.Valid {
		return nil, fmt.Errorf("seed track not found")
	}

	w.mu.Lock()
	active := 0
	for _, s := range w.sessions {
		if s.Status == db.SessionStatusPlaying || s.Status == db.SessionStatusCreated {
			active++
		}
	}
	w.mu.Unlock()
	if active >= w.MaxConcurrent {
		return nil, fmt.Errorf("too many concurrent sessions")
	}

	id, err := auth.NewOpaqueID()
	if err != nil {
		return nil, err
	}
	if _, err := w.DB.CreateListeningSession(id, userID, seedTrackID); err != nil {
		return nil, err
	}

	live := &Live{
		ID:              id,
		UserID:          userID,
		SeedTrackID:     seedTrackID,
		Status:          db.SessionStatusPlaying,
		NowPlayingID:    seedTrackID,
		Pinned:          map[int64]struct{}{},
		Sources:         map[int64]string{},
		Recent:          []int64{seedTrackID},
		ArtistPenalties: map[string]float64{},
	}
	w.onTrackStarted(live, seedTrackID, "")
	_ = w.fillQueue(ctx, live)
	live.Status = db.SessionStatusPlaying
	_ = w.persist(live, "")

	w.mu.Lock()
	w.sessions[id] = live
	w.mu.Unlock()

	if w.OnAdvance != nil {
		w.OnAdvance(id, seedTrackID)
	}
	_, _ = w.DB.InsertPlaybackEvent(userID, seedTrackID, id)
	return live, nil
}

func (w *Worker) ensurePinned(live *Live) {
	if live.Pinned == nil {
		live.Pinned = map[int64]struct{}{}
	}
	if live.Sources == nil {
		live.Sources = map[int64]string{}
	}
	if live.ArtistPenalties == nil {
		live.ArtistPenalties = map[string]float64{}
	}
}

func (w *Worker) splitQueue(live *Live) (pins, auto []int64) {
	w.ensurePinned(live)
	for _, id := range live.Queue {
		if _, ok := live.Pinned[id]; ok {
			pins = append(pins, id)
			continue
		}
		auto = append(auto, id)
	}
	return pins, auto
}

func (w *Worker) fillQueue(ctx context.Context, live *Live) error {
	return w.fillQueueFrom(ctx, live, 0)
}

// scheduleQueueRefill tops up (or re-anchors) the auto queue asynchronously.
// Critical: live.mu is NOT held during ANN / LanceDB so short-poll ToStatus and
// feedback stay responsive (holding the lock through neighbor search caused
// gateway 504s and a stuck SPA).
//
// Safe to call while holding live.mu — the goroutine acquires the lock itself.
// replaceSeed > 0 drops auto-prefetch and refills biased to that seed (like).
// replaceSeed == 0 tops up from now-playing / session seed — skipped when the
// auto queue is still above a watermark so every skip does not start ANN.
func (w *Worker) scheduleQueueRefill(live *Live, replaceSeed int64) {
	go func(l *Live, seedOverride int64) {
		l.mu.Lock()
		if l.Status == db.SessionStatusStopped {
			l.mu.Unlock()
			return
		}
		if seedOverride == 0 {
			_, auto := w.splitQueue(l)
			minAuto := w.QueuePrefetch / 2
			if minAuto < 3 {
				minAuto = 3
			}
			if len(auto) >= minAuto {
				l.mu.Unlock()
				return
			}
		}
		l.refillGen++
		gen := l.refillGen
		if seedOverride > 0 {
			pins, _ := w.splitQueue(l)
			l.Queue = append([]int64(nil), pins...)
		}
		l.mu.Unlock()

		// ANN / catalog fallback without the session mutex.
		if err := w.fillQueueFrom(context.Background(), l, seedOverride); err != nil {
			return
		}

		l.mu.Lock()
		defer l.mu.Unlock()
		if l.Status == db.SessionStatusStopped || gen != l.refillGen {
			return
		}
		_ = w.persist(l, "")
	}(live, replaceSeed)
}

// fillQueueFrom tops up auto-prefetched tracks. seedOverride > 0 biases the ANN query
// (e.g. after a like). Already-queued auto tracks are preserved and topped up.
//
// Do not hold live.mu across this call when the session is shared (status polls).
// Create may call it on a private Live before publishing; scheduleQueueRefill is
// the safe path for live sessions.
func (w *Worker) fillQueueFrom(ctx context.Context, live *Live, seedOverride int64) error {
	// Snapshot under lock so concurrent poll/feedback can proceed during ANN.
	live.mu.Lock()
	need := w.QueuePrefetch
	pins, oldAuto := w.splitQueue(live)
	existingAuto := append([]int64(nil), oldAuto...)
	userID := live.UserID
	sessionID := live.ID
	nowPlaying := live.NowPlayingID
	seedTrack := live.SeedTrackID
	recent := append([]int64(nil), live.Recent...)
	tracksSinceBridge := live.TracksSinceBridge
	bridgeInterval := live.BridgeInterval
	penalties := map[string]float64{}
	for k, v := range live.ArtistPenalties {
		penalties[k] = v
	}
	live.mu.Unlock()

	skips, err := w.DB.ListSkippedTrackIDs(userID, sessionID)
	if err != nil {
		return err
	}
	exclude := map[int64]struct{}{}
	for id := range skips {
		exclude[id] = struct{}{}
	}
	for _, id := range recent {
		exclude[id] = struct{}{}
	}
	for _, id := range pins {
		exclude[id] = struct{}{}
	}
	for _, id := range existingAuto {
		exclude[id] = struct{}{}
	}
	exclude[nowPlaying] = struct{}{}

	needMore := need - len(existingAuto)
	if needMore <= 0 {
		live.mu.Lock()
		defer live.mu.Unlock()
		pins, oldAuto = w.splitQueue(live)
		live.Queue = append(pins, oldAuto...)
		if len(live.Queue) > len(pins)+need {
			live.Queue = live.Queue[:len(pins)+need]
		}
		return nil
	}

	prefs, _ := w.DB.GetRadioPrefs(userID)
	seed := seedOverride
	if seed == 0 {
		seed = nowPlaying
	}
	if seed == 0 {
		seed = seedTrack
	}

	poolK := w.NeighborPool
	if poolK <= 0 {
		poolK = affinity.DefaultPoolSize
	}
	candidates, err := w.queryNeighbors(ctx, seed, poolK, prefs)
	if err != nil || len(candidates) == 0 {
		ids, ferr := w.DB.ListTrackIDsNotMissing(needMore + 5)
		live.mu.Lock()
		defer live.mu.Unlock()
		pins, oldAuto = w.splitQueue(live)
		if ferr != nil {
			live.Queue = append(pins, oldAuto...)
			return nil
		}
		auto := append([]int64(nil), oldAuto...)
		seen := map[int64]struct{}{}
		for _, id := range auto {
			seen[id] = struct{}{}
		}
		for _, id := range ids {
			if _, bad := exclude[id]; bad {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			auto = append(auto, id)
			if len(auto) >= need {
				break
			}
		}
		live.Queue = append(pins, auto...)
		return nil
	}

	pool := make([]affinity.Candidate, 0, len(candidates))
	for _, n := range candidates {
		if _, bad := exclude[n.TrackID]; bad {
			continue
		}
		t, err := w.DB.GetTrack(n.TrackID)
		if err != nil || t == nil || t.MissingAt.Valid {
			continue
		}
		aff, _ := w.DB.GetAffinity(userID, n.TrackID)
		pool = append(pool, affinity.Candidate{
			TrackID:   n.TrackID,
			Artist:    t.ArtistName,
			BaseScore: n.Score + aff,
			Source:    "neighbor",
		})
	}

	// Snapshot diversification inputs under lock; DB work stays unlocked so skip
	// feedback cannot wedge behind GetTrack storms.
	live.mu.Lock()
	recentIDs := append([]int64{}, live.Recent...)
	if live.NowPlayingID > 0 {
		recentIDs = append(recentIDs, live.NowPlayingID)
	}
	bridgeQueued := live.BridgeQueued
	if live.BridgeInterval <= 0 {
		live.BridgeInterval = affinity.BridgeCadenceMin + w.rng().Intn(affinity.BridgeCadenceMax-affinity.BridgeCadenceMin+1)
	}
	bridgeInterval = live.BridgeInterval
	live.mu.Unlock()

	recentArtists := w.artistsForTrackIDs(recentIDs)
	bridgeCands := w.bridgeCandidates(userID, exclude, prefs)
	bridgeDue := prefs.RadioBridge && !bridgeQueued && tracksSinceBridge >= bridgeInterval

	res := affinity.SelectDiversified(pool, affinity.SelectOpts{
		Need:              needMore,
		CooldownWindow:    affinity.DefaultCooldownWindow,
		ArtistCooldown:    prefs.ArtistCooldown,
		ArtistPenalty:     prefs.ArtistPenalty,
		BoundedRandom:     prefs.BoundedRandom,
		TopN:              affinity.DefaultTopN,
		RecentArtists:     recentArtists,
		ArtistPenalties:   penalties,
		Exclude:           exclude,
		RNG:               w.rng(),
		BridgeEnabled:     bridgeDue,
		TracksSinceBridge: tracksSinceBridge,
		BridgeInterval:    bridgeInterval,
		BridgeCandidates:  bridgeCands,
	})

	live.mu.Lock()
	defer live.mu.Unlock()
	w.ensurePinned(live)
	pins, oldAuto = w.splitQueue(live)
	auto := append([]int64(nil), oldAuto...)
	seen := map[int64]struct{}{}
	for _, id := range auto {
		seen[id] = struct{}{}
	}
	for _, p := range res.Picks {
		if _, ok := seen[p.TrackID]; ok {
			continue
		}
		auto = append(auto, p.TrackID)
		seen[p.TrackID] = struct{}{}
		if p.Source != "" && p.Source != "neighbor" {
			live.Sources[p.TrackID] = p.Source
		}
		if p.Source == "radio_bridge" {
			live.BridgeQueued = true
		}
	}
	if len(auto) > need {
		auto = auto[:need]
	}
	live.Queue = append(pins, auto...)
	if live.BridgeInterval <= 0 {
		live.BridgeInterval = res.BridgeInterval
	}
	return nil
}

func (w *Worker) queryNeighbors(ctx context.Context, seedTrackID int64, k int, prefs db.RadioPrefs) ([]affinity.Neighbor, error) {
	if prefs.QueryJitter && w.VectorNeighbors != nil && w.DB != nil {
		vecRow, err := w.DB.GetTrackVector(seedTrackID)
		if err == nil && vecRow != nil && vecRow.Status == db.VecReady && len(vecRow.Embedding) > 0 {
			alpha := prefs.JitterAlpha
			if alpha <= 0 {
				alpha = affinity.DefaultJitterAlpha
			}
			q := affinity.JitterVector(vecRow.Embedding, alpha, w.rng())
			ns, err := w.VectorNeighbors(ctx, q, seedTrackID, k)
			if err == nil && len(ns) > 0 {
				return ns, nil
			}
		}
	}
	if w.Neighbors == nil {
		return nil, fmt.Errorf("no neighbor function")
	}
	return w.Neighbors(ctx, seedTrackID, k)
}

func (w *Worker) artistsForTrackIDs(ids []int64) []string {
	var out []string
	for _, id := range ids {
		t, err := w.DB.GetTrack(id)
		if err != nil || t == nil {
			continue
		}
		out = append(out, t.ArtistName)
	}
	return out
}

func (w *Worker) bridgeCandidates(userID int64, exclude map[int64]struct{}, prefs db.RadioPrefs) []affinity.Candidate {
	if !prefs.RadioBridge {
		return nil
	}
	hits, err := w.DB.ListHighAffinityTracks(userID, 40)
	if err != nil || len(hits) == 0 {
		return nil
	}
	var out []affinity.Candidate
	for _, h := range hits {
		if _, bad := exclude[h.TrackID]; bad {
			continue
		}
		t, err := w.DB.GetTrack(h.TrackID)
		if err != nil || t == nil || t.MissingAt.Valid {
			continue
		}
		out = append(out, affinity.Candidate{
			TrackID:   h.TrackID,
			Artist:    t.ArtistName,
			BaseScore: h.Score,
			Source:    "radio_bridge",
		})
	}
	return out
}

func (w *Worker) persist(live *Live, feedbackJSON string) error {
	return w.DB.UpdateListeningSessionState(
		live.ID, live.Status, live.NowPlayingID, live.Queue, feedbackJSON, "",
	)
}

// Get returns a live session if owned by userID (or loads from DB).
func (w *Worker) Get(sessionID string, userID int64) (*Live, error) {
	w.mu.Lock()
	live, ok := w.sessions[sessionID]
	w.mu.Unlock()
	if ok {
		if live.UserID != userID {
			return nil, fmt.Errorf("forbidden")
		}
		return live, nil
	}
	row, err := w.DB.GetListeningSession(sessionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	if row.UserID != userID {
		return nil, fmt.Errorf("forbidden")
	}
	queue, _ := db.ParseQueueJSON(nullString(row.QueueJSON))
	live = &Live{
		ID:              row.ID,
		UserID:          row.UserID,
		SeedTrackID:     nullInt64(row.SeedTrackID),
		Status:          row.Status,
		NowPlayingID:    nullInt64(row.NowPlayingID),
		Queue:           queue,
		Pinned:          map[int64]struct{}{},
		Sources:         map[int64]string{},
		Recent:          []int64{},
		ArtistPenalties: map[string]float64{},
	}
	if row.LastFeedback.Valid {
		var ack FeedbackAck
		if json.Unmarshal([]byte(row.LastFeedback.String), &ack) == nil {
			live.LastFeedback = ack
		}
	}
	w.mu.Lock()
	w.sessions[sessionID] = live
	w.mu.Unlock()
	return live, nil
}

// Advance moves to next track (natural end or skip).
func (w *Worker) Advance(ctx context.Context, live *Live) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	return w.advanceLocked(ctx, live)
}

func (w *Worker) advanceLocked(ctx context.Context, live *Live) error {
	if len(live.Queue) == 0 {
		// fillQueueFrom locks internally — must not call while holding live.mu.
		live.mu.Unlock()
		_ = w.fillQueueFrom(ctx, live, 0)
		live.mu.Lock()
		if live.Status == db.SessionStatusStopped {
			return fmt.Errorf("session stopped")
		}
	}
	if len(live.Queue) == 0 {
		live.Status = db.SessionStatusStopped
		return w.persist(live, "")
	}
	next := live.Queue[0]
	live.Queue = live.Queue[1:]
	w.ensurePinned(live)
	delete(live.Pinned, next)
	src := live.Sources[next]
	delete(live.Sources, next)
	if live.NowPlayingID > 0 {
		live.Recent = append(live.Recent, live.NowPlayingID)
		live.History = append(live.History, live.NowPlayingID)
		if len(live.History) > maxSessionHistory {
			live.History = live.History[len(live.History)-maxSessionHistory:]
		}
	}
	live.NowPlayingID = next
	live.Status = db.SessionStatusPlaying
	userID := live.UserID
	sid := live.ID

	// DB lookups for diversification must not run under live.mu — that blocked
	// skip feedback for seconds while refill/GetTrack ran.
	live.mu.Unlock()
	prefs, _ := w.DB.GetRadioPrefs(userID)
	artist := ""
	if t, err := w.DB.GetTrack(next); err == nil && t != nil {
		artist = t.ArtistName
	}
	live.mu.Lock()
	if live.Status == db.SessionStatusStopped {
		return fmt.Errorf("session stopped")
	}
	if live.NowPlayingID != next {
		return nil
	}
	w.applyTrackStarted(live, next, src, artist, prefs)
	// Commit now_playing immediately so skip feedback can return without waiting
	// on ANN refill or FFmpeg HLS packaging.
	if err := w.persist(live, ""); err != nil {
		return err
	}
	if w.OnAdvance != nil {
		go w.OnAdvance(sid, next)
	}
	w.scheduleQueueRefill(live, 0)
	// Playback history insert outside the hot path callers still hold live.mu.
	go func() {
		_, _ = w.DB.InsertPlaybackEvent(userID, next, sid)
	}()
	return nil
}

// onTrackStarted updates artist-penalty + Radio Bridge cadence after a track becomes now_playing.
// Safe to call without live.mu (Create path); fetches prefs/artist then applies under lock if needed.
func (w *Worker) onTrackStarted(live *Live, trackID int64, source string) {
	prefs, _ := w.DB.GetRadioPrefs(live.UserID)
	artist := ""
	if t, err := w.DB.GetTrack(trackID); err == nil && t != nil {
		artist = t.ArtistName
	}
	w.applyTrackStarted(live, trackID, source, artist, prefs)
}

// applyTrackStarted mutates diversification state. Caller must hold live.mu (or own live exclusively).
func (w *Worker) applyTrackStarted(live *Live, trackID int64, source, artist string, prefs db.RadioPrefs) {
	w.ensurePinned(live)
	if prefs.ArtistPenalty && artist != "" {
		live.ArtistPenalties = affinity.OnArtistPlayed(
			live.ArtistPenalties, artist, affinity.DefaultPenaltyBoost, affinity.DefaultPenaltyDecay,
		)
	}
	if source == "radio_bridge" {
		live.BridgeQueued = false
		live.TracksSinceBridge = 0
		live.BridgeInterval = affinity.BridgeCadenceMin + w.rng().Intn(affinity.BridgeCadenceMax-affinity.BridgeCadenceMin+1)
		// Bridge becomes the new Radio seed / neighborhood anchor.
		live.SeedTrackID = trackID
		return
	}
	if source != "user_pin" {
		live.TracksSinceBridge++
	}
}

// ApplyFeedback persists signal and mutates queue (ADR-007).
// DB writes (feedback / affinity / skips) run without live.mu so short-poll
// ToStatus cannot wedge behind MariaDB. Queue mutation + persist stay locked;
// ANN refill is always async.
func (w *Worker) ApplyFeedback(ctx context.Context, live *Live, signal string, trackID int64) error {
	live.mu.Lock()
	if trackID == 0 {
		trackID = live.NowPlayingID
	}
	userID := live.UserID
	sessionID := live.ID
	live.mu.Unlock()

	prevSignals := map[int64]string{}
	if signal == db.SignalClear {
		prevSignals, _ = w.DB.LatestLikeDislikeSignals(userID, []int64{trackID})
	}
	if err := w.DB.InsertTrackFeedback(userID, trackID, signal, sessionID); err != nil {
		return err
	}

	switch signal {
	case db.SignalLike:
		_, _ = w.DB.UpsertAffinity(userID, trackID, 0.25)
	case db.SignalClear:
		switch prevSignals[trackID] {
		case db.SignalLike:
			_, _ = w.DB.UpsertAffinity(userID, trackID, -0.25)
		case db.SignalDislike:
			_, _ = w.DB.UpsertAffinity(userID, trackID, 0.25)
		}
	case db.SignalDislike:
		_, _ = w.DB.UpsertAffinity(userID, trackID, -0.25)
		_ = w.DB.AddSkip(userID, trackID, db.SkipScopeSession, sessionID)
	case db.SignalSkip:
		_ = w.DB.AddSkip(userID, trackID, db.SkipScopeSession, sessionID)
		_, _ = w.DB.UpsertAffinity(userID, trackID, -0.1)
	case db.SignalBan:
		_ = w.DB.AddSkip(userID, trackID, db.SkipScopeLibrary, "")
		_, _ = w.DB.UpsertAffinity(userID, trackID, -1)
	case db.SignalComplete:
		// no affinity / skip side effects
	default:
		return fmt.Errorf("unknown signal %q", signal)
	}

	live.mu.Lock()
	defer live.mu.Unlock()
	ack := FeedbackAck{Signal: signal, TrackID: trackID, At: db.Now()}
	live.LastFeedback = ack
	fb, _ := json.Marshal(ack)

	var (
		asyncRefill      bool
		asyncReplaceSeed int64
		asyncTopUp       bool
	)

	switch signal {
	case db.SignalLike:
		// Only re-anchor the auto queue when liking now-playing; history likes
		// still bump affinity (above) without blocking on ANN.
		if trackID == live.NowPlayingID {
			asyncReplaceSeed = trackID
			asyncRefill = true
		}
	case db.SignalClear:
		// affinity already adjusted; no queue mutation
	case db.SignalDislike:
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		} else {
			asyncTopUp = true
			asyncRefill = true
		}
	case db.SignalSkip:
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		}
	case db.SignalComplete:
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		}
	case db.SignalBan:
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		} else {
			asyncTopUp = true
			asyncRefill = true
		}
	}
	if err := w.persist(live, string(fb)); err != nil {
		return err
	}
	if asyncRefill {
		if asyncReplaceSeed > 0 {
			w.scheduleQueueRefill(live, asyncReplaceSeed)
		} else if asyncTopUp {
			w.scheduleQueueRefill(live, 0)
		}
	}
	return nil
}

// Back restores the previous track from History (played/skipped).
// The current now-playing track is pushed to the front of the queue so the station continues.
// When History is empty, returns ErrNoHistory (client should restart media from 0).
func (w *Worker) Back(ctx context.Context, live *Live) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.Status == db.SessionStatusStopped {
		return fmt.Errorf("session stopped")
	}
	if len(live.History) == 0 {
		return ErrNoHistory
	}
	prev := live.History[len(live.History)-1]
	live.History = live.History[:len(live.History)-1]
	current := live.NowPlayingID
	if current > 0 {
		live.Queue = filterOut(live.Queue, current)
		live.Queue = append([]int64{current}, live.Queue...)
		w.ensurePinned(live)
		delete(live.Pinned, current)
	}
	live.NowPlayingID = prev
	live.Status = db.SessionStatusPlaying
	userID := live.UserID
	sid := live.ID
	live.mu.Unlock()
	prefs, _ := w.DB.GetRadioPrefs(userID)
	artist := ""
	if t, err := w.DB.GetTrack(prev); err == nil && t != nil {
		artist = t.ArtistName
	}
	live.mu.Lock()
	if live.Status == db.SessionStatusStopped {
		return fmt.Errorf("session stopped")
	}
	if live.NowPlayingID != prev {
		return nil
	}
	w.applyTrackStarted(live, prev, "", artist, prefs)
	if err := w.persist(live, ""); err != nil {
		return err
	}
	go func() {
		_, _ = w.DB.InsertPlaybackEvent(userID, prev, sid)
	}()
	if w.OnAdvance != nil {
		go w.OnAdvance(sid, prev)
	}
	w.scheduleQueueRefill(live, 0)
	_ = ctx
	return nil
}

// InjectQueue pins a track into the upcoming queue (ADR-007 V5b).
// position is "next" (front of queue) or "end" (after existing pins).
// If the track is already queued, it is moved to the requested position
// instead of returning a conflict (so "Play next" from Up next works).
func (w *Worker) InjectQueue(ctx context.Context, live *Live, trackID int64, position string) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if trackID <= 0 {
		return fmt.Errorf("track_id required")
	}
	switch position {
	case "", "next":
		position = "next"
	case "end":
		// ok
	default:
		return fmt.Errorf("position must be next or end")
	}
	if live.Status == db.SessionStatusStopped {
		return fmt.Errorf("session stopped")
	}
	t, err := w.DB.GetTrack(trackID)
	if err != nil {
		return err
	}
	if t == nil || t.MissingAt.Valid {
		return fmt.Errorf("track not found")
	}
	if trackID == live.NowPlayingID {
		return errQueueConflict
	}
	skips, err := w.DB.ListSkippedTrackIDs(live.UserID, live.ID)
	if err != nil {
		return err
	}
	if _, banned := skips[trackID]; banned {
		return fmt.Errorf("track is skipped or banned")
	}
	w.ensurePinned(live)
	live.Queue = filterOut(live.Queue, trackID)
	pins, auto := w.splitQueue(live)
	if position == "next" {
		pins = append([]int64{trackID}, pins...)
	} else {
		pins = append(pins, trackID)
	}
	live.Pinned[trackID] = struct{}{}
	live.Sources[trackID] = "user_pin"
	live.Queue = append(pins, auto...)
	if err := w.persist(live, ""); err != nil {
		return err
	}
	w.scheduleQueueRefill(live, 0)
	_ = ctx
	return nil
}

// RemoveFromQueue drops a track from the upcoming queue, session-skips it so
// affinity refill will not immediately re-add it, then tops up the queue.
func (w *Worker) RemoveFromQueue(ctx context.Context, live *Live, trackID int64) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if trackID <= 0 {
		return fmt.Errorf("track_id required")
	}
	if live.Status == db.SessionStatusStopped {
		return fmt.Errorf("session stopped")
	}
	if !contains(live.Queue, trackID) {
		return fmt.Errorf("track not in queue")
	}
	w.ensurePinned(live)
	live.Queue = filterOut(live.Queue, trackID)
	delete(live.Pinned, trackID)
	delete(live.Sources, trackID)
	_ = w.DB.AddSkip(live.UserID, trackID, db.SkipScopeSession, live.ID)
	if err := w.persist(live, ""); err != nil {
		return err
	}
	w.scheduleQueueRefill(live, 0)
	_ = ctx
	return nil
}

// errQueueConflict is returned when the track is already playing (cannot re-queue as next).
var errQueueConflict = fmt.Errorf("track is now playing")

// IsQueueConflict reports whether err is a now-playing inject conflict.
func IsQueueConflict(err error) bool {
	return err == errQueueConflict
}

// Stop marks the session stopped.
func (w *Worker) Stop(live *Live) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.Status = db.SessionStatusStopped
	return w.persist(live, "")
}

// StatusDTO builds short-poll payload.
func (w *Worker) ToStatus(live *Live) StatusDTO {
	live.mu.Lock()
	w.ensurePinned(live)
	dto := StatusDTO{
		ID:             live.ID,
		UserID:         live.UserID,
		Status:         live.Status,
		SeedTrackID:    live.SeedTrackID,
		HLSURL:         fmt.Sprintf("/api/v1/sessions/%s/hls/index.m3u8", live.ID),
		ProgressiveURL: "",
		History:        []TrackRef{},
		Queue:          []TrackRef{},
		CanGoBack:      len(live.History) > 0,
	}
	userID := live.UserID
	if live.NowPlayingID > 0 {
		dto.NowPlaying = &TrackRef{TrackID: live.NowPlayingID}
		dto.ProgressiveURL = fmt.Sprintf("/api/v1/tracks/%d/stream", live.NowPlayingID)
	}
	start := 0
	if len(live.History) > maxHistoryExposed {
		start = len(live.History) - maxHistoryExposed
	}
	for _, id := range live.History[start:] {
		dto.History = append(dto.History, TrackRef{TrackID: id})
	}
	for _, id := range live.Queue {
		ref := TrackRef{TrackID: id}
		if _, ok := live.Pinned[id]; ok {
			ref.Source = "user_pin"
		} else if src, ok := live.Sources[id]; ok && src != "" {
			ref.Source = src
		}
		dto.Queue = append(dto.Queue, ref)
	}
	if live.LastFeedback.Signal != "" {
		ack := live.LastFeedback
		dto.LastFeedback = &ack
	}
	nowID := live.NowPlayingID
	live.mu.Unlock()
	w.enrichTrackRefs(userID, &dto)
	w.enrichStreamInfo(userID, nowID, &dto)
	return dto
}

// enrichStreamInfo attaches progressive codec/bitrate for the floating player badge.
func (w *Worker) enrichStreamInfo(userID, nowPlayingID int64, dto *StatusDTO) {
	if dto == nil || nowPlayingID <= 0 {
		return
	}
	t, err := w.DB.GetTrack(nowPlayingID)
	if err != nil || t == nil {
		return
	}
	prefs := db.DefaultStreamPrefs(userID)
	if p, err := w.DB.GetStreamPrefs(userID); err == nil {
		prefs = p
	}
	catalogFmt := ""
	if t.Format.Valid {
		catalogFmt = t.Format.String
	}
	catalogBr := 0
	if t.BitrateKbps.Valid && t.BitrateKbps.Int64 > 0 {
		catalogBr = int(t.BitrateKbps.Int64)
	}
	info := stream.ResolveEffectiveStream(t.Path, catalogFmt, catalogBr, stream.EncodeOpts{
		Format:      prefs.StreamFormat,
		BitrateKbps: prefs.BitrateKbps,
	})
	dto.StreamTrackID = nowPlayingID
	dto.StreamCodec = info.Codec
	dto.StreamBitrateKbps = info.BitrateKbps
	dto.StreamTranscoding = info.Transcoding
}

// enrichTrackRefs attaches per-user like/dislike and play counts for SPA chrome.
func (w *Worker) enrichTrackRefs(userID int64, dto *StatusDTO) {
	if dto == nil {
		return
	}
	ids := make([]int64, 0, 1+len(dto.History)+len(dto.Queue))
	if dto.NowPlaying != nil && dto.NowPlaying.TrackID > 0 {
		ids = append(ids, dto.NowPlaying.TrackID)
	}
	for _, r := range dto.History {
		ids = append(ids, r.TrackID)
	}
	for _, r := range dto.Queue {
		ids = append(ids, r.TrackID)
	}
	signals, _ := w.DB.LatestLikeDislikeSignals(userID, ids)
	plays, _ := w.DB.PlayCountsForTracks(ids)
	apply := func(ref *TrackRef) {
		if ref == nil {
			return
		}
		if sig, ok := signals[ref.TrackID]; ok {
			ref.Feedback = sig
		}
		if n, ok := plays[ref.TrackID]; ok && n > 0 {
			ref.PlayCount = n
		}
	}
	apply(dto.NowPlaying)
	for i := range dto.History {
		apply(&dto.History[i])
	}
	for i := range dto.Queue {
		apply(&dto.Queue[i])
	}
}

func nullInt64(n sql.NullInt64) int64 {
	if n.Valid {
		return n.Int64
	}
	return 0
}

func nullString(n sql.NullString) string {
	if n.Valid {
		return n.String
	}
	return ""
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func filterOut(ids []int64, id int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

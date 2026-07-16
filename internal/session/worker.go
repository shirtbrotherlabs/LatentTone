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

	// Radio diversification state (in-memory; resets if process reloads session from SQLite).
	ArtistPenalties   map[string]float64
	TracksSinceBridge int
	BridgeInterval    int
	BridgeQueued      bool

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
	ID              string       `json:"id"`
	UserID          int64        `json:"user_id"`
	Status          string       `json:"status"`
	SeedTrackID     int64        `json:"seed_track_id"`
	NowPlaying      *TrackRef    `json:"now_playing"`
	// History is recently played/skipped tracks (oldest → newest), capped for SPA.
	History         []TrackRef   `json:"history,omitempty"`
	Queue           []TrackRef   `json:"queue"`
	LastFeedback    *FeedbackAck `json:"last_feedback,omitempty"`
	HLSURL          string       `json:"hls_url"`
	ProgressiveURL  string       `json:"progressive_url"`
	CanGoBack       bool         `json:"can_go_back"`
}

// maxHistoryExposed is how many past tracks appear on session status for Now Playing.
const maxHistoryExposed = 8

// TrackRef is a minimal track pointer for API clients.
type TrackRef struct {
	TrackID   int64   `json:"track_id"`
	Score     float64 `json:"score,omitempty"`
	Source    string  `json:"source,omitempty"` // "user_pin" when manually injected
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

// fillQueueFrom tops up auto-prefetched tracks. seedOverride > 0 biases the ANN query
// (e.g. after a like). Already-queued auto tracks are preserved and topped up.
func (w *Worker) fillQueueFrom(ctx context.Context, live *Live, seedOverride int64) error {
	need := w.QueuePrefetch
	pins, oldAuto := w.splitQueue(live)
	// Preserve already-queued auto tracks (and their sources); only top up.
	existingAuto := append([]int64(nil), oldAuto...)
	skips, err := w.DB.ListSkippedTrackIDs(live.UserID, live.ID)
	if err != nil {
		return err
	}
	exclude := map[int64]struct{}{}
	for id := range skips {
		exclude[id] = struct{}{}
	}
	for _, id := range live.Recent {
		exclude[id] = struct{}{}
	}
	for _, id := range pins {
		exclude[id] = struct{}{}
	}
	for _, id := range existingAuto {
		exclude[id] = struct{}{}
	}
	exclude[live.NowPlayingID] = struct{}{}

	needMore := need - len(existingAuto)
	if needMore <= 0 {
		live.Queue = append(pins, existingAuto...)
		if len(live.Queue) > len(pins)+need {
			live.Queue = live.Queue[:len(pins)+need]
		}
		return nil
	}

	prefs, _ := w.DB.GetRadioPrefs(live.UserID)
	seed := seedOverride
	if seed == 0 {
		seed = live.NowPlayingID
	}
	if seed == 0 {
		seed = live.SeedTrackID
	}

	poolK := w.NeighborPool
	if poolK <= 0 {
		poolK = affinity.DefaultPoolSize
	}
	candidates, err := w.queryNeighbors(ctx, seed, poolK, prefs)
	if err != nil || len(candidates) == 0 {
		// Fallback: any non-missing catalog track (enables smoke without embeds).
		ids, ferr := w.DB.ListTrackIDsNotMissing(needMore + 5)
		if ferr != nil {
			live.Queue = append(pins, existingAuto...)
			return nil
		}
		auto := existingAuto
		for _, id := range ids {
			if _, bad := exclude[id]; bad {
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
		aff, _ := w.DB.GetAffinity(live.UserID, n.TrackID)
		pool = append(pool, affinity.Candidate{
			TrackID:   n.TrackID,
			Artist:    t.ArtistName,
			BaseScore: n.Score + aff,
			Source:    "neighbor",
		})
	}

	recentArtists := w.recentArtists(live)
	bridgeCands := w.bridgeCandidates(live, exclude, prefs)
	bridgeDue := prefs.RadioBridge && !live.BridgeQueued && live.TracksSinceBridge >= w.bridgeInterval(live)

	res := affinity.SelectDiversified(pool, affinity.SelectOpts{
		Need:              needMore,
		CooldownWindow:    affinity.DefaultCooldownWindow,
		ArtistCooldown:    prefs.ArtistCooldown,
		ArtistPenalty:     prefs.ArtistPenalty,
		BoundedRandom:     prefs.BoundedRandom,
		TopN:              affinity.DefaultTopN,
		RecentArtists:     recentArtists,
		ArtistPenalties:   live.ArtistPenalties,
		Exclude:           exclude,
		RNG:               w.rng(),
		BridgeEnabled:     bridgeDue,
		TracksSinceBridge: live.TracksSinceBridge,
		BridgeInterval:    w.bridgeInterval(live),
		BridgeCandidates:  bridgeCands,
	})

	w.ensurePinned(live)
	auto := existingAuto
	for _, p := range res.Picks {
		auto = append(auto, p.TrackID)
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

func (w *Worker) bridgeInterval(live *Live) int {
	if live.BridgeInterval > 0 {
		return live.BridgeInterval
	}
	n := affinity.BridgeCadenceMin + w.rng().Intn(affinity.BridgeCadenceMax-affinity.BridgeCadenceMin+1)
	live.BridgeInterval = n
	return n
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

func (w *Worker) recentArtists(live *Live) []string {
	var out []string
	ids := append([]int64{}, live.Recent...)
	if live.NowPlayingID > 0 {
		ids = append(ids, live.NowPlayingID)
	}
	for _, id := range ids {
		t, err := w.DB.GetTrack(id)
		if err != nil || t == nil {
			continue
		}
		out = append(out, t.ArtistName)
	}
	return out
}

func (w *Worker) bridgeCandidates(live *Live, exclude map[int64]struct{}, prefs db.RadioPrefs) []affinity.Candidate {
	if !prefs.RadioBridge {
		return nil
	}
	hits, err := w.DB.ListHighAffinityTracks(live.UserID, 40)
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
		_ = w.fillQueue(ctx, live)
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
	w.onTrackStarted(live, next, src)
	// Commit now_playing immediately so skip feedback can return without waiting
	// on ANN refill or FFmpeg HLS packaging.
	if err := w.persist(live, ""); err != nil {
		return err
	}
	_, _ = w.DB.InsertPlaybackEvent(live.UserID, next, live.ID)
	if w.OnAdvance != nil {
		sid, tid := live.ID, next
		go w.OnAdvance(sid, tid)
	}
	// Top up the auto queue after the lock is released (goroutine waits on mu).
	go func(l *Live) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.Status == db.SessionStatusStopped {
			return
		}
		_ = w.fillQueue(context.Background(), l)
		_ = w.persist(l, "")
	}(live)
	return nil
}

// onTrackStarted updates artist-penalty + Radio Bridge cadence after a track becomes now_playing.
func (w *Worker) onTrackStarted(live *Live, trackID int64, source string) {
	w.ensurePinned(live)
	prefs, _ := w.DB.GetRadioPrefs(live.UserID)
	t, err := w.DB.GetTrack(trackID)
	artist := ""
	if err == nil && t != nil {
		artist = t.ArtistName
	}
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
func (w *Worker) ApplyFeedback(ctx context.Context, live *Live, signal string, trackID int64) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if trackID == 0 {
		trackID = live.NowPlayingID
	}
	if err := w.DB.InsertTrackFeedback(live.UserID, trackID, signal, live.ID); err != nil {
		return err
	}
	ack := FeedbackAck{Signal: signal, TrackID: trackID, At: db.Now()}
	live.LastFeedback = ack
	fb, _ := json.Marshal(ack)

	switch signal {
	case db.SignalLike:
		_, _ = w.DB.UpsertAffinity(live.UserID, trackID, 0.25)
		pins, _ := w.splitQueue(live)
		live.Queue = append([]int64(nil), pins...)
		_ = w.fillQueueFrom(ctx, live, trackID)
	case db.SignalDislike:
		_, _ = w.DB.UpsertAffinity(live.UserID, trackID, -0.25)
		_ = w.DB.AddSkip(live.UserID, trackID, db.SkipScopeSession, live.ID)
		live.Queue = filterOut(live.Queue, trackID)
		// Player thumbs-down: leave the current track immediately (same as skip, stronger penalty).
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		} else {
			_ = w.fillQueue(ctx, live)
		}
	case db.SignalSkip:
		_ = w.DB.AddSkip(live.UserID, trackID, db.SkipScopeSession, live.ID)
		_, _ = w.DB.UpsertAffinity(live.UserID, trackID, -0.1)
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		}
	case db.SignalComplete:
		// Natural end of progressive/HLS item: advance without session-skip or affinity penalty.
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		}
	case db.SignalBan:
		_ = w.DB.AddSkip(live.UserID, trackID, db.SkipScopeLibrary, "")
		_, _ = w.DB.UpsertAffinity(live.UserID, trackID, -1)
		live.Queue = filterOut(live.Queue, trackID)
		if live.NowPlayingID == trackID {
			if err := w.advanceLocked(ctx, live); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown signal %q", signal)
	}
	return w.persist(live, string(fb))
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
	w.onTrackStarted(live, prev, "")
	if err := w.persist(live, ""); err != nil {
		return err
	}
	_, _ = w.DB.InsertPlaybackEvent(live.UserID, prev, live.ID)
	if w.OnAdvance != nil {
		sid, tid := live.ID, prev
		go w.OnAdvance(sid, tid)
	}
	go func(l *Live) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.Status == db.SessionStatusStopped {
			return
		}
		_ = w.fillQueue(context.Background(), l)
		_ = w.persist(l, "")
	}(live)
	_ = ctx
	return nil
}

// InjectQueue pins a track into the upcoming queue (ADR-007 V5b).
// position is "next" (front of pins) or "end" (after existing pins).
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
	if trackID == live.NowPlayingID || contains(live.Queue, trackID) {
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
	pins, auto := w.splitQueue(live)
	if position == "next" {
		pins = append([]int64{trackID}, pins...)
	} else {
		pins = append(pins, trackID)
	}
	live.Pinned[trackID] = struct{}{}
	live.Sources[trackID] = "user_pin"
	live.Queue = append(pins, auto...)
	_ = w.fillQueue(ctx, live)
	return w.persist(live, "")
}

// errQueueConflict is returned when the track is already queued or playing.
var errQueueConflict = fmt.Errorf("track already in queue")

// IsQueueConflict reports whether err is a duplicate inject.
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
	live.mu.Unlock()
	w.enrichTrackRefs(userID, &dto)
	return dto
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

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
	"sync"

	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/lance"
)

// NeighborFn selects similar tracks for the seed (injectable for tests).
type NeighborFn func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error)

// Live holds in-memory trajectory for an active listening session.
type Live struct {
	ID           string
	UserID       int64
	SeedTrackID  int64
	Status       string
	NowPlayingID int64
	Queue        []int64
	Recent       []int64
	LastFeedback FeedbackAck
	mu           sync.Mutex
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
	Queue           []TrackRef   `json:"queue"`
	LastFeedback    *FeedbackAck `json:"last_feedback,omitempty"`
	HLSURL          string       `json:"hls_url"`
	ProgressiveURL  string       `json:"progressive_url"`
}

// TrackRef is a minimal track pointer for API clients.
type TrackRef struct {
	TrackID int64   `json:"track_id"`
	Score   float64 `json:"score,omitempty"`
}

// Worker manages live sessions and persistence.
type Worker struct {
	DB            *db.DB
	Store         *lance.Store
	Neighbors     NeighborFn
	QueuePrefetch int
	MaxConcurrent int
	OnAdvance     func(sessionID string, trackID int64)

	mu       sync.Mutex
	sessions map[string]*Live
}

// NewWorker constructs a session worker.
func NewWorker(catalog *db.DB, store *lance.Store, maxConcurrent, prefetch int) *Worker {
	if prefetch <= 0 {
		prefetch = 2
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	w := &Worker{
		DB:            catalog,
		Store:         store,
		QueuePrefetch: prefetch,
		MaxConcurrent: maxConcurrent,
		sessions:      make(map[string]*Live),
	}
	w.Neighbors = func(ctx context.Context, seedTrackID int64, k int) ([]affinity.Neighbor, error) {
		return affinity.NeighborsWithStore(ctx, catalog, store, seedTrackID, k)
	}
	return w
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
		ID:           id,
		UserID:       userID,
		SeedTrackID:  seedTrackID,
		Status:       db.SessionStatusPlaying,
		NowPlayingID: seedTrackID,
		Recent:       []int64{seedTrackID},
	}
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

func (w *Worker) fillQueue(ctx context.Context, live *Live) error {
	need := w.QueuePrefetch
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
	for _, id := range live.Queue {
		exclude[id] = struct{}{}
	}
	exclude[live.NowPlayingID] = struct{}{}

	seed := live.NowPlayingID
	if seed == 0 {
		seed = live.SeedTrackID
	}
	candidates, err := w.Neighbors(ctx, seed, need*5+10)
	if err != nil || len(candidates) == 0 {
		// Fallback: any non-missing catalog track (enables smoke without embeds).
		ids, ferr := w.DB.ListTrackIDsNotMissing(need + 5)
		if ferr != nil {
			live.Queue = nil
			return nil
		}
		for _, id := range ids {
			if _, bad := exclude[id]; bad {
				continue
			}
			live.Queue = append(live.Queue, id)
			if len(live.Queue) >= need {
				break
			}
		}
		return nil
	}

	type scored struct {
		id    int64
		score float64
	}
	var ranked []scored
	for _, n := range candidates {
		if _, bad := exclude[n.TrackID]; bad {
			continue
		}
		aff, _ := w.DB.GetAffinity(live.UserID, n.TrackID)
		ranked = append(ranked, scored{id: n.TrackID, score: n.Score + aff})
	}
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].score > ranked[i].score {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	live.Queue = live.Queue[:0]
	for _, r := range ranked {
		if len(live.Queue) >= need {
			break
		}
		t, err := w.DB.GetTrack(r.id)
		if err != nil || t == nil || t.MissingAt.Valid {
			continue
		}
		live.Queue = append(live.Queue, r.id)
	}
	return nil
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
		ID:           row.ID,
		UserID:       row.UserID,
		SeedTrackID:  nullInt64(row.SeedTrackID),
		Status:       row.Status,
		NowPlayingID: nullInt64(row.NowPlayingID),
		Queue:        queue,
		Recent:       []int64{},
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
	live.Recent = append(live.Recent, live.NowPlayingID)
	live.NowPlayingID = next
	live.Status = db.SessionStatusPlaying
	_ = w.fillQueue(ctx, live)
	if err := w.persist(live, ""); err != nil {
		return err
	}
	if w.OnAdvance != nil {
		w.OnAdvance(live.ID, next)
	}
	_, _ = w.DB.InsertPlaybackEvent(live.UserID, next, live.ID)
	return nil
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
		ns, err := w.Neighbors(ctx, trackID, w.QueuePrefetch+2)
		if err == nil {
			skips, _ := w.DB.ListSkippedTrackIDs(live.UserID, live.ID)
			seen := map[int64]struct{}{live.NowPlayingID: {}, trackID: {}}
			for _, id := range live.Recent {
				seen[id] = struct{}{}
			}
			var boosted []int64
			for _, n := range ns {
				if _, bad := skips[n.TrackID]; bad {
					continue
				}
				if _, ok := seen[n.TrackID]; ok {
					continue
				}
				boosted = append(boosted, n.TrackID)
				seen[n.TrackID] = struct{}{}
				if len(boosted) >= w.QueuePrefetch {
					break
				}
			}
			rest := make([]int64, 0, len(live.Queue))
			for _, id := range live.Queue {
				if contains(boosted, id) {
					continue
				}
				rest = append(rest, id)
			}
			live.Queue = append(boosted, rest...)
			if len(live.Queue) > w.QueuePrefetch {
				live.Queue = live.Queue[:w.QueuePrefetch]
			}
		}
	case db.SignalDislike:
		_, _ = w.DB.UpsertAffinity(live.UserID, trackID, -0.25)
		_ = w.DB.AddSkip(live.UserID, trackID, db.SkipScopeSession, live.ID)
		live.Queue = filterOut(live.Queue, trackID)
		_ = w.fillQueue(ctx, live)
	case db.SignalSkip:
		_ = w.DB.AddSkip(live.UserID, trackID, db.SkipScopeSession, live.ID)
		_, _ = w.DB.UpsertAffinity(live.UserID, trackID, -0.1)
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
	defer live.mu.Unlock()
	dto := StatusDTO{
		ID:             live.ID,
		UserID:         live.UserID,
		Status:         live.Status,
		SeedTrackID:    live.SeedTrackID,
		HLSURL:         fmt.Sprintf("/api/v1/sessions/%s/hls/index.m3u8", live.ID),
		ProgressiveURL: "",
		Queue:          []TrackRef{},
	}
	if live.NowPlayingID > 0 {
		dto.NowPlaying = &TrackRef{TrackID: live.NowPlayingID}
		dto.ProgressiveURL = fmt.Sprintf("/api/v1/tracks/%d/stream", live.NowPlayingID)
	}
	for _, id := range live.Queue {
		dto.Queue = append(dto.Queue, TrackRef{TrackID: id})
	}
	if live.LastFeedback.Signal != "" {
		ack := live.LastFeedback
		dto.LastFeedback = &ack
	}
	return dto
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

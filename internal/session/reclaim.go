// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-21

package session

import (
	"sort"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// DefaultMaxSessionsPerUser caps live sessions owned by one account.
const DefaultMaxSessionsPerUser = 16

// DefaultSessionIdleTTL stops sessions with no client activity.
const DefaultSessionIdleTTL = 45 * time.Minute

// Touch records client activity so idle reclaim leaves the session alone.
func (w *Worker) Touch(live *Live) {
	if live == nil {
		return
	}
	live.mu.Lock()
	live.LastActive = time.Now().UTC()
	live.mu.Unlock()
}

// ReclaimIdle stops in-memory sessions past IdleTTL and marks stale DB rows stopped.
// Returns how many sessions were stopped (memory + DB).
func (w *Worker) ReclaimIdle() int {
	ttl := w.IdleTTL
	if ttl <= 0 {
		ttl = DefaultSessionIdleTTL
	}
	cut := time.Now().UTC().Add(-ttl)

	var toStop []*Live
	w.mu.Lock()
	for _, s := range w.sessions {
		if s == nil {
			continue
		}
		s.mu.Lock()
		active := s.Status == db.SessionStatusPlaying || s.Status == db.SessionStatusCreated
		last := s.LastActive
		if last.IsZero() {
			last = s.CreatedAt
		}
		s.mu.Unlock()
		if active && !last.IsZero() && last.Before(cut) {
			toStop = append(toStop, s)
		}
	}
	w.mu.Unlock()

	n := 0
	for _, s := range toStop {
		if err := w.Stop(s); err == nil {
			n++
		}
	}
	if w.DB != nil {
		if dbN, err := w.DB.StopStaleListeningSessions(cut); err == nil {
			n += int(dbN)
		}
	}
	return n
}

// ensureCapacity runs idle reclaim, then evicts oldest sessions so creating one
// more for userID stays within per-user and global caps. Prefer the new session.
func (w *Worker) ensureCapacity(userID int64) {
	_ = w.ReclaimIdle()

	maxUser := w.MaxPerUser
	if maxUser <= 0 {
		maxUser = DefaultMaxSessionsPerUser
	}
	maxGlobal := w.MaxConcurrent
	if maxGlobal <= 0 {
		maxGlobal = 64
	}

	for w.countActiveUser(userID) >= maxUser {
		if !w.stopOldest(userID) {
			break
		}
	}
	// After restart, active rows may exist only in the DB.
	if w.DB != nil && userID > 0 {
		if n, err := w.DB.CountActiveListeningSessionsForUser(userID); err == nil && int(n) >= maxUser {
			_, _ = w.DB.StopOldestActiveListeningSessions(userID, int(n)-maxUser+1)
		}
	}
	for w.countActiveGlobal() >= maxGlobal {
		if !w.stopOldest(0) {
			break
		}
	}
}

func (w *Worker) countActiveGlobal() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, s := range w.sessions {
		if s == nil {
			continue
		}
		s.mu.Lock()
		if s.Status == db.SessionStatusPlaying || s.Status == db.SessionStatusCreated {
			n++
		}
		s.mu.Unlock()
	}
	return n
}

func (w *Worker) countActiveUser(userID int64) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, s := range w.sessions {
		if s == nil || s.UserID != userID {
			continue
		}
		s.mu.Lock()
		if s.Status == db.SessionStatusPlaying || s.Status == db.SessionStatusCreated {
			n++
		}
		s.mu.Unlock()
	}
	return n
}

// stopOldest stops the least-recently-active live session. If userID > 0, only
// that user's sessions are considered. Returns false when nothing to stop.
func (w *Worker) stopOldest(userID int64) bool {
	type cand struct {
		live *Live
		at   time.Time
	}
	var list []cand
	w.mu.Lock()
	for _, s := range w.sessions {
		if s == nil {
			continue
		}
		if userID > 0 && s.UserID != userID {
			continue
		}
		s.mu.Lock()
		active := s.Status == db.SessionStatusPlaying || s.Status == db.SessionStatusCreated
		at := s.LastActive
		if at.IsZero() {
			at = s.CreatedAt
		}
		s.mu.Unlock()
		if !active {
			continue
		}
		list = append(list, cand{live: s, at: at})
	}
	w.mu.Unlock()
	if len(list) == 0 {
		return false
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].at.Equal(list[j].at) {
			return list[i].live.ID < list[j].live.ID
		}
		return list[i].at.Before(list[j].at)
	})
	_ = w.Stop(list[0].live)
	return true
}

// LastActiveAt returns the last client activity time (UTC).
func (l *Live) LastActiveAt() time.Time {
	if l == nil {
		return time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.LastActive
}

// ListLiveActive returns in-memory active sessions, optionally filtered by userID (0 = all).
func (w *Worker) ListLiveActive(userID int64) []*Live {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []*Live
	for _, s := range w.sessions {
		if s == nil {
			continue
		}
		if userID > 0 && s.UserID != userID {
			continue
		}
		s.mu.Lock()
		active := s.Status == db.SessionStatusPlaying || s.Status == db.SessionStatusCreated
		s.mu.Unlock()
		if active {
			out = append(out, s)
		}
	}
	return out
}

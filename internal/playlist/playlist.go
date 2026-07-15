// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package playlist

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/lance"
)

// DefaultLength is the default number of tracks in a neighbor playlist (including seed).
const DefaultLength = 20

// MaxLength caps playlist size.
const MaxLength = 100

// Options controls neighbor playlist generation.
type Options struct {
	Length int
	Name   string
	UserID int64 // optional; when >0, stored on the playlist row
}

// Result is a created playlist with hydrated entries.
type Result struct {
	Playlist *db.Playlist
	Entries  []db.PlaylistEntry
}

// CreateFromSeed builds a neighbor playlist: seed first, then top-k similar tracks.
func CreateFromSeed(ctx context.Context, catalog *db.DB, store *lance.Store, seedTrackID int64, opt Options) (*Result, error) {
	seed, err := catalog.GetTrack(seedTrackID)
	if err != nil {
		return nil, err
	}
	if seed == nil {
		return nil, fmt.Errorf("seed track %d not found", seedTrackID)
	}
	length := opt.Length
	if length <= 0 {
		length = DefaultLength
	}
	if length > MaxLength {
		length = MaxLength
	}
	if length < 1 {
		length = 1
	}

	name := opt.Name
	if name == "" {
		name = fmt.Sprintf("Neighbors · %s — %s", seed.ArtistName, seed.Title)
	}

	entries := []db.PlaylistEntry{{
		Position: 0,
		TrackID:  seedTrackID,
		Score:    sql.NullFloat64{}, // seed has no neighbor score
		Track:    seed,
	}}

	need := length - 1
	if need > 0 {
		ns, err := affinity.NeighborsWithStore(ctx, catalog, store, seedTrackID, need)
		if err != nil {
			return nil, err
		}
		for i, n := range ns {
			if len(entries) >= length {
				break
			}
			t, err := catalog.GetTrack(n.TrackID)
			if err != nil {
				return nil, err
			}
			if t == nil || t.MissingAt.Valid {
				continue
			}
			entries = append(entries, db.PlaylistEntry{
				Position: i + 1,
				TrackID:  n.TrackID,
				Score:    sql.NullFloat64{Float64: n.Score, Valid: true},
				Track:    t,
			})
		}
	}

	// renumber after skips
	for i := range entries {
		entries[i].Position = i
	}

	var userID sql.NullInt64
	if opt.UserID > 0 {
		userID = sql.NullInt64{Int64: opt.UserID, Valid: true}
	}
	id, err := catalog.CreatePlaylist(db.CreatePlaylistOpts{
		Name:        name,
		SeedTrackID: sql.NullInt64{Int64: seedTrackID, Valid: true},
		UserID:      userID,
		Kind:        db.PlaylistKindNeighbor,
		Entries:     entries,
	})
	if err != nil {
		return nil, err
	}
	pl, err := catalog.GetPlaylist(id)
	if err != nil {
		return nil, err
	}
	// re-load with joins for consistency
	loaded, err := catalog.ListPlaylistEntries(id)
	if err != nil {
		return nil, err
	}
	return &Result{Playlist: pl, Entries: loaded}, nil
}

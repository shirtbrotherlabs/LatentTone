// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db

import (
	"sort"
	"strings"
)

// SessionKindRadio is continuous affinity Radio.
const SessionKindRadio = "radio"

// SessionKindAlbum is a finite album playthrough (no ANN refill).
const SessionKindAlbum = "album"

// FormatQualityRank ranks container quality for duplicate keep/suppress.
// Higher is better. Bitrate is a secondary boost (kbps).
func FormatQualityRank(format string, bitrateKbps int) int {
	f := strings.ToLower(strings.TrimSpace(format))
	base := 100
	switch f {
	case "flac", "wav", "aiff", "aif":
		base = 1000
	case "alac":
		base = 850
	case "m4a", "mp4", "aac":
		base = 500
	case "ogg", "opus", "oga":
		base = 450
	case "mp3", "mpeg":
		base = 200
	case "wma", "ape", "wv":
		base = 150
	}
	if bitrateKbps < 0 {
		bitrateKbps = 0
	}
	if bitrateKbps > 2000 {
		bitrateKbps = 2000
	}
	return base + bitrateKbps
}

// AlbumDuplicateMark flags tracks that should be suppressed when playing an album.
type AlbumDuplicateMark struct {
	TrackID      int64
	IsDuplicate  bool
	PreferredID  int64 // keeper in the duplicate group (self when keeper)
	Reason       string
}

// MarkAlbumDuplicates within one album: same normalized title + duration ≤1s;
// keep highest FormatQualityRank (bitrate tie-break already in rank).
func MarkAlbumDuplicates(tracks []Track) []AlbumDuplicateMark {
	out := make([]AlbumDuplicateMark, len(tracks))
	type member struct {
		idx   int
		id    int64
		dur   int64
		rank  int
		title string
	}
	byTitle := map[string][]member{}
	for i := range tracks {
		t := &tracks[i]
		out[i] = AlbumDuplicateMark{TrackID: t.ID, PreferredID: t.ID}
		if !t.DurationMS.Valid || t.DurationMS.Int64 <= 0 {
			continue
		}
		key := NormalizeTag(t.Title)
		if key == "" {
			continue
		}
		fmt := ""
		if t.Format.Valid {
			fmt = t.Format.String
		}
		br := 0
		if t.BitrateKbps.Valid {
			br = int(t.BitrateKbps.Int64)
		}
		byTitle[key] = append(byTitle[key], member{
			idx: i, id: t.ID, dur: t.DurationMS.Int64,
			rank: FormatQualityRank(fmt, br), title: t.Title,
		})
	}

	for _, members := range byTitle {
		if len(members) < 2 {
			continue
		}
		// Connected components by duration proximity.
		n := len(members)
		parent := make([]int, n)
		for i := range parent {
			parent[i] = i
		}
		var find func(int) int
		find = func(i int) int {
			if parent[i] != i {
				parent[i] = find(parent[i])
			}
			return parent[i]
		}
		union := func(a, b int) {
			ra, rb := find(a), find(b)
			if ra != rb {
				parent[rb] = ra
			}
		}
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				d := members[i].dur - members[j].dur
				if d < 0 {
					d = -d
				}
				if d <= MaxDuplicateDurationDeltaMS {
					union(i, j)
				}
			}
		}
		comps := map[int][]member{}
		for i := 0; i < n; i++ {
			r := find(i)
			comps[r] = append(comps[r], members[i])
		}
		for _, comp := range comps {
			if len(comp) < 2 {
				continue
			}
			sort.Slice(comp, func(i, j int) bool {
				if comp[i].rank != comp[j].rank {
					return comp[i].rank > comp[j].rank
				}
				return comp[i].id < comp[j].id
			})
			keeper := comp[0]
			for _, m := range comp {
				out[m.idx].PreferredID = keeper.id
				if m.id != keeper.id {
					out[m.idx].IsDuplicate = true
					out[m.idx].Reason = "lower_quality_duplicate"
				}
			}
		}
	}
	return out
}

// PlayableAlbumTrackIDs returns disc/track-ordered ids with duplicates suppressed.
func PlayableAlbumTrackIDs(tracks []Track) []int64 {
	marks := MarkAlbumDuplicates(tracks)
	suppress := map[int64]bool{}
	for _, m := range marks {
		if m.IsDuplicate {
			suppress[m.TrackID] = true
		}
	}
	// Preserve input order (ListTracksByAlbum is already ordered).
	var ids []int64
	for _, t := range tracks {
		if suppress[t.ID] {
			continue
		}
		ids = append(ids, t.ID)
	}
	return ids
}

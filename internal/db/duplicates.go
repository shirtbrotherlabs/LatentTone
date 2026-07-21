// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db

import (
	"sort"
)

// MaxDuplicateDurationDeltaMS is the maximum |duration| gap for a duplicate pair (1s).
const MaxDuplicateDurationDeltaMS = 1000

// DuplicateTrack is one member of a duplicate group.
type DuplicateTrack struct {
	TrackID    int64
	Title      string
	Album      string
	Artist     string
	Path       string
	DurationMS int64
	CoverPath  string
}

// DuplicateGroup is a set of tracks that match on normalized tags and near-equal duration.
type DuplicateGroup struct {
	Key        string
	Title      string
	Album      string
	Artist     string
	DurationMS int64 // representative (min in group)
	Tracks     []DuplicateTrack
}

// ListDuplicateGroups finds catalog duplicates by normalized title+album+artist
// and duration within MaxDuplicateDurationDeltaMS. limitGroups caps returned groups
// (0 → 200). Requires duration_ms set on both sides of a pair.
func (d *DB) ListDuplicateGroups(limitGroups int) ([]DuplicateGroup, error) {
	if limitGroups <= 0 {
		limitGroups = 200
	}
	if limitGroups > 2000 {
		limitGroups = 2000
	}
	rows, err := d.SQL.Query(`
SELECT t.id, t.title, COALESCE(al.title, ''), COALESCE(a.name, ''), t.path,
       t.duration_ms, COALESCE(al.cover_path, '')
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.missing_at IS NULL AND t.duration_ms IS NOT NULL AND t.duration_ms > 0
ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		id, dur                                                   int64
		title, album, artist, path, cover, key                    string
	}
	byKey := map[string][]row{}
	for rows.Next() {
		var r row
		var dur int64
		if err := rows.Scan(&r.id, &r.title, &r.album, &r.artist, &r.path, &dur, &r.cover); err != nil {
			return nil, err
		}
		r.dur = dur
		r.key = DuplicateKey(r.title, r.album, r.artist)
		if r.key == "\x1f\x1f" || NormalizeTag(r.title) == "" {
			continue
		}
		byKey[r.key] = append(byKey[r.key], r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var groups []DuplicateGroup
	for key, members := range byKey {
		if len(members) < 2 {
			continue
		}
		// Connected components where |dur_i - dur_j| <= 1s.
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
				delta := members[i].dur - members[j].dur
				if delta < 0 {
					delta = -delta
				}
				if delta <= MaxDuplicateDurationDeltaMS {
					union(i, j)
				}
			}
		}
		comps := map[int][]row{}
		for i := 0; i < n; i++ {
			r := find(i)
			comps[r] = append(comps[r], members[i])
		}
		for _, comp := range comps {
			if len(comp) < 2 {
				continue
			}
			sort.Slice(comp, func(i, j int) bool { return comp[i].id < comp[j].id })
			minDur := comp[0].dur
			for _, m := range comp[1:] {
				if m.dur < minDur {
					minDur = m.dur
				}
			}
			g := DuplicateGroup{
				Key:        key,
				Title:      comp[0].title,
				Album:      comp[0].album,
				Artist:     comp[0].artist,
				DurationMS: minDur,
				Tracks:     make([]DuplicateTrack, 0, len(comp)),
			}
			for _, m := range comp {
				g.Tracks = append(g.Tracks, DuplicateTrack{
					TrackID:    m.id,
					Title:      m.title,
					Album:      m.album,
					Artist:     m.artist,
					Path:       m.path,
					DurationMS: m.dur,
					CoverPath:  m.cover,
				})
			}
			groups = append(groups, g)
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Tracks) != len(groups[j].Tracks) {
			return len(groups[i].Tracks) > len(groups[j].Tracks)
		}
		if groups[i].Artist != groups[j].Artist {
			return groups[i].Artist < groups[j].Artist
		}
		return groups[i].Title < groups[j].Title
	})
	if len(groups) > limitGroups {
		groups = groups[:limitGroups]
	}
	return groups, nil
}

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db_test

import (
	"database/sql"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

func TestMarkAlbumDuplicatesKeepsFlacOverMp3(t *testing.T) {
	ms := int64(200000)
	tracks := []db.Track{
		{
			ID: 1, Title: "Harder and Faster",
			DurationMS: sql.NullInt64{Int64: ms, Valid: true},
			Format:     sql.NullString{String: "mp3", Valid: true},
			BitrateKbps: sql.NullInt64{Int64: 320, Valid: true},
		},
		{
			ID: 2, Title: "Harder and Faster!",
			DurationMS: sql.NullInt64{Int64: ms, Valid: true},
			Format:     sql.NullString{String: "flac", Valid: true},
		},
		{
			ID: 3, Title: "Other Song",
			DurationMS: sql.NullInt64{Int64: ms, Valid: true},
			Format:     sql.NullString{String: "mp3", Valid: true},
			BitrateKbps: sql.NullInt64{Int64: 192, Valid: true},
		},
	}
	marks := db.MarkAlbumDuplicates(tracks)
	byID := map[int64]db.AlbumDuplicateMark{}
	for _, m := range marks {
		byID[m.TrackID] = m
	}
	if !byID[1].IsDuplicate || byID[1].PreferredID != 2 {
		t.Fatalf("mp3 should be duplicate of flac: %+v", byID[1])
	}
	if byID[2].IsDuplicate {
		t.Fatalf("flac should be keeper: %+v", byID[2])
	}
	if byID[3].IsDuplicate {
		t.Fatalf("unique track must not be marked: %+v", byID[3])
	}
	ids := db.PlayableAlbumTrackIDs(tracks)
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("playable ids %#v", ids)
	}
}

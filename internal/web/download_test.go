// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package web

import (
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

func TestDownloadFilename(t *testing.T) {
	t.Parallel()
	got := downloadFilename(&db.Track{ID: 9, ArtistName: `AC/DC`, Title: `Back in Black`}, "mp3")
	if got != "ACDC - Back in Black.mp3" {
		t.Fatalf("got %q", got)
	}
	got = downloadFilename(&db.Track{ID: 3, ArtistName: "", Title: ""}, "flac")
	if got != "track_3.flac" {
		t.Fatalf("fallback got %q", got)
	}
	got = downloadFilename(&db.Track{ID: 1, ArtistName: "Art", Title: `Say "Hello"`}, "opus")
	if got != "Art - Say Hello.opus" {
		t.Fatalf("quotes stripped got %q", got)
	}
}

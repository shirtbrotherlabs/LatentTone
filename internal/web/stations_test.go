// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func TestMeStationsAPI(t *testing.T) {
	dir := t.TempDir()
	catalog, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close() })

	tn := 1
	trackID, err := catalog.UpsertTrack(db.TrackInput{
		Path: "a/1.mp3", Title: "Seed Song", Album: "Alb", AlbumArtist: "Art",
		Artists: []string{"Art"}, Format: "mp3", TrackNumber: &tn, FileMtime: 1, FileSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		LibraryRoot:  dir,
		DatabasePath: filepath.Join(dir, "t.db"),
		ListenAddr:   ":0",
		AuthMode:     "authenticated",
		HLSRoot:      filepath.Join(dir, "hls"),
	}
	mcfg := &meta.Config{LibraryRoot: dir, DatabasePath: cfg.DatabasePath}
	srv, err := web.New(cfg, mcfg, catalog, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	token := register(t, h, "stationuser")

	rr := doJSON(t, h, http.MethodGet, "/api/v1/me/stations", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth want 401 got %d", rr.Code)
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/auth/me", token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("me %d %s", rr.Code, rr.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	userID := int64(me["id"].(float64))

	if _, err := catalog.CreateListeningSession("sess-a", userID, trackID); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpdateListeningSessionState("sess-a", db.SessionStatusStopped, trackID, nil, "", ""); err != nil {
		t.Fatal(err)
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/me/stations?limit=5", token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET stations %d %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Stations []map[string]any `json:"stations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Stations) != 1 {
		t.Fatalf("want 1 station got %d", len(body.Stations))
	}
	st := body.Stations[0]
	if st["id"] != "sess-a" {
		t.Fatalf("id %#v", st["id"])
	}
	if st["status"] != db.SessionStatusStopped {
		t.Fatalf("status %#v", st["status"])
	}
	if st["stopped_at"] == nil || st["stopped_at"] == "" {
		t.Fatalf("missing stopped_at %#v", st)
	}
	seed, _ := st["seed_track"].(map[string]any)
	if seed == nil || seed["title"] != "Seed Song" {
		t.Fatalf("seed_track %#v", st["seed_track"])
	}
}

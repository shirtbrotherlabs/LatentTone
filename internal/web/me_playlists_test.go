// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/playlist"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func setupPlaylistAPI(t *testing.T) (http.Handler, *db.DB, int64, int64, int64) {
	t.Helper()
	dir := t.TempDir()
	catalog, dsn := dbtest.Open(t)

	mk := func(path, title string) int64 {
		tn := 1
		id, err := catalog.UpsertTrack(db.TrackInput{
			Path: path, Title: title, Album: "Alb", AlbumArtist: "Art",
			Artists: []string{"Art"}, Format: "mp3", TrackNumber: &tn, FileMtime: 1, FileSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	t1 := mk("a/1.mp3", "One")
	t2 := mk("a/2.mp3", "Two")
	t3 := mk("a/3.mp3", "Three")

	cfg := &config.Config{
		LibraryRoot: dir,
		DatabaseDSN: dsn,
		ListenAddr:  ":0",
		AuthMode:    "authenticated",
		HLSRoot:     filepath.Join(dir, "hls"),
	}
	mcfg := &meta.Config{LibraryRoot: dir, DatabaseDSN: cfg.DatabaseDSN}
	srv, err := web.New(cfg, mcfg, catalog, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler(), catalog, t1, t2, t3
}

func register(t *testing.T, h http.Handler, username string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":"secretpass"}`, username)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", username, rr.Code, rr.Body.String())
	}
	var reg map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	token, _ := reg["token"].(string)
	if token == "" {
		t.Fatal("missing token")
	}
	return token
}

func doJSON(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = bytes.NewBuffer(nil)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	h.ServeHTTP(rr, req)
	return rr
}

func TestMePlaylistsCRUDAndIsolation(t *testing.T) {
	h, _, t1, t2, t3 := setupPlaylistAPI(t)
	alice := register(t, h, "alice")
	bob := register(t, h, "bob")

	rr := doJSON(t, h, http.MethodPost, "/api/v1/me/playlists", "", `{"name":"Nope"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unauth create want 403 got %d", rr.Code)
	}

	rr = doJSON(t, h, http.MethodPost, "/api/v1/me/playlists", alice, `{"name":"Alice Mix"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	pid := int64(created["id"].(float64))
	if created["kind"] != "user" || created["name"] != "Alice Mix" {
		t.Fatalf("bad create %+v", created)
	}

	rr = doJSON(t, h, http.MethodPost, fmt.Sprintf("/api/v1/me/playlists/%d/tracks", pid), alice,
		fmt.Sprintf(`{"track_ids":[%d,%d,%d]}`, t1, t2, t3))
	if rr.Code != http.StatusOK {
		t.Fatalf("add tracks %d %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodPut, fmt.Sprintf("/api/v1/me/playlists/%d/tracks/order", pid), alice,
		fmt.Sprintf(`{"track_ids":[%d,%d,%d]}`, t3, t1, t2))
	if rr.Code != http.StatusOK {
		t.Fatalf("reorder %d %s", rr.Code, rr.Body.String())
	}
	var ordered map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &ordered); err != nil {
		t.Fatal(err)
	}
	tracks, _ := ordered["tracks"].([]any)
	if len(tracks) != 3 {
		t.Fatalf("tracks=%d", len(tracks))
	}
	first := tracks[0].(map[string]any)
	if int64(first["track_id"].(float64)) != t3 {
		t.Fatalf("want first=%d got %v", t3, first["track_id"])
	}

	rr = doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/me/playlists/%d/tracks/%d", pid, t1), alice, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("remove %d %s", rr.Code, rr.Body.String())
	}

	rr = doJSON(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/me/playlists/%d", pid), alice, `{"name":"Renamed"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("rename %d %s", rr.Code, rr.Body.String())
	}

	// Bob cannot read/mutate Alice's playlist (404).
	rr = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/v1/me/playlists/%d", pid), bob, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bob get want 404 got %d", rr.Code)
	}
	rr = doJSON(t, h, http.MethodPatch, fmt.Sprintf("/api/v1/me/playlists/%d", pid), bob, `{"name":"Hijack"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bob patch want 404 got %d", rr.Code)
	}
	rr = doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/me/playlists/%d", pid), bob, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bob delete want 404 got %d", rr.Code)
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/me/playlists", bob, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("bob list %d", rr.Code)
	}
	var bobList map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &bobList)
	if pls, _ := bobList["playlists"].([]any); len(pls) != 0 {
		t.Fatalf("bob should see zero playlists, got %v", bobList)
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/me/playlists", alice, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("alice list %d", rr.Code)
	}
	var aliceList map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &aliceList)
	if pls, _ := aliceList["playlists"].([]any); len(pls) != 1 {
		t.Fatalf("alice list want 1 got %v", aliceList)
	}

	rr = doJSON(t, h, http.MethodDelete, fmt.Sprintf("/api/v1/me/playlists/%d", pid), alice, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("delete %d %s", rr.Code, rr.Body.String())
	}
	rr = doJSON(t, h, http.MethodGet, fmt.Sprintf("/api/v1/me/playlists/%d", pid), alice, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("after delete want 404 got %d", rr.Code)
	}
}

func TestNeighborGenerateStillWorksAndFromNeighbor(t *testing.T) {
	h, catalog, t1, t2, t3 := setupPlaylistAPI(t)
	alice := register(t, h, "alice")

	// Seed vectors so CreateFromSeed can rank (optional store=nil uses catalog-stored vectors).
	mkVec := func(id int64, v []float32) {
		if _, err := catalog.EnsureVectorRows("t", `{}`); err != nil {
			t.Fatal(err)
		}
		if err := catalog.MarkVectorReady(id, "t", `{}`, v, 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	mkVec(t1, []float32{1, 0, 0})
	mkVec(t2, []float32{0.9, 0.1, 0})
	mkVec(t3, []float32{0, 1, 0})

	rr := doJSON(t, h, http.MethodPost, "/api/v1/playlists", "",
		fmt.Sprintf(`{"seed_track_id":%d,"length":2}`, t1))
	if rr.Code != http.StatusCreated {
		t.Fatalf("neighbor unauth create %d %s", rr.Code, rr.Body.String())
	}
	var neigh map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &neigh)
	if neigh["kind"] != "neighbor" {
		t.Fatalf("kind=%v", neigh["kind"])
	}
	nid := int64(neigh["id"].(float64))

	rr = doJSON(t, h, http.MethodPost, "/api/v1/playlists", alice,
		fmt.Sprintf(`{"seed_track_id":%d,"length":2,"name":"Auth Neighbor"}`, t1))
	if rr.Code != http.StatusCreated {
		t.Fatalf("neighbor auth create %d %s", rr.Code, rr.Body.String())
	}
	var neighAuth map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &neighAuth)
	if neighAuth["user_id"] == nil {
		t.Fatalf("expected user_id when authenticated: %+v", neighAuth)
	}

	rr = doJSON(t, h, http.MethodPost, "/api/v1/me/playlists/from-neighbor", alice,
		fmt.Sprintf(`{"playlist_id":%d,"name":"Promoted"}`, nid))
	if rr.Code != http.StatusCreated {
		t.Fatalf("from-neighbor %d %s", rr.Code, rr.Body.String())
	}
	var promoted map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &promoted)
	if promoted["kind"] != "user" || promoted["name"] != "Promoted" {
		t.Fatalf("promoted %+v", promoted)
	}

	// Package helper still works directly.
	res, err := playlist.CreateFromSeed(context.Background(), catalog, nil, t1, playlist.Options{Length: 2})
	if err != nil || res.Playlist == nil {
		t.Fatalf("CreateFromSeed %v %+v", err, res)
	}
}

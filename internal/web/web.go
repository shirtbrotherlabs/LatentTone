// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/api"
	"github.com/shirtbrotherlabs/LatentTone/internal/affinity"
	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/lance"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/playlist"
	"github.com/shirtbrotherlabs/LatentTone/internal/scan"
	"github.com/shirtbrotherlabs/LatentTone/internal/session"
	"github.com/shirtbrotherlabs/LatentTone/internal/stream"
	"github.com/shirtbrotherlabs/LatentTone/internal/web/apidocs"
)

//go:embed templates/*.html
var templateFS embed.FS

// NeighborView is a similar track for templates.
type NeighborView struct {
	Track *db.Track
	Score float64
}

// FeatureView is pretty-printed extractor output.
type FeatureView struct {
	Extractor    string
	ModelVersion string
	PrettyJSON   string
	VectorDim    string
}

// Server is the catalog browse UI with Phase 2 embed controls and Phase 3 APIs.
type Server struct {
	Cfg      *config.Config
	MetaCfg  *meta.Config
	DB       *db.DB
	Scanner  *scan.Scanner
	Embed    *meta.Controller
	ServeCtx context.Context
	Log      *log.Logger
	Auth     *auth.Manager
	Sessions *session.Worker
	HLS      *stream.Manager

	tmpl *template.Template

	scanMu   sync.Mutex
	scanning bool
	lastScan string
}

// New constructs the browse / API server.
func New(cfg *config.Config, metaCfg *meta.Config, catalog *db.DB, scanner *scan.Scanner, embedCtrl *meta.Controller, serveCtx context.Context) (*Server, error) {
	funcs := template.FuncMap{
		"duration": db.FormatDuration,
		"printf":   fmt.Sprintf,
	}
	tmpl, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(cfg.HLSRoot, 0o755)
	hls := stream.NewManager(cfg.HLSRoot, cfg.LibraryRoot, cfg.FFmpegPath, cfg.HLSTTL)
	var lanceStore *lance.Store
	if metaCfg != nil && metaCfg.LanceDBPath != "" {
		lanceStore = &lance.Store{
			DBPath:     metaCfg.LanceDBPath,
			Table:      metaCfg.LanceDBTable,
			HelperPath: metaCfg.LanceHelperPath,
		}
	}
	worker := session.NewWorker(catalog, lanceStore, cfg.MaxSessions, cfg.QueuePrefetch)
	worker.OnAdvance = func(sessionID string, trackID int64) {
		t, err := catalog.GetTrack(trackID)
		if err != nil || t == nil {
			return
		}
		_ = hls.EnsureHLS(sessionID, t.Path)
	}
	authMgr := auth.NewManager(catalog, cfg.AuthMode, cfg.SessionTTL, cfg.SecureCookie)
	s := &Server{
		Cfg:      cfg,
		MetaCfg:  metaCfg,
		DB:       catalog,
		Scanner:  scanner,
		Embed:    embedCtrl,
		ServeCtx: serveCtx,
		Log:      log.Default(),
		Auth:     authMgr,
		Sessions: worker,
		HLS:      hls,
		tmpl:     tmpl,
	}
	if serveCtx != nil {
		go func() {
			t := time.NewTicker(15 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-serveCtx.Done():
					return
				case <-t.C:
					hls.SweepTTL()
				}
			}
		}()
	}
	return s, nil
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHome)
	mux.HandleFunc("/artists", s.handleArtists)
	mux.HandleFunc("/artists/", s.handleArtist)
	mux.HandleFunc("/albums/", s.handleAlbum)
	mux.HandleFunc("/tracks", s.handleTracks)
	mux.HandleFunc("/tracks/", s.handleTrack)
	mux.HandleFunc("/playlists/", s.handlePlaylistPage)
	mux.HandleFunc("/lance", s.handleLance)
	mux.HandleFunc("/covers/", s.handleCover)
	mux.HandleFunc("/scan", s.handleScan)
	mux.HandleFunc("/embed/start", s.handleEmbedStart)
	mux.HandleFunc("/embed/stop", s.handleEmbedStop)
	mux.HandleFunc("/embed/status", s.handleEmbedStatus)
	mux.HandleFunc("/api/embed/status", s.handleEmbedStatus)
	mux.HandleFunc("/api/v1/playlists", s.handleAPIPlaylists)
	mux.HandleFunc("/api/v1/playlists/", s.handleAPIPlaylist)
	mux.HandleFunc("/api/v1/me/playlists", s.handleMePlaylists)
	mux.HandleFunc("/api/v1/me/playlists/", s.handleMePlaylists)

	mux.HandleFunc("/api/v1/auth/register", s.handleAuthRegister)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/v1/auth/me", s.handleAuthMe)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/sessions/", s.handleSessions)
	mux.HandleFunc("/api/v1/tracks/", s.handleAPITracks)

	if s.Cfg.EnableStreamProbe {
		mux.HandleFunc("/dev/stream", s.handleDevStream)
	}
	if s.Cfg.EnableAPIDocs {
		apidocs.Register(mux, api.OpenAPIYAML)
	}
	return s.Auth.Middleware(mux)
}

func (s *Server) handleAPITracks(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/stream") || strings.Contains(r.URL.Path, "/stream") {
		s.handleTrackStream(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.scanMu.Lock()
	scanning := s.scanning
	last := s.lastScan
	s.scanMu.Unlock()
	artists, albums, tracks, _ := s.DB.Counts()
	ready, pending, processing, _, _, _, _ := s.DB.VectorStatusCounts()
	scanners := s.scannerStatus(tracks)
	embedRunning := s.Embed != nil && s.Embed.Running()
	embedDone, embedClaimed, embedPct := 0, 0, 0
	if s.Embed != nil {
		p := s.Embed.ProgressSnapshot()
		embedDone = p.Done
		embedClaimed = p.Claimed
		if embedClaimed > 0 {
			embedPct = (100 * embedDone) / embedClaimed
			if embedPct > 100 {
				embedPct = 100
			}
		}
	}
	data := map[string]any{
		"Title":              "LatentTone Catalog",
		"Artists":            artists,
		"Albums":             albums,
		"Tracks":             tracks,
		"LastScan":           last,
		"Scanning":           scanning,
		"EmbedRunning":       embedRunning,
		"LastEmbed":          "",
		"MaxTracks":          0,
		"SampleMode":         "",
		"EmbedConcurrency":   4,
		"IdentityReady":      ready,
		"IdentityPending":    pending,
		"IdentityProcessing": processing,
		"EmbedDone":          embedDone,
		"EmbedClaimed":       embedClaimed,
		"EmbedPct":           embedPct,
		"Scanners":           scanners,
	}
	if s.Embed != nil {
		data["LastEmbed"] = s.Embed.LastStatus()
	}
	if s.MetaCfg != nil {
		webCfg := s.MetaCfg.ForWebStart()
		data["MaxTracks"] = webCfg.MaxTracks
		data["SampleMode"] = webCfg.SampleMode
		data["EmbedConcurrency"] = webCfg.Concurrency
	}
	s.render(w, "home.html", data)
}

func (s *Server) handleArtists(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.ListArtists()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "artists.html", map[string]any{"Title": "Artists", "Artists": list})
}

func (s *Server) handleArtist(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r.URL.Path, "/artists/")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	artist, err := s.DB.GetArtist(id)
	if err != nil || artist == nil {
		http.NotFound(w, r)
		return
	}
	albums, err := s.DB.ListAlbumsByArtist(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "artist.html", map[string]any{
		"Title":  artist.Name,
		"Artist": artist,
		"Albums": albums,
	})
}

func (s *Server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r.URL.Path, "/albums/")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	album, err := s.DB.GetAlbum(id)
	if err != nil || album == nil {
		http.NotFound(w, r)
		return
	}
	tracks, err := s.DB.ListTracksByAlbum(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "album.html", map[string]any{
		"Title":  album.Title,
		"Album":  album,
		"Tracks": tracks,
	})
}

func (s *Server) handleTracks(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.ListTracks(500)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "tracks.html", map[string]any{"Title": "Tracks", "Tracks": list})
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r.URL.Path, "/tracks/")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	track, err := s.DB.GetTrack(id)
	if err != nil || track == nil {
		http.NotFound(w, r)
		return
	}

	var vecStatus, vecErr, extractorSet, modelVersions, vectorDim string
	vec, _ := s.DB.GetTrackVector(id)
	if vec != nil {
		vecStatus = vec.Status
		extractorSet = vec.ExtractorSet
		modelVersions = vec.ModelVersions
		if vec.VectorDim.Valid {
			vectorDim = strconv.FormatInt(vec.VectorDim.Int64, 10)
		}
		if vec.ErrorMessage.Valid {
			vecErr = vec.ErrorMessage.String
		}
	} else {
		vecStatus = "none"
	}

	rawFeats, _ := s.DB.ListTrackFeatures(id)
	var feats []FeatureView
	for _, f := range rawFeats {
		dim := "—"
		if f.VectorDim.Valid {
			dim = strconv.FormatInt(f.VectorDim.Int64, 10)
		}
		feats = append(feats, FeatureView{
			Extractor:    f.Extractor,
			ModelVersion: f.ModelVersion,
			PrettyJSON:   db.FeaturesToPrettyJSON(f.FeaturesJSON),
			VectorDim:    dim,
		})
	}

	var neighbors []NeighborView
	if vec != nil && vec.Status == db.VecReady {
		ns, err := affinity.NeighborsWithStore(r.Context(), s.DB, s.lanceStore(), id, 8)
		if err == nil {
			for _, n := range ns {
				t, _ := s.DB.GetTrack(n.TrackID)
				if t == nil {
					continue
				}
				neighbors = append(neighbors, NeighborView{Track: t, Score: n.Score})
			}
		}
	}

	recent, _ := s.DB.ListPlaylistsForSeed(id, 5)

	s.render(w, "track.html", map[string]any{
		"Title":           track.Title,
		"Track":           track,
		"VecStatus":       vecStatus,
		"VecError":        vecErr,
		"ExtractorSet":    extractorSet,
		"ModelVersions":   modelVersions,
		"VectorDim":       vectorDim,
		"Features":        feats,
		"Neighbors":       neighbors,
		"RecentPlaylists": recent,
	})
}

func (s *Server) lanceStore() *lance.Store {
	if s.MetaCfg == nil {
		return nil
	}
	return &lance.Store{
		DBPath:     s.MetaCfg.LanceDBPath,
		Table:      s.MetaCfg.LanceDBTable,
		HelperPath: s.MetaCfg.LanceHelperPath,
	}
}

func (s *Server) handlePlaylistPage(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r.URL.Path, "/playlists/")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pl, err := s.DB.GetPlaylist(id)
	if err != nil || pl == nil {
		http.NotFound(w, r)
		return
	}
	entries, err := s.DB.ListPlaylistEntries(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "playlist.html", map[string]any{
		"Title":    pl.Name,
		"Playlist": pl,
		"Entries":  entries,
	})
}

type createPlaylistBody struct {
	SeedTrackID int64  `json:"seed_track_id"`
	Length      int    `json:"length"`
	Name        string `json:"name"`
}

func (s *Server) handleAPIPlaylists(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/playlists" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.apiCreatePlaylist(w, r)
	default:
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIPlaylist(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r.URL.Path, "/api/v1/playlists/")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	pl, err := s.DB.GetPlaylist(id)
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return
	}
	if pl == nil {
		writeJSONError(w, 404, "playlist not found")
		return
	}
	entries, err := s.DB.ListPlaylistEntries(id)
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, playlistJSON(pl, entries))
}

func (s *Server) apiCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body createPlaylistBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeJSONError(w, 400, "invalid JSON body")
		return
	}
	if body.SeedTrackID <= 0 {
		writeJSONError(w, 400, "seed_track_id required")
		return
	}
	opt := playlist.Options{
		Length: body.Length,
		Name:   body.Name,
	}
	if u := auth.UserFrom(r.Context()); u != nil {
		opt.UserID = u.ID
	}
	res, err := playlist.CreateFromSeed(r.Context(), s.DB, s.lanceStore(), body.SeedTrackID, opt)
	if err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, playlistJSON(res.Playlist, res.Entries))
}

func playlistHeaderJSON(pl *db.Playlist) map[string]any {
	out := map[string]any{
		"id":         pl.ID,
		"name":       pl.Name,
		"kind":       pl.Kind,
		"length":     pl.Length,
		"created_at": pl.CreatedAt,
		"updated_at": pl.UpdatedAt,
	}
	if pl.SeedTrackID.Valid {
		out["seed_track_id"] = pl.SeedTrackID.Int64
	} else {
		out["seed_track_id"] = nil
	}
	if pl.UserID.Valid {
		out["user_id"] = pl.UserID.Int64
	} else {
		out["user_id"] = nil
	}
	return out
}

func playlistJSON(pl *db.Playlist, entries []db.PlaylistEntry) map[string]any {
	out := playlistHeaderJSON(pl)
	tracks := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		row := map[string]any{
			"position": e.Position,
			"track_id": e.TrackID,
			"score":    nil,
		}
		if e.Score.Valid {
			row["score"] = e.Score.Float64
		}
		if e.Track != nil {
			row["title"] = e.Track.Title
			row["artist"] = e.Track.ArtistName
			row["album"] = e.Track.AlbumTitle
			if e.Track.DurationMS.Valid {
				row["duration_ms"] = e.Track.DurationMS.Int64
			}
		}
		tracks = append(tracks, row)
	}
	out["tracks"] = tracks
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/covers/")
	rel = filepath.Clean("/" + rel)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	root := filepath.Clean(s.Cfg.LibraryRoot)
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, abs)
}

func (s *Server) handleLance(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/lance" {
		http.NotFound(w, r)
		return
	}
	limit := queryInt(r, "limit", 50)
	if limit > 500 {
		limit = 500
	}
	offset := queryInt(r, "offset", 0)
	preview := queryInt(r, "preview", 8)
	if preview > 32 {
		preview = 32
	}

	if s.MetaCfg == nil {
		s.render(w, "lance.html", map[string]any{
			"Title": "LanceDB",
			"Error": "metadata config not loaded — cannot locate LanceDB path",
		})
		return
	}
	store := &lance.Store{
		DBPath:     s.MetaCfg.LanceDBPath,
		Table:      s.MetaCfg.LanceDBTable,
		HelperPath: s.MetaCfg.LanceHelperPath,
	}
	if !store.Enabled() {
		s.render(w, "lance.html", map[string]any{
			"Title": "LanceDB",
			"Error": "LanceDB not configured in metadata.yaml",
		})
		return
	}

	dump, err := store.Dump(r.Context(), limit, offset, preview)
	if err != nil {
		s.render(w, "lance.html", map[string]any{
			"Title": "LanceDB",
			"Error": err.Error(),
		})
		return
	}
	if dump.Error != "" {
		s.render(w, "lance.html", map[string]any{
			"Title":  "LanceDB",
			"Error":  dump.Error,
			"DBPath": dump.DBPath,
			"Table":  dump.Table,
			"Tables": dump.Tables,
		})
		return
	}

	if r.URL.Query().Get("raw") == "1" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(dump)
		return
	}

	type rowView struct {
		TrackID     int64
		Title       string
		Artist      string
		VectorDim   int
		PreviewText string
		TailText    string
	}
	rows := make([]rowView, 0, len(dump.Rows))
	for _, row := range dump.Rows {
		rv := rowView{
			TrackID:     row.TrackID,
			VectorDim:   row.VectorDim,
			PreviewText: formatFloatPreview(row.VectorPreview),
			TailText:    formatFloatPreview(row.VectorTail),
		}
		if t, err := s.DB.GetTrack(row.TrackID); err == nil && t != nil {
			rv.Title = t.Title
			rv.Artist = t.ArtistName
		}
		rows = append(rows, rv)
	}

	end := offset + len(rows)
	s.render(w, "lance.html", map[string]any{
		"Title":        "LanceDB",
		"DBPath":       dump.DBPath,
		"Table":        dump.Table,
		"Tables":       dump.Tables,
		"Count":        dump.Count,
		"Offset":       dump.Offset,
		"Limit":        dump.Limit,
		"Preview":      dump.Preview,
		"EndExclusive": end,
		"HasPrev":      offset > 0,
		"HasNext":      end < dump.Count,
		"PrevOffset":   maxInt(0, offset-limit),
		"NextOffset":   end,
		"Rows":         rows,
	})
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func formatFloatPreview(v []float64) string {
	if len(v) == 0 {
		return "—"
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(x, 'f', 4, 64)
	}
	return strings.Join(parts, ", ")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	s.scanMu.Lock()
	if s.scanning {
		s.scanMu.Unlock()
		http.Error(w, "scan already running", http.StatusConflict)
		return
	}
	s.scanning = true
	s.scanMu.Unlock()

	go func() {
		defer func() {
			s.scanMu.Lock()
			s.scanning = false
			s.scanMu.Unlock()
		}()
		res, err := s.Scanner.Full("api")
		s.scanMu.Lock()
		if err != nil {
			s.lastScan = fmt.Sprintf("error: %v", err)
		} else {
			s.lastScan = fmt.Sprintf("%s seen=%d upserted=%d missing=%d",
				time.Now().UTC().Format(time.RFC3339), res.Seen, res.Upserted, res.Missing)
		}
		s.scanMu.Unlock()
	}()

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleEmbedStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if s.Embed == nil || s.MetaCfg == nil {
		http.Error(w, "embed not configured", http.StatusServiceUnavailable)
		return
	}
	ctx := s.ServeCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Embed.Start(ctx, s.MetaCfg.ForWebStart(), s.DB, "api"); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleEmbedStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if s.Embed == nil {
		http.Error(w, "embed not configured", http.StatusServiceUnavailable)
		return
	}
	_ = s.Embed.Stop()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleEmbedStatus(w http.ResponseWriter, r *http.Request) {
	ready, pending, processing, errorN, stale, catalogTracks, _ := s.DB.VectorStatusCounts()
	out := map[string]any{
		"running":        false,
		"claimed":        0,
		"done":           0,
		"ok":             0,
		"errors":         0,
		"last":           "",
		"ready":          ready,
		"pending":        pending,
		"processing":     processing,
		"error":          errorN,
		"stale":          stale,
		"catalog_tracks": catalogTracks,
		"scanners":       s.scannerStatus(catalogTracks),
		"extractors":     []any{},
	}
	if s.Embed != nil {
		p := s.Embed.ProgressSnapshot()
		out["running"] = p.Running
		out["claimed"] = p.Claimed
		out["done"] = p.Done
		out["ok"] = p.OK
		out["errors"] = p.Errors
		out["last"] = p.Last
		out["extractors"] = p.Extractors
		// Merge live per-run counters onto scanner coverage snapshot.
		// Keep "enabled" from metadata.yaml — Controller only knows enabled
		// extractors while a job is running.
		live := make(map[string]meta.ExtractorProgress, len(p.Extractors))
		for _, ex := range p.Extractors {
			live[ex.Name] = ex
		}
		scanners, _ := out["scanners"].([]map[string]any)
		for i := range scanners {
			name, _ := scanners[i]["name"].(string)
			if lp, ok := live[name]; ok {
				scanners[i]["run_done"] = lp.Done
				scanners[i]["run_ok"] = lp.OK
				scanners[i]["run_errors"] = lp.Errors
				if p.Running {
					scanners[i]["enabled"] = lp.Enabled
				}
			}
		}
		out["scanners"] = scanners
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// scannerStatus builds catalog coverage for Essentia / YAMNet / MusiCNN.
func (s *Server) scannerStatus(catalogTracks int) []map[string]any {
	enabled := map[string]bool{}
	if s.MetaCfg != nil {
		for _, name := range s.MetaCfg.Extractors {
			enabled[name] = true
		}
	}
	counts, err := s.DB.ExtractorFeatureCounts(db.AcousticExtractors)
	if err != nil {
		counts = map[string]int{}
	}
	labels := map[string]string{
		"essentia": "Essentia",
		"yamnet":   "YAMNet",
		"musicnn":  "MusiCNN",
	}
	out := make([]map[string]any, 0, len(db.AcousticExtractors))
	for _, name := range db.AcousticExtractors {
		ready := counts[name]
		pct := 0
		if catalogTracks > 0 {
			pct = (100 * ready) / catalogTracks
			if pct > 100 {
				pct = 100
			}
		}
		out = append(out, map[string]any{
			"name":       name,
			"label":      labels[name],
			"enabled":    enabled[name],
			"ready":      ready,
			"total":      catalogTracks,
			"pct":        pct,
			"run_done":   0,
			"run_ok":     0,
			"run_errors": 0,
		})
	}
	return out
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if s.Cfg != nil {
		data["EnableStreamProbe"] = s.Cfg.EnableStreamProbe
		data["EnableAPIDocs"] = s.Cfg.EnableAPIDocs
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.Log.Printf("template %s: %v", name, err)
		http.Error(w, "template error", 500)
	}
}

func idFromPath(path, prefix string) (int64, error) {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.Trim(rest, "/")
	if rest == "" || strings.Contains(rest, "/") {
		return 0, fmt.Errorf("bad path")
	}
	return strconv.ParseInt(rest, 10, 64)
}

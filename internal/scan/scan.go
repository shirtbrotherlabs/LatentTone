// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package scan

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/tags"
)

// Scanner walks the music library and updates the catalog.
type Scanner struct {
	Cfg *config.Config
	DB  *db.DB
	Log *log.Logger
}

// Result summarizes a scan run.
type Result struct {
	Seen     int
	Upserted int
	Missing  int64
	Errors   int
}

// Full performs a full library reconcile.
func (s *Scanner) Full(trigger string) (*Result, error) {
	if s.Log == nil {
		s.Log = log.Default()
	}
	runID, err := s.DB.BeginScanRun(trigger)
	if err != nil {
		return nil, err
	}

	exts := s.Cfg.ExtSet()
	root := s.Cfg.LibraryRoot

	type job struct {
		abs, rel string
	}
	const writeBatchSize = 64
	jobs := make(chan job, 256)
	writes := make(chan db.TrackInput, writeBatchSize*4)
	var (
		seenMu   sync.Mutex
		seen     = map[string]struct{}{}
		upserted atomic.Int64
		errCount atomic.Int64
		wg       sync.WaitGroup
		writerWG sync.WaitGroup
	)

	// Single writer: drain metadata results into SQLite batches.
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		batch := make([]db.TrackInput, 0, writeBatchSize)
		flush := func() {
			if len(batch) == 0 {
				return
			}
			results, err := s.DB.UpsertTracks(batch)
			if err != nil {
				s.Log.Printf("upsert batch (%d): %v", len(batch), err)
				errCount.Add(int64(len(batch)))
				batch = batch[:0]
				return
			}
			for _, r := range results {
				if r.Err != nil {
					s.Log.Printf("upsert %s: %v", r.Path, r.Err)
					errCount.Add(1)
					continue
				}
				upserted.Add(1)
				seenMu.Lock()
				seen[r.Path] = struct{}{}
				seenMu.Unlock()
			}
			batch = batch[:0]
		}
		for in := range writes {
			batch = append(batch, in)
			if len(batch) >= writeBatchSize {
				flush()
			}
		}
		flush()
	}()

	workers := s.Cfg.Concurrency
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				in, err := tags.Extract(j.abs, j.rel)
				if err != nil {
					s.Log.Printf("tags %s: %v", j.rel, err)
					errCount.Add(1)
					// Continue with whatever Extract filled (path fallbacks).
					if in.Title == "" {
						continue
					}
				}
				if _, cacheErr := s.DB.ReuseScanMetadata(&in); cacheErr != nil {
					s.Log.Printf("scan metadata cache %s: %v", j.rel, cacheErr)
				}
				if in.DurationMS == nil || in.Year == nil {
					if probeErr := tags.EnrichMediaInfo(j.abs, s.Cfg.FFmpegPath, &in); probeErr != nil {
						s.Log.Printf("media info %s: %v", j.rel, probeErr)
					}
				}
				albumDir := filepath.Dir(j.abs)
				cover, err := FindCover(albumDir, root, s.Cfg.CoverNames)
				if err != nil {
					s.Log.Printf("cover %s: %v", j.rel, err)
				}
				if cover == "" {
					cover = EnsureEmbeddedCover(j.abs, albumDir, root, s.Cfg.CoverCacheDir)
				}
				in.CoverPath = cover
				writes <- in
			}
		}()
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.Log.Printf("walk: %v", err)
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".Trash" || name == "@eaDir" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if _, ok := exts[ext]; !ok {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel, s.Cfg.Exclude) {
			return nil
		}
		jobs <- job{abs: path, rel: rel}
		return nil
	})
	close(jobs)
	wg.Wait()
	close(writes)
	writerWG.Wait()

	if walkErr != nil {
		_ = s.DB.FinishScanRun(runID, len(seen), int(upserted.Load()), 0, "error", walkErr.Error())
		return nil, walkErr
	}

	missing, err := s.DB.MarkMissingExcept(seen)
	if err != nil {
		_ = s.DB.FinishScanRun(runID, len(seen), int(upserted.Load()), 0, "error", err.Error())
		return nil, err
	}

	status := "ok"
	if errCount.Load() > 0 {
		status = "ok_with_errors"
	}
	res := &Result{
		Seen:     len(seen),
		Upserted: int(upserted.Load()),
		Missing:  missing,
		Errors:   int(errCount.Load()),
	}
	if err := s.DB.FinishScanRun(runID, res.Seen, res.Upserted, int(res.Missing), status, ""); err != nil {
		return res, err
	}
	s.Log.Printf("scan complete: seen=%d upserted=%d missing=%d errors=%d", res.Seen, res.Upserted, res.Missing, res.Errors)
	return res, nil
}

// ScanPath updates a single relative or absolute audio path.
func (s *Scanner) ScanPath(absPath string) error {
	root := s.Cfg.LibraryRoot
	absPath = filepath.Clean(absPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path outside library root: %s", absPath)
	}
	rel = filepath.ToSlash(rel)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), ".")
	if _, ok := s.Cfg.ExtSet()[ext]; !ok {
		return nil
	}
	in, err := tags.Extract(absPath, rel)
	if err != nil && in.Title == "" {
		return err
	}
	if _, cacheErr := s.DB.ReuseScanMetadata(&in); cacheErr != nil {
		return cacheErr
	}
	if in.DurationMS == nil || in.Year == nil {
		// Keep watcher scans resilient: path/tag metadata is still useful if
		// ffprobe cannot decode a damaged or unsupported file.
		_ = tags.EnrichMediaInfo(absPath, s.Cfg.FFmpegPath, &in)
	}
	albumDir := filepath.Dir(absPath)
	cover, _ := FindCover(albumDir, root, s.Cfg.CoverNames)
	if cover == "" {
		cover = EnsureEmbeddedCover(absPath, albumDir, root, s.Cfg.CoverCacheDir)
	}
	in.CoverPath = cover
	_, err = s.DB.UpsertTrack(in)
	return err
}

// MarkPathMissing sets missing_at for a relative path if present.
func (s *Scanner) MarkPathMissing(relPath string) error {
	relPath = filepath.ToSlash(relPath)
	now := db.Now()
	_, err := s.DB.SQL.Exec(
		`UPDATE tracks SET missing_at = ?, updated_at = ? WHERE path = ? AND missing_at IS NULL`,
		now, now, relPath,
	)
	return err
}

func excluded(rel string, patterns []string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Simple substring / suffix heuristics for common exclude globs.
		p = strings.ReplaceAll(p, "**/", "")
		p = strings.Trim(p, "*")
		if p != "" && strings.Contains(rel, strings.Trim(p, "/")) {
			return true
		}
	}
	return false
}

// LibraryReadable checks the music root exists and is a directory.
func LibraryReadable(root string) error {
	fi, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	return nil
}

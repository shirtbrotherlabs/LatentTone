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
	jobs := make(chan job, 256)
	var (
		seenMu   sync.Mutex
		seen     = map[string]struct{}{}
		upserted atomic.Int64
		errCount atomic.Int64
		wg       sync.WaitGroup
	)

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
				cover, err := FindCover(filepath.Dir(j.abs), root, s.Cfg.CoverNames)
				if err != nil {
					s.Log.Printf("cover %s: %v", j.rel, err)
				}
				in.CoverPath = cover
				if _, err := s.DB.UpsertTrack(in); err != nil {
					s.Log.Printf("upsert %s: %v", j.rel, err)
					errCount.Add(1)
					continue
				}
				upserted.Add(1)
				seenMu.Lock()
				seen[in.Path] = struct{}{}
				seenMu.Unlock()
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
	cover, _ := FindCover(filepath.Dir(absPath), root, s.Cfg.CoverNames)
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
		p = strings.ReplaceAll(p, "**/" , "")
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

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package scan

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch runs a filesystem watcher; events are debounced into ScanPath / MarkPathMissing.
// Periodic full reconcile remains the source of truth if the watcher misses events.
func (s *Scanner) Watch(stop <-chan struct{}) error {
	if s.Log == nil {
		s.Log = log.Default()
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	root := s.Cfg.LibraryRoot
	if err := addWatchRecursive(w, root); err != nil {
		s.Log.Printf("watch: initial walk: %v", err)
	}

	debounce := make(map[string]time.Time)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			path := ev.Name
			if fi, err := os.Stat(path); err == nil && fi.IsDir() {
				if ev.Op&fsnotify.Create != 0 {
					_ = addWatchRecursive(w, path)
				}
				continue
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				rel, err := filepath.Rel(root, path)
				if err == nil && !strings.HasPrefix(rel, "..") {
					_ = s.MarkPathMissing(filepath.ToSlash(rel))
				}
				delete(debounce, path)
				continue
			}
			debounce[path] = time.Now()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			s.Log.Printf("watch error: %v", err)
		case <-ticker.C:
			now := time.Now()
			for path, t := range debounce {
				if now.Sub(t) < 2*time.Second {
					continue
				}
				delete(debounce, path)
				if err := s.ScanPath(path); err != nil {
					s.Log.Printf("watch scan %s: %v", path, err)
				}
			}
		}
	}
}

func addWatchRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".Trash" || name == "@eaDir" || name == ".git" {
			return filepath.SkipDir
		}
		_ = w.Add(path)
		return nil
	})
}

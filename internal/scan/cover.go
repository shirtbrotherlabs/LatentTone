// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package scan

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shirtbrotherlabs/LatentTone/internal/tags"
)

// coverExtractLocks serializes embedded-art extraction per album directory so
// concurrent scan workers don't race to write the same cache file.
var coverExtractLocks sync.Map // albumDir -> *sync.Mutex

// FindCover looks for a preferred cover filename in albumDir and returns a
// path relative to libraryRoot (slash-separated), or empty if none found.
func FindCover(albumDir, libraryRoot string, names []string) (string, error) {
	for _, name := range names {
		candidate := filepath.Join(albumDir, name)
		fi, err := os.Stat(candidate)
		if err != nil || fi.IsDir() {
			continue
		}
		rel, err := filepath.Rel(libraryRoot, candidate)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(rel, "..") {
			continue
		}
		return filepath.ToSlash(rel), nil
	}
	// Case-insensitive scan of directory for known basenames.
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		return "", nil
	}
	wanted := make(map[string]struct{}, len(names))
	for _, n := range names {
		wanted[strings.ToLower(n)] = struct{}{}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := wanted[strings.ToLower(e.Name())]; !ok {
			continue
		}
		rel, err := filepath.Rel(libraryRoot, filepath.Join(albumDir, e.Name()))
		if err != nil {
			continue
		}
		return filepath.ToSlash(rel), nil
	}
	return "", nil
}

// EnsureEmbeddedCover extracts embedded art from audioAbs when the album folder
// has no sidecar cover, writing it into cacheDir under a path mirroring the
// album's location in the library. It returns a library-root-relative cover
// path (matching sidecar semantics) or empty when no art is available.
//
// The library is mounted read-only, so extracted art lives in cacheDir; the
// cover HTTP handler falls back to cacheDir when the path is absent on disk.
func EnsureEmbeddedCover(audioAbs, albumDir, libraryRoot, cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	relDir, err := filepath.Rel(libraryRoot, albumDir)
	if err != nil || strings.HasPrefix(relDir, "..") {
		return ""
	}
	relDir = filepath.ToSlash(relDir)

	muAny, _ := coverExtractLocks.LoadOrStore(albumDir, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Reuse a previously extracted cover if present.
	for _, ext := range []string{".jpg", ".png", ".gif", ".webp", ".bmp"} {
		relCover := relDir + "/cover" + ext
		if _, err := os.Stat(filepath.Join(cacheDir, filepath.FromSlash(relCover))); err == nil {
			return relCover
		}
	}

	art, ok := tags.ExtractArt(audioAbs)
	if !ok || len(art.Data) == 0 {
		return ""
	}
	relCover := relDir + "/cover" + art.Ext()
	dest := filepath.Join(cacheDir, filepath.FromSlash(relCover))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return ""
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, art.Data, 0o644); err != nil {
		return ""
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return relCover
}

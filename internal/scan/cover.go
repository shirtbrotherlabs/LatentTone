// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package scan

import (
	"os"
	"path/filepath"
	"strings"
)

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

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db

import (
	"strings"
	"unicode"
)

// NormalizeTag folds catalog text for duplicate / search matching:
// lowercase, strip punctuation, collapse whitespace.
func NormalizeTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			// drop punctuation / symbols
		}
	}
	return strings.TrimSpace(b.String())
}

// DuplicateKey is the normalized identity for tag-based duplicate grouping.
func DuplicateKey(title, album, artist string) string {
	return NormalizeTag(title) + "\x1f" + NormalizeTag(album) + "\x1f" + NormalizeTag(artist)
}

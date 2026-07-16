// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package tags

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	mediatag "github.com/dhowden/tag"
	"github.com/go-flac/go-flac/v2"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/tcolgate/mp3"
)

// Extract reads structural metadata from an audio file and applies path fallbacks.
func Extract(absPath, relPath string) (db.TrackInput, error) {
	in := db.TrackInput{
		Path:   filepath.ToSlash(relPath),
		Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), "."),
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		return in, err
	}
	in.FileMtime = fi.ModTime().Unix()
	in.FileSize = fi.Size()

	var tagErr error
	switch in.Format {
	case "mp3":
		tagErr = readMP3(absPath, &in)
	case "flac":
		tagErr = readFLAC(absPath, &in)
	case "m4a", "aac", "ogg", "opus":
		tagErr = readCommonTags(absPath, &in)
	default:
		// Other formats: path fallbacks only in Phase 1 (tagged OSS readers TBD).
		tagErr = nil
	}

	applyPathFallbacks(&in, relPath)
	if tagErr != nil {
		// Prefer cataloguing via path fallbacks over skipping the file.
		if in.Title != "" {
			return in, nil
		}
		return in, fmt.Errorf("read tags: %w", tagErr)
	}
	return in, nil
}

func readMP3(path string, in *db.TrackInput) error {
	tagErr := readCommonTags(path, in)
	if ms, ok := readMP3Duration(path); ok {
		in.DurationMS = &ms
	}
	return tagErr
}

func readFLAC(path string, in *db.TrackInput) error {
	tagErr := readCommonTags(path, in)
	f, err := flac.ParseFile(path)
	if err != nil {
		return err
	}

	if si, err := f.GetStreamInfo(); err == nil && si != nil {
		if si.SampleRate > 0 && si.SampleCount > 0 {
			ms := int64(float64(si.SampleCount) / float64(si.SampleRate) * 1000)
			in.DurationMS = &ms
		}
		sr := si.SampleRate
		ch := si.ChannelCount
		in.SampleRateHz = &sr
		in.Channels = &ch
	}
	return tagErr
}

func readCommonTags(path string, in *db.TrackInput) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	meta, err := mediatag.ReadFrom(f)
	if err != nil {
		return err
	}
	in.Title = strings.TrimSpace(meta.Title())
	in.Album = strings.TrimSpace(meta.Album())
	if artist := strings.TrimSpace(meta.Artist()); artist != "" {
		in.Artists = splitArtists(artist)
	}
	in.AlbumArtist = strings.TrimSpace(meta.AlbumArtist())
	if genre := strings.TrimSpace(meta.Genre()); genre != "" {
		in.Genres = splitGenres(genre)
	}
	if year := meta.Year(); year > 0 {
		in.Year = intPtr(year)
		in.AlbumYear = intPtr(year)
	}
	if track, _ := meta.Track(); track > 0 {
		in.TrackNumber = intPtr(track)
	}
	if disc, _ := meta.Disc(); disc > 0 {
		in.DiscNumber = intPtr(disc)
	}
	in.Comment = strings.TrimSpace(meta.Comment())
	return nil
}

func readMP3Duration(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	decoder := mp3.NewDecoder(f)
	var (
		frame    mp3.Frame
		skipped  int
		duration int64
	)
	for {
		err := decoder.Decode(&frame, &skipped)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, false
		}
		duration += frame.Duration().Milliseconds()
	}
	return duration, duration > 0
}

func parseNumBeforeSlash(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "/-"); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n > 0
}

// PathFallbacks fills empty title/album/artist from path segments.
func PathFallbacks(relPath string) (artist, album, title string) {
	relPath = filepath.ToSlash(relPath)
	base := filepath.Base(relPath)
	title = strings.TrimSuffix(base, filepath.Ext(base))
	dir := filepath.Dir(relPath)
	if dir == "." || dir == "/" || dir == "" {
		return "", "", title
	}
	album = filepath.Base(dir)
	parent := filepath.Dir(dir)
	if parent != "." && parent != "/" && parent != "" {
		artist = filepath.Base(parent)
	}
	title = stripTrackPrefix(title)
	return artist, album, title
}

func applyPathFallbacks(in *db.TrackInput, relPath string) {
	artist, album, title := PathFallbacks(relPath)
	if in.Title == "" {
		in.Title = title
	}
	if in.Album == "" {
		in.Album = album
	}
	if in.AlbumArtist == "" && artist != "" {
		in.AlbumArtist = artist
	}
	if len(in.Artists) == 0 && artist != "" {
		in.Artists = []string{artist}
	}
}

func stripTrackPrefix(title string) string {
	s := strings.TrimSpace(title)
	for i := 0; i < len(s) && i < 4; i++ {
		if s[i] < '0' || s[i] > '9' {
			if i == 0 {
				return s
			}
			rest := strings.TrimSpace(s[i:])
			rest = strings.TrimLeft(rest, "-.) ")
			if rest != "" {
				return rest
			}
			return s
		}
	}
	return s
}

func splitArtists(s string) []string {
	for _, sep := range []string{" feat. ", " ft. ", " Feat. ", " / ", ";", ","} {
		if strings.Contains(s, sep) {
			parts := strings.Split(s, sep)
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []string{s}
}

func splitGenres(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == '/' || r == ','
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 && strings.TrimSpace(s) != "" {
		return []string{strings.TrimSpace(s)}
	}
	return out
}

func intPtr(n int) *int { return &n }

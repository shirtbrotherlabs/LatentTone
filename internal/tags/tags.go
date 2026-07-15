// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package tags

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacvorbis/v2"
	"github.com/go-flac/go-flac/v2"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
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
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()

	in.Title = strings.TrimSpace(tag.Title())
	in.Album = strings.TrimSpace(tag.Album())
	artist := strings.TrimSpace(tag.Artist())
	if artist != "" {
		in.Artists = splitArtists(artist)
	}
	// AlbumArtist via TPE2
	if t := tag.GetTextFrame(tag.CommonID("Band/Orchestra/Accompaniment")); t.Text != "" {
		in.AlbumArtist = strings.TrimSpace(t.Text)
	}
	if g := strings.TrimSpace(tag.Genre()); g != "" {
		in.Genres = splitGenres(g)
	}
	if y := strings.TrimSpace(tag.Year()); y != "" {
		if n, err := strconv.Atoi(y); err == nil && n > 0 {
			in.Year = intPtr(n)
			in.AlbumYear = intPtr(n)
		}
	}
	if tn := strings.TrimSpace(tag.GetTextFrame(tag.CommonID("Track number/Position in set")).Text); tn != "" {
		if n, ok := parseNumBeforeSlash(tn); ok {
			in.TrackNumber = intPtr(n)
		}
	}
	if dn := strings.TrimSpace(tag.GetTextFrame(tag.CommonID("Part of a set")).Text); dn != "" {
		if n, ok := parseNumBeforeSlash(dn); ok {
			in.DiscNumber = intPtr(n)
		}
	}
	in.Comment = firstComment(tag)
	return nil
}

func readFLAC(path string, in *db.TrackInput) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return err
	}
	for _, b := range f.Meta {
		if b.Type != flac.VorbisComment {
			continue
		}
		c, err := flacvorbis.ParseFromMetaDataBlock(*b)
		if err != nil {
			return err
		}
		if v, _ := c.Get(flacvorbis.FIELD_TITLE); len(v) > 0 {
			in.Title = strings.TrimSpace(v[0])
		}
		if v, _ := c.Get(flacvorbis.FIELD_ALBUM); len(v) > 0 {
			in.Album = strings.TrimSpace(v[0])
		}
		if v, _ := c.Get(flacvorbis.FIELD_ARTIST); len(v) > 0 {
			in.Artists = splitArtists(strings.TrimSpace(v[0]))
		}
		if v, _ := c.Get("ALBUMARTIST"); len(v) > 0 {
			in.AlbumArtist = strings.TrimSpace(v[0])
		}
		if v, _ := c.Get(flacvorbis.FIELD_GENRE); len(v) > 0 {
			in.Genres = splitGenres(strings.TrimSpace(v[0]))
		}
		if v, _ := c.Get(flacvorbis.FIELD_TRACKNUMBER); len(v) > 0 {
			if n, ok := parseNumBeforeSlash(v[0]); ok {
				in.TrackNumber = intPtr(n)
			}
		}
		if v, _ := c.Get("DISCNUMBER"); len(v) > 0 {
			if n, ok := parseNumBeforeSlash(v[0]); ok {
				in.DiscNumber = intPtr(n)
			}
		}
		if v, _ := c.Get(flacvorbis.FIELD_DATE); len(v) > 0 {
			y := strings.TrimSpace(v[0])
			if len(y) >= 4 {
				if n, err := strconv.Atoi(y[:4]); err == nil && n > 0 {
					in.Year = intPtr(n)
					in.AlbumYear = intPtr(n)
				}
			}
		}
		break
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
	return nil
}

func firstComment(tag *id3v2.Tag) string {
	frames := tag.GetFrames(tag.CommonID("Comments"))
	for _, f := range frames {
		if cf, ok := f.(id3v2.CommentFrame); ok {
			return strings.TrimSpace(cf.Text)
		}
	}
	return ""
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

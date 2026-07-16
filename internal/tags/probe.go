// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

var fourDigitYear = regexp.MustCompile(`(?:^|[^0-9])([12][0-9]{3})(?:[^0-9]|$)`)

type probeResult struct {
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// EnrichMediaInfo fills missing duration and year values using ffprobe.
// Native tag readers remain preferred; ffprobe is only called when a field is
// absent, which adds support for MP3, M4A, OGG and other configured formats.
func EnrichMediaInfo(path, ffmpegPath string, in *db.TrackInput) error {
	if in == nil || (in.DurationMS != nil && in.Year != nil) {
		return nil
	}
	// MP3 metadata and duration are handled natively. If its year is still
	// absent, ffprobe sees the same ID3 data and would add a costly full-file
	// pass without recovering additional information.
	if strings.EqualFold(filepath.Ext(path), ".mp3") && in.DurationMS != nil {
		return nil
	}
	probePath := ffprobePath(ffmpegPath)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, probePath,
		"-v", "error",
		"-show_entries", "format=duration:format_tags=date,year,release_date,TDRC,TYER",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("ffprobe timeout: %w", ctx.Err())
		}
		return fmt.Errorf("ffprobe: %w", err)
	}
	return applyProbeJSON(out, in)
}

func applyProbeJSON(data []byte, in *db.TrackInput) error {
	var result probeResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parse ffprobe json: %w", err)
	}
	if in.DurationMS == nil {
		if seconds, err := strconv.ParseFloat(strings.TrimSpace(result.Format.Duration), 64); err == nil && seconds > 0 {
			ms := int64(seconds*1000 + 0.5)
			in.DurationMS = &ms
		}
	}
	if in.Year == nil {
		for _, key := range []string{"date", "year", "release_date", "tdrc", "tyer"} {
			if raw := probeTag(result.Format.Tags, key); raw != "" {
				if year, ok := parseYear(raw); ok {
					in.Year = intPtr(year)
					in.AlbumYear = intPtr(year)
					break
				}
			}
		}
	}
	return nil
}

func probeTag(tags map[string]string, wanted string) string {
	for key, value := range tags {
		if strings.EqualFold(key, wanted) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseYear(raw string) (int, bool) {
	match := fourDigitYear.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) != 2 {
		return 0, false
	}
	year, err := strconv.Atoi(match[1])
	return year, err == nil && year >= 1000 && year <= 2999
}

func ffprobePath(ffmpegPath string) string {
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath == "" || ffmpegPath == "ffmpeg" {
		return "ffprobe"
	}
	base := filepath.Base(ffmpegPath)
	if base == "ffmpeg" {
		return filepath.Join(filepath.Dir(ffmpegPath), "ffprobe")
	}
	return "ffprobe"
}

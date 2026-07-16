// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package stream

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

// EncodeOpts controls progressive/HLS audio encode targets.
type EncodeOpts struct {
	// Format is original | mp3 | aac | opus (empty treated as original).
	Format string
	// BitrateKbps target for lossy encodes (default 192).
	BitrateKbps int
}

// EffectiveStream describes what progressive delivery will serve for a track.
type EffectiveStream struct {
	// Codec is a short label: flac, mp3, aac, opus, wav, ogg, …
	Codec string
	// BitrateKbps is the encode target or catalog bitrate; 0 when unknown.
	BitrateKbps int
	// Transcoding is true when FFmpeg re-encodes instead of serving the file.
	Transcoding bool
}

// NeedsTranscode reports whether progressive delivery should run FFmpeg
// instead of serving the original file bytes.
func NeedsTranscode(relPath, catalogFormat string, opts EncodeOpts) bool {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "mp3" || format == "aac" || format == "opus" {
		return true
	}
	return !browserSafeContainer(relPath, catalogFormat)
}

func bitrateOrDefault(kbps int) int {
	if kbps <= 0 {
		return 192
	}
	return kbps
}

// DisplayCodec normalizes a path extension / catalog format hint to a short codec label.
func DisplayCodec(relPath, catalogFormat string) string {
	ext := strings.ToLower(filepath.Ext(relPath))
	fmtHint := strings.ToLower(strings.TrimSpace(catalogFormat))
	switch ext {
	case ".mp3":
		return "mp3"
	case ".flac":
		return "flac"
	case ".opus":
		return "opus"
	case ".ogg":
		if fmtHint == "opus" {
			return "opus"
		}
		return "ogg"
	case ".m4a", ".aac", ".mp4":
		return "aac"
	case ".wav", ".wave":
		return "wav"
	case ".webm":
		return "webm"
	}
	switch fmtHint {
	case "mp3", "mpeg":
		return "mp3"
	case "flac":
		return "flac"
	case "opus":
		return "opus"
	case "ogg", "vorbis":
		return "ogg"
	case "m4a", "aac", "mp4":
		return "aac"
	case "wav", "wave":
		return "wav"
	case "webm":
		return "webm"
	}
	if fmtHint != "" {
		return fmtHint
	}
	if ext != "" {
		return strings.TrimPrefix(ext, ".")
	}
	return "audio"
}

// ResolveEffectiveStream reports the codec/bitrate the progressive URL will deliver.
func ResolveEffectiveStream(relPath, catalogFormat string, catalogBitrateKbps int, opts EncodeOpts) EffectiveStream {
	if NeedsTranscode(relPath, catalogFormat, opts) {
		br := bitrateOrDefault(opts.BitrateKbps)
		switch strings.ToLower(strings.TrimSpace(opts.Format)) {
		case "aac":
			return EffectiveStream{Codec: "aac", BitrateKbps: br, Transcoding: true}
		case "opus":
			return EffectiveStream{Codec: "opus", BitrateKbps: br, Transcoding: true}
		case "mp3":
			return EffectiveStream{Codec: "mp3", BitrateKbps: br, Transcoding: true}
		default:
			// Unsafe original → auto MP3 fallback.
			return EffectiveStream{Codec: "mp3", BitrateKbps: br, Transcoding: true}
		}
	}
	return EffectiveStream{
		Codec:       DisplayCodec(relPath, catalogFormat),
		BitrateKbps: catalogBitrateKbps,
		Transcoding: false,
	}
}

// browserSafeContainer is true for formats most Chromium/Firefox/Safari builds
// can decode via progressive <audio>. WMA and other niche containers are not.
func browserSafeContainer(relPath, catalogFormat string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	fmtHint := strings.ToLower(strings.TrimSpace(catalogFormat))
	switch ext {
	case ".mp3", ".m4a", ".aac", ".mp4", ".wav", ".wave", ".flac", ".ogg", ".opus", ".webm":
		return true
	case ".wma", ".asf", ".ape", ".wv", ".mpc", ".aiff", ".aif", ".dsf", ".dff", ".mka", ".ac3", ".dts":
		return false
	}
	switch fmtHint {
	case "mp3", "mpeg", "m4a", "aac", "mp4", "wav", "flac", "ogg", "opus", "webm":
		return true
	case "wma", "asf", "ape", "wavpack", "aiff":
		return false
	}
	// Unknown extension: prefer transcode so progressive playback does not fail silently.
	return false
}

// ResolveEncodeTarget picks the FFmpeg output codec/container for progressive
// streaming when NeedsTranscode is true.
func ResolveEncodeTarget(opts EncodeOpts) (codec, format, contentType, bitrate string) {
	bitrate = fmt.Sprintf("%dk", bitrateOrDefault(opts.BitrateKbps))
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "aac":
		return "aac", "adts", "audio/aac", bitrate
	case "opus":
		// Ogg/Opus is widely supported by Chromium/Firefox progressive <audio>.
		return "libopus", "ogg", "audio/ogg", bitrate
	default:
		// mp3 (explicit) or auto-fallback for unsafe originals
		return "libmp3lame", "mp3", "audio/mpeg", bitrate
	}
}

// HLSAudioArgs returns FFmpeg audio encode args for session HLS packaging.
func HLSAudioArgs(opts EncodeOpts) []string {
	bitrate := fmt.Sprintf("%dk", bitrateOrDefault(opts.BitrateKbps))
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "mp3":
		// HLS with MP3 audio in MPEG-TS is widely supported.
		return []string{"-c:a", "libmp3lame", "-b:a", bitrate}
	case "opus":
		// Opus-in-MPEG-TS is poorly supported by hls.js/Safari; keep AAC for HLS
		// fallback while progressive serves Opus.
		return []string{"-c:a", "aac", "-b:a", bitrate}
	default:
		// original preference and aac both use AAC in HLS (browser-safe).
		return []string{"-c:a", "aac", "-b:a", bitrate}
	}
}

// ServeProgressiveTranscode pipes FFmpeg stdout to the HTTP response.
func (m *Manager) ServeProgressiveTranscode(w http.ResponseWriter, absPath string, opts EncodeOpts) error {
	if err := assertUnderRoot(m.LibraryRoot, absPath); err != nil {
		return err
	}
	codec, format, ctype, bitrate := ResolveEncodeTarget(opts)
	cmd := exec.Command(m.FFmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-i", absPath,
		"-vn",
		"-c:a", codec, "-b:a", bitrate,
		"-f", format,
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, stdout)
	errBuf, _ := io.ReadAll(io.LimitReader(stderr, 4<<10))
	waitErr := cmd.Wait()
	if copyErr != nil {
		return copyErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(string(errBuf))
		if msg == "" {
			msg = waitErr.Error()
		}
		if m.Log != nil {
			m.Log.Printf("progressive transcode %s: %s", filepath.Base(absPath), msg)
		}
		return fmt.Errorf("transcode failed: %s", msg)
	}
	return nil
}

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15
// Last-Modified: 2026-07-20

package stream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
// Always includes -vn so embedded cover art (common in MP3/FLAC) is not
// treated as a video stream — libx264 then fails on odd cover dimensions.
func HLSAudioArgs(opts EncodeOpts) []string {
	bitrate := fmt.Sprintf("%dk", bitrateOrDefault(opts.BitrateKbps))
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "mp3":
		// HLS with MP3 audio in MPEG-TS is widely supported.
		return []string{"-vn", "-c:a", "libmp3lame", "-b:a", bitrate}
	case "opus":
		// Opus-in-MPEG-TS is poorly supported by hls.js/Safari; keep AAC for HLS
		// fallback while progressive serves Opus.
		return []string{"-vn", "-c:a", "aac", "-b:a", bitrate}
	default:
		// original preference and aac both use AAC in HLS (browser-safe).
		return []string{"-vn", "-c:a", "aac", "-b:a", bitrate}
	}
}

// IsTranscodeRangeProbe reports whether Range is a closed warm-up probe
// (e.g. bytes=0-2047) rather than a media-player open-ended request (bytes=0-).
// Closed probes must not start FFmpeg; open-ended ranges must stream the full
// encode because Chrome's <audio> always sends Range: bytes=0- on load.
func IsTranscodeRangeProbe(rangeHeader string) bool {
	h := strings.TrimSpace(rangeHeader)
	if h == "" {
		return false
	}
	lower := strings.ToLower(h)
	if !strings.HasPrefix(lower, "bytes=") {
		return true
	}
	spec := strings.TrimSpace(h[len("bytes="):])
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = strings.TrimSpace(spec[:i])
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return true
	}
	end := strings.TrimSpace(parts[1])
	// bytes=0- / bytes=123- → open-ended media fetch
	if end == "" {
		return false
	}
	// bytes=0-2047 → closed probe
	return true
}

// ProgressiveFFmpegArgs builds FFmpeg argv for a low-latency progressive encode.
// Tuned for fast time-to-first-byte: small probe, single audio map, one thread,
// flush packets immediately (skip responsiveness under load).
func ProgressiveFFmpegArgs(absPath string, opts EncodeOpts) (args []string, contentType string) {
	codec, format, ctype, bitrate := ResolveEncodeTarget(opts)
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-analyzeduration", "500000",
		"-probesize", "32768",
		"-i", absPath,
		"-map", "0:a:0",
		"-vn",
		"-c:a", codec, "-b:a", bitrate,
		"-threads", "1",
		"-flush_packets", "1",
		"-f", format,
		"pipe:1",
	}, ctype
}

// ServeProgressiveTranscode pipes FFmpeg stdout to the HTTP response.
// ctx should be the request context — on cancel (skip / navigation) FFmpeg is
// killed so orphaned encodes cannot pile up and starve the server.
func (m *Manager) ServeProgressiveTranscode(ctx context.Context, w http.ResponseWriter, absPath string, opts EncodeOpts) error {
	if err := assertUnderRoot(m.LibraryRoot, absPath); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.progSem != nil {
		select {
		case m.progSem <- struct{}{}:
			defer func() { <-m.progSem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ffArgs, ctype := ProgressiveFFmpegArgs(absPath, opts)
	cmd := exec.Command(m.FFmpegPath, ffArgs...)
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

	var killOnce sync.Once
	kill := func() {
		killOnce.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			kill()
		case <-done:
		}
	}()

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)

	// Drain stderr concurrently so a full pipe cannot stall FFmpeg while we copy stdout.
	errCh := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(io.LimitReader(stderr, 4<<10))
		errCh <- buf
	}()

	_, copyErr := io.Copy(w, stdout)
	if copyErr != nil || ctx.Err() != nil {
		kill()
	}
	waitErr := cmd.Wait()
	errBuf := <-errCh
	if ctx.Err() != nil {
		return ctx.Err()
	}
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

// FileExtensionForEncode returns the download filename extension for opts
// (mp3/aac/opus) or the original path extension when format is original.
func FileExtensionForEncode(relPath string, opts EncodeOpts) string {
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "mp3":
		return "mp3"
	case "aac":
		return "aac"
	case "opus":
		return "opus"
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(relPath)), ".")
	if ext == "" {
		return "bin"
	}
	return ext
}

// DownloadNeedsTranscode is true when the download must run FFmpeg.
// Unlike progressive playback, "original" never transcodes for download.
func DownloadNeedsTranscode(opts EncodeOpts) bool {
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "mp3", "aac", "opus":
		return true
	default:
		return false
	}
}

// ServeDownloadTranscode encodes the full track to the response as an attachment.
// Extra concurrent callers wait on dlSem (does not refuse with 503).
func (m *Manager) ServeDownloadTranscode(ctx context.Context, w http.ResponseWriter, absPath string, opts EncodeOpts, filename string) error {
	if err := assertUnderRoot(m.LibraryRoot, absPath); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.dlSem != nil {
		select {
		case m.dlSem <- struct{}{}:
			defer func() { <-m.dlSem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ffArgs, ctype := ProgressiveFFmpegArgs(absPath, opts)
	cmd := exec.Command(m.FFmpegPath, ffArgs...)
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

	var killOnce sync.Once
	kill := func() {
		killOnce.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			kill()
		case <-done:
		}
	}()

	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", contentDispositionAttachment(filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)

	errCh := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(io.LimitReader(stderr, 4<<10))
		errCh <- buf
	}()

	_, copyErr := io.Copy(w, stdout)
	if copyErr != nil || ctx.Err() != nil {
		kill()
	}
	waitErr := cmd.Wait()
	errBuf := <-errCh
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if copyErr != nil {
		return copyErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(string(errBuf))
		if msg == "" {
			msg = waitErr.Error()
		}
		if m.Log != nil {
			m.Log.Printf("download transcode %s: %s", filepath.Base(absPath), msg)
		}
		return fmt.Errorf("transcode failed: %s", msg)
	}
	return nil
}

// contentDispositionAttachment builds a Content-Disposition header value.
func contentDispositionAttachment(filename string) string {
	safe := strings.ReplaceAll(filename, `"`, `'`)
	return fmt.Sprintf(`attachment; filename="%s"`, safe)
}


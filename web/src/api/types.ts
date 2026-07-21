/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

export type User = {
  id: number;
  username: string;
  is_admin?: boolean;
};

/** Runtime deployment hints from GET /api/v1/config (no auth). */
export type PublicConfig = {
  public_base_url: string;
};

export type TrackRef = {
  track_id: number;
  score?: number;
  source?: string;
  /** Latest like/dislike for the session user when known. */
  feedback?: "like" | "dislike" | string;
  play_count?: number;
};

export type SessionStatus = {
  id: string;
  user_id: number;
  status: string;
  seed_track_id: number;
  now_playing?: TrackRef | null;
  /** Recently played/skipped (oldest → newest), capped server-side. */
  history?: TrackRef[];
  queue: TrackRef[];
  last_feedback?: { signal: string; track_id: number; at: string } | null;
  hls_url: string;
  progressive_url: string;
  can_go_back?: boolean;
  /** Progressive delivery codec for now-playing (flac, mp3, aac, opus, …). */
  stream_track_id?: number;
  stream_codec?: string;
  /** Progressive bitrate in kbps; omitted/0 when unknown. */
  stream_bitrate_kbps?: number;
  /** True when progressive delivery is an FFmpeg encode instead of original bytes. */
  stream_transcoding?: boolean;
};

export type CatalogTrack = {
  id: number;
  title: string;
  artist: string;
  album: string;
  album_id?: number;
  artist_id?: number;
  track_number?: number | null;
  disc_number?: number | null;
  duration_ms?: number | null;
  bitrate_kbps?: number | null;
  format?: string | null;
  year?: number | null;
  genres?: string;
  cover_url?: string;
  /** Per-user like/dislike when API-enriched. */
  feedback?: "like" | "dislike" | string;
  /** Global play count when API-enriched. */
  play_count?: number;
};

export type CatalogAlbum = {
  id: number;
  title: string;
  artist: string;
  artist_id?: number;
  year?: number | null;
  cover_url?: string;
};

export type CatalogArtist = {
  id: number;
  name: string;
  cover_url?: string;
};

export type CatalogGenre = {
  id: number;
  name: string;
  count: number;
};

export type SearchSuggestion = {
  kind: "track" | "artist" | "album" | string;
  id: number;
  label: string;
  sublabel?: string;
  cover_url?: string;
  track_id?: number;
  duration_ms?: number;
};

export type DuplicateGroup = {
  title: string;
  album: string;
  artist: string;
  duration_ms: number;
  count: number;
  tracks: (CatalogTrack & { path?: string })[];
};

export type CreateSessionSeed = {
  seed_track_id?: number;
  seed_artist_id?: number;
  seed_genre_id?: number;
  seed_genre?: string;
  seed_playlist_id?: number;
};

export type PlaylistHeader = {
  id: number;
  name: string;
  kind: string;
  length: number;
  created_at?: string;
  updated_at?: string;
  seed_track_id?: number;
  user_id?: number;
  cover_url?: string;
};

export type PlaylistTrack = {
  position: number;
  track_id: number;
  /** Alias for track_id when present in playlist detail payloads. */
  id?: number;
  score?: number;
  title?: string;
  artist?: string;
  album?: string;
  duration_ms?: number | null;
  year?: number | null;
  album_id?: number;
  artist_id?: number;
  cover_url?: string;
  feedback?: "like" | "dislike" | string;
  play_count?: number;
};

export type Playlist = PlaylistHeader & {
  tracks?: PlaylistTrack[];
};

export type CatalogSummary = {
  artists: number;
  albums: number;
  tracks: number;
};

export type ScanStatus = {
  running: boolean;
  last: string;
  artists: number;
  albums: number;
  tracks: number;
  stoppable: boolean;
};

export type ScanSchedule = {
  enabled: boolean;
  interval_seconds: number;
  updated_at?: string;
  next_run_at?: string;
  source?: string;
};

export type EmbedScannerRow = {
  name: string;
  label?: string;
  enabled?: boolean;
  ready?: number;
  total?: number;
  pct?: number;
  run_done?: number;
  run_ok?: number;
  run_errors?: number;
};

export type EmbedStatus = {
  running: boolean;
  claimed: number;
  done: number;
  ok: number;
  errors: number;
  last: string;
  ready: number;
  pending: number;
  processing: number;
  error: number;
  stale: number;
  catalog_tracks: number;
  scanners: EmbedScannerRow[];
  extractors?: { name: string; enabled?: boolean; done?: number; ok?: number; errors?: number }[];
};

export type QueueTrack = CatalogTrack & {
  source?: string;
  feedback?: "like" | "dislike" | string;
  play_count?: number;
};

/** Per-user Radio diversification preferences (defaults all ON). */
export type RadioPrefs = {
  user_id: number;
  radio_bridge: boolean;
  artist_cooldown: boolean;
  query_jitter: boolean;
  artist_penalty: boolean;
  bounded_random: boolean;
  jitter_alpha: number;
  updated_at?: string;
};

export type RadioPrefsPatch = Partial<
  Pick<
    RadioPrefs,
    | "radio_bridge"
    | "artist_cooldown"
    | "query_jitter"
    | "artist_penalty"
    | "bounded_random"
    | "jitter_alpha"
  >
>;

/** Per-user progressive/HLS encode defaults. */
export type StreamPrefs = {
  user_id: number;
  stream_format: "original" | "mp3" | "aac" | "opus" | string;
  bitrate_kbps: number;
  updated_at?: string;
};

export type StreamPrefsPatch = Partial<Pick<StreamPrefs, "stream_format" | "bitrate_kbps">>;

/** Recent continuous-play radio station (listening_sessions row + track meta). */
export type Station = {
  id: string;
  status: string;
  seed_track_id?: number;
  now_playing_id?: number;
  started_at: string;
  updated_at: string;
  stopped_at?: string;
  seed_track?: CatalogTrack;
  now_playing?: CatalogTrack;
};

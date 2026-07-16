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
  stream_format: "original" | "mp3" | "aac" | string;
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

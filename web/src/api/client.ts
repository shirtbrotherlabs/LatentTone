/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

import type {
  CatalogAlbum,
  CatalogArtist,
  CatalogGenre,
  CatalogSummary,
  CatalogTrack,
  CreateSessionSeed,
  DuplicateGroup,
  EmbedStatus,
  Playlist,
  PlaylistHeader,
  PublicConfig,
  RadioPrefs,
  RadioPrefsPatch,
  ScanSchedule,
  ScanStatus,
  SearchSuggestion,
  SessionStatus,
  Station,
  StreamPrefs,
  StreamPrefsPatch,
  User,
  ListeningSessionsResponse,
} from "./types";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(path, {
    ...init,
    headers,
    // Session is HttpOnly cookie `lt_session` — never store tokens in JS.
    // Always send cookies (same-origin and cross-path) with the request.
    credentials: "include",
  });
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { error: text };
    }
  }
  if (!res.ok) {
    const msg =
      data && typeof data === "object" && data !== null && "error" in data
        ? String((data as { error: unknown }).error)
        : res.statusText;
    throw new ApiError(res.status, msg || "request failed");
  }
  return data as T;
}

export const api = {
  /** Non-secret deployment config (public_base_url). No auth. */
  getPublicConfig() {
    return request<PublicConfig>("/api/v1/config");
  },
  register(username: string, password: string) {
    return request<{ user: User; token?: string }>("/api/v1/auth/register", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
  },
  login(username: string, password: string) {
    return request<{ user: User; token?: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
  },
  logout() {
    return request<{ ok: string }>("/api/v1/auth/logout", { method: "POST" });
  },
  me() {
    return request<User>("/api/v1/auth/me");
  },
  changePassword(currentPassword: string, newPassword: string) {
    return request<{ ok: boolean }>("/api/v1/auth/password", {
      method: "POST",
      body: JSON.stringify({
        current_password: currentPassword,
        new_password: newPassword,
      }),
    });
  },
  getRadioPrefs() {
    return request<RadioPrefs>("/api/v1/me/radio-prefs");
  },
  patchRadioPrefs(patch: RadioPrefsPatch) {
    return request<RadioPrefs>("/api/v1/me/radio-prefs", {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  },
  getStreamPrefs() {
    return request<StreamPrefs>("/api/v1/me/stream-prefs");
  },
  patchStreamPrefs(patch: StreamPrefsPatch) {
    return request<StreamPrefs>("/api/v1/me/stream-prefs", {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  },
  listStations(limit = 12) {
    return request<{ stations: Station[] }>(`/api/v1/me/stations?limit=${limit}`);
  },
  listMyListeningSessions(limit = 100) {
    return request<ListeningSessionsResponse>(
      `/api/v1/me/listening-sessions?limit=${limit}`,
    );
  },
  listAdminListeningSessions(limit = 200) {
    return request<ListeningSessionsResponse>(
      `/api/v1/admin/listening-sessions?limit=${limit}`,
    );
  },
  createSession(seed: number | CreateSessionSeed) {
    const body =
      typeof seed === "number"
        ? { seed_track_id: seed }
        : {
            seed_track_id: seed.seed_track_id,
            seed_artist_id: seed.seed_artist_id,
            seed_genre_id: seed.seed_genre_id,
            seed_genre: seed.seed_genre,
            seed_playlist_id: seed.seed_playlist_id,
            seed_album_id: seed.seed_album_id,
            mode: seed.mode,
          };
    return request<SessionStatus>("/api/v1/sessions", {
      method: "POST",
      body: JSON.stringify(body),
    });
  },
  getSession(id: string) {
    return request<SessionStatus>(`/api/v1/sessions/${id}`);
  },
  stopSession(id: string) {
    return request<SessionStatus>(`/api/v1/sessions/${id}`, { method: "DELETE" });
  },
  feedback(id: string, signal: string, trackId?: number) {
    return request<SessionStatus>(`/api/v1/sessions/${id}/feedback`, {
      method: "POST",
      body: JSON.stringify({ signal, track_id: trackId || 0 }),
    });
  },
  injectQueue(id: string, trackId: number, position: "next" | "end" = "next") {
    return request<SessionStatus>(`/api/v1/sessions/${id}/queue`, {
      method: "POST",
      body: JSON.stringify({ track_id: trackId, position }),
    });
  },
  removeFromQueue(id: string, trackId: number) {
    return request<SessionStatus>(`/api/v1/sessions/${id}/queue`, {
      method: "DELETE",
      body: JSON.stringify({ track_id: trackId }),
    });
  },
  /** Restore previous track from session history. 409 = no history (client restarts from 0). */
  sessionBack(id: string) {
    return request<SessionStatus>(`/api/v1/sessions/${id}/back`, {
      method: "POST",
      body: "{}",
    });
  },
  catalogSummary() {
    return request<CatalogSummary>("/api/v1/catalog");
  },
  scanStatus() {
    return request<ScanStatus>("/api/scan/status");
  },
  scanStart(force = false) {
    const q = force ? "?force=1" : "";
    return request<{ ok: boolean; running: boolean; stoppable: boolean }>(`/api/scan/start${q}`, {
      method: "POST",
    });
  },
  getScanSchedule() {
    return request<ScanSchedule>("/api/scan/schedule");
  },
  patchScanSchedule(body: { enabled?: boolean; interval_seconds?: number }) {
    return request<ScanSchedule>("/api/scan/schedule", {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  },
  embedStatus() {
    return request<EmbedStatus>("/api/embed/status");
  },
  embedStart() {
    return request<{ ok: boolean; running: boolean }>("/api/embed/start", { method: "POST" });
  },
  embedStop() {
    return request<{ ok: boolean; running: boolean }>("/api/embed/stop", { method: "POST" });
  },
  listArtists() {
    return request<{ artists: CatalogArtist[] }>("/api/v1/catalog/artists");
  },
  getArtist(id: number) {
    return request<{ id: number; name: string; albums: CatalogAlbum[] }>(
      `/api/v1/catalog/artists/${id}`,
    );
  },
  listAlbums(limit = 500) {
    return request<{ albums: CatalogAlbum[] }>(`/api/v1/catalog/albums?limit=${limit}`);
  },
  getAlbum(id: number) {
    return request<{ album: CatalogAlbum; tracks: CatalogTrack[] }>(
      `/api/v1/catalog/albums/${id}`,
    );
  },
  listTracks(opts: { limit?: number; q?: string; year?: number } = {}) {
    const params = new URLSearchParams();
    if (opts.limit) params.set("limit", String(opts.limit));
    if (opts.q) params.set("q", opts.q);
    if (opts.year) params.set("year", String(opts.year));
    const qs = params.toString();
    return request<{ tracks: CatalogTrack[] }>(
      `/api/v1/catalog/tracks${qs ? `?${qs}` : ""}`,
    );
  },
  searchSuggest(q: string, limit = 12, signal?: AbortSignal) {
    const params = new URLSearchParams({ q, limit: String(limit) });
    return request<{ suggestions: SearchSuggestion[]; q: string }>(
      `/api/v1/catalog/search/suggest?${params}`,
      signal ? { signal } : {},
    );
  },
  listGenres(limit = 200) {
    return request<{ genres: CatalogGenre[] }>(`/api/v1/catalog/genres?limit=${limit}`);
  },
  listDuplicates(limit = 200) {
    return request<{ groups: DuplicateGroup[]; rule: string }>(
      `/api/v1/catalog/duplicates?limit=${limit}`,
    );
  },
  /** Random Now Playing seeds; prefers tracks with playback history. */
  listSeedSuggestions(limit = 12) {
    const params = new URLSearchParams({
      suggest: "seeds",
      limit: String(limit),
    });
    return request<{ tracks: CatalogTrack[] }>(
      `/api/v1/catalog/tracks?${params}`,
    );
  },
  getTrack(id: number) {
    return request<CatalogTrack>(`/api/v1/catalog/tracks/${id}`);
  },
  listYears() {
    return request<{ years: { year: number; count: number }[] }>("/api/v1/catalog/years");
  },
  listTracksByYear(year: number, limit = 500) {
    return request<{ year: number; tracks: CatalogTrack[] }>(
      `/api/v1/catalog/years/${year}?limit=${limit}`,
    );
  },
  listPlaylists() {
    return request<{ playlists: PlaylistHeader[] }>("/api/v1/me/playlists");
  },
  createPlaylist(name: string) {
    return request<Playlist>("/api/v1/me/playlists", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },
  getPlaylist(id: number) {
    return request<Playlist>(`/api/v1/me/playlists/${id}`);
  },
  renamePlaylist(id: number, name: string) {
    return request<Playlist>(`/api/v1/me/playlists/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
  },
  deletePlaylist(id: number) {
    return request<{ ok: boolean }>(`/api/v1/me/playlists/${id}`, { method: "DELETE" });
  },
  addPlaylistTracks(id: number, trackIds: number[]) {
    return request<Playlist>(`/api/v1/me/playlists/${id}/tracks`, {
      method: "POST",
      body: JSON.stringify({ track_ids: trackIds }),
    });
  },
  removePlaylistTrack(id: number, trackId: number) {
    return request<Playlist>(`/api/v1/me/playlists/${id}/tracks/${trackId}`, {
      method: "DELETE",
    });
  },
  reorderPlaylist(id: number, trackIds: number[]) {
    return request<Playlist>(`/api/v1/me/playlists/${id}/tracks/order`, {
      method: "PUT",
      body: JSON.stringify({ track_ids: trackIds }),
    });
  },
};

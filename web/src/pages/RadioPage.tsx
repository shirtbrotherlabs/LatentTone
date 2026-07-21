/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-17
 * Last-Modified: 2026-07-20
 */

import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogArtist, CatalogGenre, CatalogTrack, Station } from "../api/types";
import { usePlayer } from "../player/PlayerContext";

function stationCover(st: Station): string | undefined {
  return st.now_playing?.cover_url ?? st.seed_track?.cover_url;
}

function stationTitle(st: Station): string {
  return (
    st.now_playing?.title ||
    st.seed_track?.title ||
    (st.now_playing_id ? `Track #${st.now_playing_id}` : null) ||
    (st.seed_track_id ? `Track #${st.seed_track_id}` : null) ||
    "Station"
  );
}

function stationSubtitle(st: Station): string {
  const artist = st.now_playing?.artist || st.seed_track?.artist || "";
  const seed = st.seed_track?.title;
  const live = st.status === "playing" || st.status === "created";
  if (live) return artist ? `${artist} · live` : "Live station";
  if (seed && st.now_playing?.title && seed !== st.now_playing.title) {
    return artist ? `${artist} · seeded from ${seed}` : `Seeded from ${seed}`;
  }
  return artist || "Stopped station";
}

export function RadioPage() {
  const { startRadio, resumeStation, starting, error, status } = usePlayer();
  const navigate = useNavigate();
  const [suggestions, setSuggestions] = useState<CatalogTrack[]>([]);
  const [genres, setGenres] = useState<CatalogGenre[]>([]);
  const [artists, setArtists] = useState<CatalogArtist[]>([]);
  const [stations, setStations] = useState<Station[]>([]);
  const [localError, setLocalError] = useState<string | null>(null);
  const [resumingId, setResumingId] = useState<string | null>(null);

  const reloadStations = useCallback(() => {
    void api
      .listStations(8)
      .then((r) => setStations(r.stations))
      .catch(() => setStations([]));
  }, []);

  useEffect(() => {
    void api
      .listSeedSuggestions(12)
      .then((r) => setSuggestions(r.tracks))
      .catch(() => setSuggestions([]));
    void api
      .listGenres(24)
      .then((r) => setGenres(r.genres))
      .catch(() => setGenres([]));
    void api
      .listArtists()
      .then((r) => setArtists(r.artists.slice(0, 18)))
      .catch(() => setArtists([]));
    reloadStations();
  }, [reloadStations]);

  useEffect(() => {
    reloadStations();
  }, [status?.id, status?.status, reloadStations]);

  const playSeed = async (seed: number | { seed_artist_id?: number; seed_genre_id?: number }) => {
    setLocalError(null);
    try {
      await startRadio(seed);
      navigate("/now-playing");
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : "failed");
    }
  };

  return (
    <section>
      <h1 className="page-title page-title-sm">Radio</h1>
      <p className="muted" style={{ marginTop: 0 }}>
        Start a continuous station from a track, artist, or genre seed — or pick up a recent one.
        Queue source tags show neighbor vs bridge vs pin when listening.
      </p>
      {(localError || error) && <p className="error">{localError || error}</p>}

      {stations.length > 0 ? (
        <>
          <h2 className="section-title">Your stations</h2>
          <div className="cover-grid">
            {stations.map((st) => (
              <button
                key={st.id}
                type="button"
                className="tile tile-cover-card"
                disabled={!!resumingId || starting}
                onClick={() => {
                  setResumingId(st.id);
                  setLocalError(null);
                  void resumeStation(st)
                    .then(() => navigate("/now-playing"))
                    .catch((err) => setLocalError(err instanceof Error ? err.message : "failed"))
                    .finally(() => setResumingId(null));
                }}
              >
                {stationCover(st) ? (
                  <img className="tile-cover" src={stationCover(st)} alt="" />
                ) : (
                  <div className="tile-cover tile-cover-fallback" aria-hidden>
                    {(stationTitle(st)[0] || "?").toUpperCase()}
                  </div>
                )}
                <span className="tile-label">{stationTitle(st)}</span>
                <span className="tile-sub muted">{stationSubtitle(st)}</span>
              </button>
            ))}
          </div>
        </>
      ) : null}

      <h2 className="section-title">Quick seeds</h2>
      <div className="cover-grid">
        {suggestions.map((t) => (
          <button
            key={t.id}
            type="button"
            className="tile tile-cover-card"
            disabled={starting}
            onClick={() => void playSeed(t.id)}
          >
            {t.cover_url ? (
              <img className="tile-cover" src={t.cover_url} alt="" />
            ) : (
              <div className="tile-cover tile-cover-fallback" aria-hidden>
                {(t.title[0] || "?").toUpperCase()}
              </div>
            )}
            <span className="tile-label">{t.title}</span>
            <span className="tile-sub muted">{t.artist}</span>
          </button>
        ))}
      </div>

      {genres.length > 0 ? (
        <>
          <h2 className="section-title">Seed by genre</h2>
          <div className="chip-row">
            {genres.map((g) => (
              <button
                key={g.id}
                type="button"
                className="btn btn-ghost"
                disabled={starting}
                onClick={() => void playSeed({ seed_genre_id: g.id })}
              >
                {g.name} <span className="muted">({g.count})</span>
              </button>
            ))}
          </div>
        </>
      ) : null}

      {artists.length > 0 ? (
        <>
          <h2 className="section-title">Seed by artist</h2>
          <div className="cover-grid cover-grid-artists">
            {artists.map((a) => (
              <button
                key={a.id}
                type="button"
                className="tile tile-cover-card"
                disabled={starting}
                onClick={() => void playSeed({ seed_artist_id: a.id })}
              >
                {a.cover_url ? (
                  <img className="tile-cover" src={a.cover_url} alt="" />
                ) : (
                  <div className="tile-cover tile-cover-fallback" aria-hidden>
                    {(a.name[0] || "?").toUpperCase()}
                  </div>
                )}
                <span className="tile-label">{a.name}</span>
              </button>
            ))}
          </div>
        </>
      ) : null}
    </section>
  );
}

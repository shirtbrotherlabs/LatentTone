/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-17
 */

import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogTrack, Station } from "../api/types";
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
    reloadStations();
  }, [reloadStations]);

  useEffect(() => {
    reloadStations();
  }, [status?.id, status?.status, reloadStations]);

  const playSeed = async (trackId: number) => {
    setLocalError(null);
    try {
      await startRadio(trackId);
      navigate("/now-playing");
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : "failed");
    }
  };

  return (
    <section>
      <h1 className="page-title page-title-sm">Radio</h1>
      <p className="muted" style={{ marginTop: 0 }}>
        Start a continuous station from a seed, or pick up a recent one.
      </p>

      {(localError || error) && <p className="error">{localError || error}</p>}

      {stations.length > 0 ? (
        <div className="station-section">
          <h2>Pick up where you left off</h2>
          <p className="muted station-hint">
            Recent radio stations. Live tiles reconnect when possible; stopped tiles start a new
            station from the last track.
          </p>
          <div className="grid-list seed-grid">
            {stations.map((st) => {
              const cover = stationCover(st);
              const busy = resumingId === st.id || starting;
              return (
                <div key={st.id} className="tile seed-tile station-tile">
                  {cover ? (
                    <img className="seed-cover" src={cover} alt="" />
                  ) : (
                    <div className="seed-cover seed-cover-fallback" aria-hidden>
                      LT
                    </div>
                  )}
                  <div className="seed-tile-text">
                    <h3>{stationTitle(st)}</h3>
                    <p>
                      {stationSubtitle(st)}
                      {st.started_at ? ` · ${st.started_at.slice(0, 16).replace("T", " ")}` : ""}
                    </p>
                  </div>
                  <button
                    type="button"
                    className="btn btn-compact"
                    disabled={busy}
                    title="Play"
                    onClick={() => {
                      setLocalError(null);
                      setResumingId(st.id);
                      void resumeStation(st)
                        .then(() => navigate("/now-playing"))
                        .catch((err) => {
                          setLocalError(err instanceof Error ? err.message : "failed");
                        })
                        .finally(() => setResumingId(null));
                    }}
                  >
                    {busy && resumingId === st.id ? "…" : "Play"}
                  </button>
                </div>
              );
            })}
          </div>
        </div>
      ) : null}

      <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 400 }}>Quick seeds</h2>
      <div className="grid-list seed-grid" style={{ marginTop: "0.75rem" }}>
        {suggestions.map((t) => (
          <button
            key={t.id}
            type="button"
            className="tile seed-tile"
            disabled={starting}
            onClick={() => void playSeed(t.id)}
          >
            {t.cover_url ? (
              <img className="seed-cover" src={t.cover_url} alt="" />
            ) : (
              <div className="seed-cover seed-cover-fallback" aria-hidden>
                LT
              </div>
            )}
            <div className="seed-tile-text">
              <h3>{t.title}</h3>
              <p>{t.artist}</p>
            </div>
          </button>
        ))}
      </div>
    </section>
  );
}

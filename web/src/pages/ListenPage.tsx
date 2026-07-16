/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogTrack, QueueTrack, Station } from "../api/types";
import { TrackTable, type TrackTableRow } from "../components/TrackTable";
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

function asTableRow(track: CatalogTrack | QueueTrack): TrackTableRow {
  const q = track as QueueTrack;
  return {
    ...track,
    id: track.id,
    feedback: q.feedback ?? track.feedback,
    play_count: q.play_count ?? track.play_count,
  };
}

export function ListenPage() {
  const {
    status,
    nowTrack,
    historyTracks,
    queueTracks,
    startRadio,
    resumeStation,
    starting,
    error,
    stop,
    trackFeedback,
    removeFromQueue,
  } = usePlayer();
  const [seed, setSeed] = useState("");
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

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setLocalError(null);
    const id = Number(seed);
    if (!Number.isFinite(id) || id <= 0) {
      setLocalError("Enter a numeric catalog track id");
      return;
    }
    try {
      await startRadio(id);
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : "failed");
    }
  };

  const sessionActive = status && status.status !== "stopped";
  const trackId = nowTrack?.id ?? status?.now_playing?.track_id;
  const artistId = nowTrack?.artist_id;
  const albumId = nowTrack?.album_id;
  const currentAsTrack: QueueTrack | null = nowTrack
    ? {
        ...nowTrack,
        feedback: trackFeedback ?? status?.now_playing?.feedback ?? nowTrack.feedback,
        play_count: status?.now_playing?.play_count ?? nowTrack.play_count,
      }
    : trackId
      ? {
          id: trackId,
          title: `Track #${trackId}`,
          artist: "",
          album: "",
          feedback: trackFeedback ?? status?.now_playing?.feedback,
          play_count: status?.now_playing?.play_count,
        }
      : null;

  return (
    <section>
      <h1 className="page-title page-title-sm">Now Playing</h1>

      <form className="toolbar" onSubmit={(e) => void onSubmit(e)}>
        <input
          className="seed-input"
          placeholder="Seed track id"
          value={seed}
          onChange={(e) => setSeed(e.target.value)}
        />
        <button className="btn" type="submit" disabled={starting}>
          {starting ? "Starting…" : "Start radio"}
        </button>
        <Link className="btn btn-ghost" to="/library/tracks">
          Browse tracks
        </Link>
        {sessionActive ? (
          <button type="button" className="btn btn-danger" onClick={() => void stop()}>
            End station
          </button>
        ) : null}
      </form>

      {(localError || error) && <p className="error">{localError || error}</p>}

      {sessionActive ? (
        <div className="listen-now">
          <div className="np-header">
            {nowTrack?.cover_url ? (
              <img className="np-cover" src={nowTrack.cover_url} alt="" />
            ) : (
              <div className="np-cover np-cover-fallback" aria-hidden>
                LT
              </div>
            )}
            <div className="np-meta">
              <div className="np-label">Now playing</div>
              <h2 className="np-title">
                {trackId ? (
                  <Link className="fp-link" to={`/library/tracks/${trackId}`}>
                    {nowTrack?.title || `Track #${trackId}`}
                  </Link>
                ) : (
                  "…"
                )}
              </h2>
              <p className="np-links">
                {artistId ? (
                  <Link className="fp-link" to={`/library/artists/${artistId}`}>
                    {nowTrack?.artist || "Artist"}
                  </Link>
                ) : (
                  <span>{nowTrack?.artist || "Loading…"}</span>
                )}
                {nowTrack?.album ? (
                  <>
                    <span className="muted"> · </span>
                    {albumId ? (
                      <Link className="fp-link" to={`/library/albums/${albumId}`}>
                        {nowTrack.album}
                      </Link>
                    ) : (
                      <span>{nowTrack.album}</span>
                    )}
                  </>
                ) : null}
              </p>
            </div>
          </div>

          <div className="session-timeline">
            {historyTracks.length > 0 ? (
              <>
                <h2 className="queue-heading">Recently played</h2>
                <TrackTable
                  tracks={historyTracks.map(asTableRow)}
                  rowClassName={() => "track-table-row-past"}
                />
              </>
            ) : null}

            {currentAsTrack ? (
              <>
                <h2 className="queue-heading">Now</h2>
                <TrackTable
                  tracks={[asTableRow(currentAsTrack)]}
                  rowClassName={() => "track-table-row-now"}
                  renderTitleBadge={() => <span className="queue-badge">Playing</span>}
                />
              </>
            ) : null}

            <h2 className="queue-heading">Up next</h2>
            {queueTracks.length === 0 ? (
              <p className="muted">No upcoming tracks yet — affinity will fill the queue.</p>
            ) : (
              <TrackTable
                tracks={queueTracks.map(asTableRow)}
                rowClassName={() => "track-table-row-next"}
                renderTrailing={(t) => (
                  <button
                    type="button"
                    className="queue-remove-btn"
                    aria-label={`Remove ${t.title} from up next`}
                    title="Remove from up next"
                    onClick={() => {
                      setLocalError(null);
                      void removeFromQueue(t.id).catch((err) => {
                        setLocalError(err instanceof Error ? err.message : "failed");
                      });
                    }}
                  >
                    ×
                  </button>
                )}
              />
            )}
          </div>
        </div>
      ) : null}

      {stations.length > 0 ? (
        <div className="station-section">
          <h2>Pick up where you left off</h2>
          <p className="muted station-hint">
            Recent radio stations you started. Live rows reconnect when possible; stopped rows start a
            new station from the last track (“Continue from …”).
          </p>
          <ul className="station-list">
            {stations.map((st) => {
              const cover = stationCover(st);
              const live = st.status === "playing" || st.status === "created";
              const continueFrom =
                st.now_playing?.title || st.seed_track?.title || stationTitle(st);
              const actionLabel = live ? "Resume" : `Continue from ${continueFrom}`;
              const busy = resumingId === st.id || starting;
              return (
                <li key={st.id} className="station-row">
                  {cover ? (
                    <img className="station-cover" src={cover} alt="" />
                  ) : (
                    <div className="station-cover station-cover-fallback" aria-hidden>
                      LT
                    </div>
                  )}
                  <div className="station-meta">
                    <h3>{stationTitle(st)}</h3>
                    <p>
                      {stationSubtitle(st)}
                      {st.started_at ? ` · ${st.started_at.slice(0, 16).replace("T", " ")}` : ""}
                    </p>
                  </div>
                  <button
                    type="button"
                    className="btn"
                    disabled={busy}
                    title={actionLabel}
                    onClick={() => {
                      setLocalError(null);
                      setResumingId(st.id);
                      void resumeStation(st)
                        .catch((err) => {
                          setLocalError(err instanceof Error ? err.message : "failed");
                        })
                        .finally(() => setResumingId(null));
                    }}
                  >
                    {busy && resumingId === st.id ? "Starting…" : actionLabel}
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}

      <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 400 }}>Quick seeds</h2>
      <div className="grid-list seed-grid" style={{ marginTop: "0.75rem" }}>
        {suggestions.map((t) => (
          <button
            key={t.id}
            type="button"
            className="tile seed-tile"
            onClick={() => void startRadio(t.id)}
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
              <p>
                {t.artist} · id {t.id}
              </p>
            </div>
          </button>
        ))}
      </div>
    </section>
  );
}

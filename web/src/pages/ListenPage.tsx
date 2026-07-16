/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogTrack, QueueTrack, Station } from "../api/types";
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

function ThumbUpMini({ filled }: { filled?: boolean }) {
  return (
    <svg className="queue-thumb-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={filled ? 0 : 1.7}
        d="M10.8 20.2H6.4c-.9 0-1.6-.7-1.6-1.6v-6.1c0-.5.2-1 .6-1.3l5.2-4.5c.5-.4 1.2-.5 1.8-.2.7.3 1.1 1 1.1 1.8v2.3h3.7c1.3 0 2.3 1.2 2.1 2.5l-.9 5.2c-.2 1.1-1.2 1.9-2.3 1.9h-5.3z"
      />
    </svg>
  );
}

function ThumbDownMini({ filled }: { filled?: boolean }) {
  return (
    <svg className="queue-thumb-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={filled ? 0 : 1.7}
        d="M13.2 3.8h4.4c.9 0 1.6.7 1.6 1.6v6.1c0 .5-.2 1-.6 1.3l-5.2 4.5c-.5.4-1.2.5-1.8.2-.7-.3-1.1-1-1.1-1.8v-2.3H6.8c-1.3 0-2.3-1.2-2.1-2.5l.9-5.2c.2-1.1 1.2-1.9 2.3-1.9h5.3z"
      />
    </svg>
  );
}

function TrackRow({
  track,
  badge,
  className,
}: {
  track: CatalogTrack | QueueTrack;
  badge?: ReactNode;
  className?: string;
}) {
  const q = track as QueueTrack;
  const liked = q.feedback === "like";
  const disliked = q.feedback === "dislike";
  const plays = typeof q.play_count === "number" ? q.play_count : 0;

  return (
    <li className={`queue-row ${className ?? ""}`.trim()}>
      {track.cover_url ? (
        <img className="queue-cover" src={track.cover_url} alt="" />
      ) : (
        <div className="queue-cover queue-cover-fallback" aria-hidden />
      )}
      <div className="queue-row-text">
        <div className="queue-row-title-line">
          <Link className="track-title" to={`/library/tracks/${track.id}`}>
            {track.title}
          </Link>
          {badge}
          <span className="queue-thumbs" aria-label={liked ? "Liked" : disliked ? "Disliked" : "No rating"}>
            <span className={liked ? "is-on" : undefined} title="Like">
              <ThumbUpMini filled={liked} />
            </span>
            <span className={disliked ? "is-on" : undefined} title="Dislike">
              <ThumbDownMini filled={disliked} />
            </span>
          </span>
        </div>
        <div className="queue-row-sub muted">
          {track.artist_id ? (
            <Link className="fp-link" to={`/library/artists/${track.artist_id}`}>
              {track.artist || "Unknown artist"}
            </Link>
          ) : (
            <span>{track.artist || "Unknown artist"}</span>
          )}
          {track.album ? (
            <>
              {" · "}
              {track.album_id ? (
                <Link className="fp-link" to={`/library/albums/${track.album_id}`}>
                  {track.album}
                </Link>
              ) : (
                track.album
              )}
            </>
          ) : null}
          {" · "}
          <span className="queue-plays">Plays {plays}</span>
          {"source" in track && (track as QueueTrack).source === "user_pin" ? " · pinned" : ""}
        </div>
      </div>
    </li>
  );
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
        feedback: trackFeedback ?? status?.now_playing?.feedback,
        play_count: status?.now_playing?.play_count,
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
                <ol className="queue-list queue-list-rich">
                  {historyTracks.map((t, idx) => (
                    <TrackRow key={`h-${t.id}-${idx}`} track={t} className="queue-row-past" />
                  ))}
                </ol>
              </>
            ) : null}

            {currentAsTrack ? (
              <>
                <h2 className="queue-heading">Now</h2>
                <ol className="queue-list queue-list-rich">
                  <TrackRow
                    track={currentAsTrack}
                    className="queue-row-now"
                    badge={<span className="queue-badge">Playing</span>}
                  />
                </ol>
              </>
            ) : null}

            <h2 className="queue-heading">Up next</h2>
            {queueTracks.length === 0 ? (
              <p className="muted">No upcoming tracks yet — affinity will fill the queue.</p>
            ) : (
              <ol className="queue-list queue-list-rich">
                {queueTracks.map((t, idx) => (
                  <TrackRow key={`q-${t.id}-${idx}`} track={t} className="queue-row-next" />
                ))}
              </ol>
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

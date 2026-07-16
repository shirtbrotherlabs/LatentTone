/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { usePlayer } from "./PlayerContext";

function SpeakerIcon({ level, muted }: { level: number; muted: boolean }) {
  const waves = muted || level <= 0 ? 0 : level < 0.34 ? 1 : level < 0.67 ? 2 : 3;
  return (
    <svg className="speaker-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="currentColor"
        d="M3 9h3.5L12 5.5v13L6.5 15H3V9z"
        opacity={muted ? 0.4 : 1}
      />
      {waves >= 1 ? (
        <path
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
          d="M15 9.2a3.2 3.2 0 0 1 0 5.6"
        />
      ) : null}
      {waves >= 2 ? (
        <path
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
          d="M17.4 7a5.5 5.5 0 0 1 0 10"
        />
      ) : null}
      {waves >= 3 ? (
        <path
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
          d="M19.7 4.8a8 8 0 0 1 0 14.4"
        />
      ) : null}
      {muted ? (
        <path
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          d="M4 4l16 16"
        />
      ) : null}
    </svg>
  );
}

function PlayIcon() {
  return (
    <svg className="transport-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path fill="currentColor" d="M8 5.5v13l11-6.5-11-6.5z" />
    </svg>
  );
}

function PauseSquareIcon() {
  return (
    <svg className="transport-icon" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="6" y="6" width="12" height="12" rx="1.2" fill="currentColor" />
    </svg>
  );
}

function SkipForwardIcon() {
  return (
    <svg className="transport-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path fill="currentColor" d="M4 6.2v11.6l8.2-5.8L4 6.2z" />
      <path fill="currentColor" d="M12.2 6.2v11.6L20.4 12l-8.2-5.8z" />
      <rect x="20.2" y="5.5" width="2.2" height="13" rx="0.6" fill="currentColor" />
    </svg>
  );
}

/** Tape-style previous / restart (◀◀ with leading bar). */
function SkipBackIcon() {
  return (
    <svg className="transport-icon" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="1.6" y="5.5" width="2.2" height="13" rx="0.6" fill="currentColor" />
      <path fill="currentColor" d="M19.8 6.2v11.6L11.6 12l8.2-5.8z" />
      <path fill="currentColor" d="M11.6 6.2v11.6L3.4 12l8.2-5.8z" />
    </svg>
  );
}

function ThumbUpIcon({ filled }: { filled?: boolean }) {
  return (
    <svg className="transport-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={filled ? 0 : 1.6}
        d="M10.8 20.2H6.4c-.9 0-1.6-.7-1.6-1.6v-6.1c0-.5.2-1 .6-1.3l5.2-4.5c.5-.4 1.2-.5 1.8-.2.7.3 1.1 1 1.1 1.8v2.3h3.7c1.3 0 2.3 1.2 2.1 2.5l-.9 5.2c-.2 1.1-1.2 1.9-2.3 1.9h-5.3z"
      />
    </svg>
  );
}

function ThumbDownIcon({ filled }: { filled?: boolean }) {
  return (
    <svg className="transport-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={filled ? 0 : 1.6}
        d="M13.2 3.8h4.4c.9 0 1.6.7 1.6 1.6v6.1c0 .5-.2 1-.6 1.3l-5.2 4.5c-.5.4-1.2.5-1.8.2-.7-.3-1.1-1-1.1-1.8v-2.3H6.8c-1.3 0-2.3-1.2-2.1-2.5l.9-5.2c.2-1.1 1.2-1.9 2.3-1.9h5.3z"
      />
    </svg>
  );
}

function formatTime(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return "0:00";
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${s.toString().padStart(2, "0")}`;
}

function Spectrum({ active }: { active: boolean }) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const { getAnalyser } = usePlayer();

  useEffect(() => {
    if (!active) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx2d = canvas.getContext("2d");
    if (!ctx2d) return;
    let raf = 0;
    const bars = 28;
    const data = new Uint8Array(64);

    const syncSize = () => {
      const cssW = canvas.clientWidth || 168;
      const cssH = canvas.clientHeight || 36;
      const dpr = window.devicePixelRatio || 1;
      const w = Math.max(1, Math.round(cssW * dpr));
      const h = Math.max(1, Math.round(cssH * dpr));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
      return { w, h };
    };

    const draw = () => {
      const { w, h } = syncSize();
      const analyser = getAnalyser();
      const bg = ctx2d.createLinearGradient(0, 0, 0, h);
      bg.addColorStop(0, "rgba(28, 32, 40, 0.55)");
      bg.addColorStop(1, "rgba(14, 17, 22, 0.35)");
      ctx2d.fillStyle = bg;
      ctx2d.fillRect(0, 0, w, h);
      if (analyser) {
        analyser.getByteFrequencyData(data);
        const gap = 2 * (window.devicePixelRatio || 1);
        const barW = Math.max(2, (w - gap * (bars - 1)) / bars);
        for (let i = 0; i < bars; i++) {
          const v = data[i] / 255;
          const barH = Math.max(1.5, v * (h - 4));
          const x = i * (barW + gap);
          const t = i / (bars - 1);
          const shade = Math.round(150 + t * 55);
          const alpha = 0.35 + v * 0.55;
          ctx2d.fillStyle = `rgba(${shade}, ${shade + 4}, ${shade + 10}, ${alpha})`;
          const y = h - barH - 1;
          ctx2d.fillRect(x, y, barW, barH);
        }
      }
      raf = window.requestAnimationFrame(draw);
    };
    raf = window.requestAnimationFrame(draw);
    return () => window.cancelAnimationFrame(raf);
  }, [active, getAnalyser]);

  if (!active) return null;
  return <canvas ref={canvasRef} className="fp-spectrum" aria-hidden />;
}

export function FloatingPlayer() {
  const {
    status,
    nowTrack,
    nextTrack,
    error,
    feedback,
    goBack,
    togglePlayPause,
    paused,
    skipping,
    volume,
    muted,
    setVolume,
    toggleMute,
    trackFeedback,
    currentTime,
    duration,
    seekTo,
    startRadio,
    starting,
  } = usePlayer();
  const [coverOpen, setCoverOpen] = useState(false);
  const active = !!(status && status.status !== "stopped");

  useEffect(() => {
    if (!coverOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setCoverOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [coverOpen]);

  if (!active) {
    if (nowTrack) {
      const trackId = nowTrack.id;
      const continueLabel = nowTrack.title
        ? `Continue from ${nowTrack.title}`
        : "Continue from last track";
      return (
        <div className="floating-player" aria-live="polite">
          <div className="fp-idle-track">
            {nowTrack.cover_url ? (
              <img className="fp-cover" src={nowTrack.cover_url} alt="" />
            ) : (
              <div className="fp-cover fp-cover-fallback" aria-hidden>
                LT
              </div>
            )}
            <div className="fp-now-text">
              <h2>
                <Link className="fp-link" to={`/library/tracks/${trackId}`}>
                  {nowTrack.title || `Track #${trackId}`}
                </Link>
              </h2>
              <p>
                {nowTrack.artist_id ? (
                  <Link className="fp-link" to={`/library/artists/${nowTrack.artist_id}`}>
                    {nowTrack.artist || "Unknown artist"}
                  </Link>
                ) : (
                  nowTrack.artist || "Unknown artist"
                )}
                {nowTrack.album ? (
                  <>
                    {" · "}
                    {nowTrack.album_id ? (
                      <Link className="fp-link" to={`/library/albums/${nowTrack.album_id}`}>
                        {nowTrack.album}
                      </Link>
                    ) : (
                      nowTrack.album
                    )}
                  </>
                ) : null}
              </p>
              <p className="muted" style={{ margin: "0.25rem 0 0", fontSize: "0.75rem" }}>
                Last played — start a new station from this track (stopped sessions are not live).
              </p>
            </div>
            <div className="fp-idle-actions">
              <button
                type="button"
                className="btn"
                disabled={starting}
                title={continueLabel}
                onClick={() => void startRadio(trackId)}
              >
                {starting ? "Starting…" : continueLabel}
              </button>
            </div>
          </div>
        </div>
      );
    }
    return (
      <div className="floating-player" aria-live="polite">
        <div className="fp-idle">
          No active station — pick a track and start radio, or use Play next from the library.
        </div>
      </div>
    );
  }

  const pins = status.queue?.filter((q) => q.source === "user_pin").length ?? 0;
  const nextLabel = nextTrack
    ? `${nextTrack.title}${nextTrack.artist ? ` — ${nextTrack.artist}` : ""}`
    : status.queue?.[0]
      ? `Track #${status.queue[0].track_id}`
      : "Queue empty";
  const displayVol = muted ? 0 : volume;
  const trackId = nowTrack?.id ?? status.now_playing?.track_id;
  const artistId = nowTrack?.artist_id;
  const albumId = nowTrack?.album_id;
  const title = nowTrack?.title || `Track #${trackId ?? "—"}`;
  const artist = nowTrack?.artist || "Loading metadata…";
  const album = nowTrack?.album;
  const coverUrl = nowTrack?.cover_url;
  const seekMax = duration > 0 ? duration : 0;
  const seekVal = seekMax > 0 ? Math.min(currentTime, seekMax) : 0;

  return (
    <>
      <div className="floating-player" aria-live="polite">
        <div className="fp-now">
          {coverUrl ? (
            <button
              type="button"
              className="fp-cover-btn"
              aria-label="View album cover"
              onClick={() => setCoverOpen(true)}
            >
              <img className="fp-cover" src={coverUrl} alt="" />
            </button>
          ) : (
            <div className="fp-cover fp-cover-fallback" aria-hidden>
              LT
            </div>
          )}
          <div className="fp-now-text">
            <h2>
              {trackId ? (
                <Link className="fp-link" to={`/library/tracks/${trackId}`}>
                  {title}
                </Link>
              ) : (
                title
              )}
            </h2>
            <p>
              {artistId ? (
                <Link className="fp-link" to={`/library/artists/${artistId}`}>
                  {artist}
                </Link>
              ) : (
                artist
              )}
              {album ? (
                <>
                  {" · "}
                  {albumId ? (
                    <Link className="fp-link" to={`/library/albums/${albumId}`}>
                      {album}
                    </Link>
                  ) : (
                    album
                  )}
                </>
              ) : null}
              {error ? ` · ${error}` : ""}
            </p>
          </div>
          <div className="fp-seek">
            <span className="fp-seek-time">{formatTime(seekVal)}</span>
            <input
              type="range"
              className="fp-seek-range"
              min={0}
              max={seekMax || 1}
              step={0.25}
              value={seekVal}
              disabled={!seekMax}
              aria-label="Seek"
              onChange={(e) => seekTo(Number(e.target.value))}
            />
            <span className="fp-seek-time">{seekMax ? formatTime(seekMax) : "—:——"}</span>
          </div>
        </div>
        <div className="fp-controls">
          <Spectrum active={active} />
          <button
            type="button"
            className={`btn-icon btn-transport${trackFeedback === "like" ? " is-active" : ""}`}
            aria-label="Like"
            title="Like"
            aria-pressed={trackFeedback === "like"}
            onClick={() => void feedback("like")}
          >
            <ThumbUpIcon filled={trackFeedback === "like"} />
          </button>
          <button
            type="button"
            className={`btn-icon btn-transport${trackFeedback === "dislike" ? " is-active" : ""}`}
            aria-label="Dislike"
            title="Dislike"
            aria-pressed={trackFeedback === "dislike"}
            onClick={() => void feedback("dislike")}
          >
            <ThumbDownIcon filled={trackFeedback === "dislike"} />
          </button>
          <button
            type="button"
            className="btn-icon btn-transport"
            aria-label="Previous"
            title="Previous / restart"
            onClick={() => void goBack()}
          >
            <SkipBackIcon />
          </button>
          <button
            type="button"
            className="btn-icon btn-transport"
            aria-label={paused ? "Play" : "Pause"}
            title={paused ? "Play" : "Pause"}
            onClick={() => void togglePlayPause()}
          >
            {paused ? <PlayIcon /> : <PauseSquareIcon />}
          </button>
          <button
            type="button"
            className="btn-icon btn-transport"
            aria-label="Skip"
            title="Skip"
            disabled={skipping}
            onClick={() => void feedback("skip")}
          >
            <SkipForwardIcon />
          </button>
          <div className="fp-volume">
            <button
              type="button"
              className="btn-icon"
              aria-label={muted ? "Unmute" : "Mute"}
              title={muted ? "Unmute" : "Mute"}
              onClick={() => toggleMute()}
            >
              <SpeakerIcon level={displayVol} muted={muted} />
            </button>
            <input
              type="range"
              min={0}
              max={1}
              step={0.01}
              value={displayVol}
              aria-label="Volume"
              onChange={(e) => setVolume(Number(e.target.value))}
            />
          </div>
        </div>
        <div className="fp-queue">
          <div className="fp-next-label">Up next</div>
          <div className="fp-next-track" title={nextLabel}>
            {nextLabel}
          </div>
          {pins > 0 ? <div className="pill">{pins} pinned</div> : null}
        </div>
      </div>
      {coverOpen && coverUrl ? (
        <button
          type="button"
          className="cover-lightbox"
          aria-label="Dismiss album cover"
          onClick={() => setCoverOpen(false)}
        >
          <img src={coverUrl} alt="" />
        </button>
      ) : null}
    </>
  );
}

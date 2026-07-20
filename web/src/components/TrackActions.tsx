/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 * Last-Modified: 2026-07-20
 */

import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogTrack } from "../api/types";
import { usePlayer } from "../player/PlayerContext";

type Props = {
  track: CatalogTrack;
};

export function TrackActions({ track }: Props) {
  const [open, setOpen] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const { playNext, startRadio } = usePlayer();

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  const run = async (fn: () => Promise<void>, ok?: string) => {
    setMsg(null);
    try {
      await fn();
      if (ok) setMsg(ok);
      setOpen(false);
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "failed");
    }
  };

  return (
    <div className="row-actions" ref={ref}>
      <button
        type="button"
        className="menu-btn"
        aria-label="Track actions"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        ⋯
      </button>
      {open ? (
        <div className="menu" role="menu">
          <button type="button" role="menuitem" onClick={() => void run(() => playNext(track.id), "Queued next")}>
            Play next
          </button>
          <button type="button" role="menuitem" onClick={() => navigate(`/library/tracks/${track.id}`)}>
            More info
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setOpen(false);
              // Same-origin navigation so the browser honors Content-Disposition
              // and sends the session cookie (stream prefs apply server-side).
              window.location.assign(api.downloadTrackUrl(track.id));
            }}
          >
            Download
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() =>
              void run(async () => {
                const pl = await api.createPlaylist(`${track.title} mix`);
                await api.addPlaylistTracks(pl.id, [track.id]);
                navigate(`/playlists/${pl.id}`);
              })
            }
          >
            Create playlist from track
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => void run(() => startRadio(track.id), "Radio started")}
          >
            Create radio station from track
          </button>
        </div>
      ) : null}
      {msg ? <div className="muted" style={{ fontSize: "0.75rem" }}>{msg}</div> : null}
    </div>
  );
}

export function formatDuration(ms?: number | null) {
  if (!ms || ms <= 0) return "—";
  const s = Math.floor(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 *
 * Screen Wake Lock + Media Session helpers for Android / lock-screen chrome.
 * Soft-fails when APIs are missing (desktop, insecure HTTP, unsupported browsers).
 * Wake Lock requires a secure context (HTTPS or localhost).
 */

import { api } from "../api/client";
import type { CatalogTrack } from "../api/types";

type WakeLockSentinelLike = {
  released: boolean;
  release: () => Promise<void>;
  addEventListener: (type: "release", listener: () => void) => void;
};

type WakeLockNavigator = Navigator & {
  wakeLock?: {
    request: (type: "screen") => Promise<WakeLockSentinelLike>;
  };
};

let publicBaseURL = "";
let configPromise: Promise<string> | null = null;
let wakeLock: WakeLockSentinelLike | null = null;

function normalizeBase(raw: string): string {
  const s = raw.trim().replace(/\/+$/, "");
  return s || (typeof window !== "undefined" ? window.location.origin : "");
}

/** Load canonical public origin once (runtime config; falls back to page origin). */
export function ensurePublicBaseURL(): Promise<string> {
  if (publicBaseURL) return Promise.resolve(publicBaseURL);
  if (!configPromise) {
    configPromise = api
      .getPublicConfig()
      .then((c) => {
        publicBaseURL = normalizeBase(c.public_base_url || "");
        return publicBaseURL;
      })
      .catch(() => {
        publicBaseURL = typeof window !== "undefined" ? window.location.origin : "";
        return publicBaseURL;
      });
  }
  return configPromise;
}

/** Join a relative cover/stream path onto the public base (or leave absolute URLs). */
export function absoluteMediaURL(pathOrURL?: string | null): string | undefined {
  if (!pathOrURL) return undefined;
  const p = pathOrURL.trim();
  if (!p) return undefined;
  if (/^https?:\/\//i.test(p)) return p;
  const base = publicBaseURL || (typeof window !== "undefined" ? window.location.origin : "");
  if (!base) return p.startsWith("/") ? p : `/${p}`;
  const path = p.startsWith("/") ? p : `/${p}`;
  return `${base.replace(/\/+$/, "")}${path}`;
}

/** Request screen Wake Lock (no-op if unsupported / denied). Prefer calling from a user gesture. */
export async function acquireWakeLock(): Promise<void> {
  try {
    if (typeof window !== "undefined" && !window.isSecureContext) return;
    const nav = navigator as WakeLockNavigator;
    if (!nav.wakeLock) return;
    if (wakeLock && !wakeLock.released) return;
    const sentinel = await nav.wakeLock.request("screen");
    wakeLock = sentinel;
    sentinel.addEventListener("release", () => {
      if (wakeLock === sentinel) wakeLock = null;
    });
  } catch {
    /* soft-fail: insecure context, policy, or OS denial */
  }
}

export async function releaseWakeLock(): Promise<void> {
  const cur = wakeLock;
  wakeLock = null;
  if (!cur || cur.released) return;
  try {
    await cur.release();
  } catch {
    /* ignore */
  }
}

export type MediaSessionHandlers = {
  play: () => void | Promise<void>;
  pause: () => void | Promise<void>;
  previousTrack: () => void | Promise<void>;
  nextTrack: () => void | Promise<void>;
};

/** Wire OS media keys; pass null to clear. Soft-fails per unsupported action. */
export function bindMediaSessionHandlers(handlers: MediaSessionHandlers | null): void {
  if (typeof navigator === "undefined" || !("mediaSession" in navigator)) return;
  const ms = navigator.mediaSession;
  const pairs: [MediaSessionAction, (() => void) | null][] = [
    ["play", handlers ? () => void Promise.resolve(handlers.play()) : null],
    ["pause", handlers ? () => void Promise.resolve(handlers.pause()) : null],
    ["previoustrack", handlers ? () => void Promise.resolve(handlers.previousTrack()) : null],
    ["nexttrack", handlers ? () => void Promise.resolve(handlers.nextTrack()) : null],
  ];
  for (const [action, fn] of pairs) {
    try {
      ms.setActionHandler(action, fn);
    } catch {
      /* action not supported on this platform */
    }
  }
}

/** Update MediaSession metadata + playbackState from now-playing. */
export async function syncMediaSession(
  track: CatalogTrack | null,
  playing: boolean,
): Promise<void> {
  if (typeof navigator === "undefined" || !("mediaSession" in navigator)) return;
  await ensurePublicBaseURL();
  const ms = navigator.mediaSession;
  try {
    ms.playbackState = playing ? "playing" : "paused";
  } catch {
    /* ignore */
  }
  if (!track) {
    try {
      ms.metadata = null;
    } catch {
      /* ignore */
    }
    return;
  }
  const artworkURL = absoluteMediaURL(track.cover_url);
  try {
    ms.metadata = new MediaMetadata({
      title: track.title || `Track #${track.id}`,
      artist: track.artist || "",
      album: track.album || "",
      artwork: artworkURL
        ? [
            { src: artworkURL, sizes: "512x512", type: "image/jpeg" },
            { src: artworkURL, sizes: "256x256", type: "image/jpeg" },
          ]
        : [],
    });
  } catch {
    /* soft-fail */
  }
}

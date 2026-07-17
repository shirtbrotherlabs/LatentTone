/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 *
 * Progressive <audio> first for near-instant track switches; HLS optional warm path.
 * Web Audio gain + analyser for volume, mute, and spectrum.
 */

import Hls from "hls.js";

export type AttachOpts = {
  hlsUrl?: string;
  progressiveUrl?: string;
  /** Forces remount when session HLS URL is unchanged across track advances. */
  trackId?: number;
  /** Prefer progressive (per-track) URL so skip does not wait on FFmpeg HLS. */
  preferProgressive?: boolean;
  /** Bypass same-key short-circuit (e.g. re-attach after skip confirmed). */
  force?: boolean;
  onError?: (msg: string) => void;
  onPlayState?: (playing: boolean) => void;
};

const isDev = Boolean(import.meta.env?.DEV);

function isAbortLike(e: unknown): boolean {
  if (!e || typeof e !== "object") return false;
  const err = e as { name?: string; message?: string };
  if (err.name === "AbortError") return true;
  const msg = err.message ?? "";
  return /interrupted by a (?:new )?load request/i.test(msg) || /The play\(\) request was interrupted/i.test(msg);
}

export class AudioEngine {
  readonly audio: HTMLAudioElement;
  private hls: Hls | null = null;
  private ctx: AudioContext | null = null;
  private gain: GainNode | null = null;
  private analyser: AnalyserNode | null = null;
  private connected = false;
  private lastKey = "";
  private volume = 0.85;
  private muted = false;
  private preMuteVolume = 0.85;
  private playStateHandler: ((playing: boolean) => void) | null = null;
  private endedHandler: (() => void) | null = null;
  /** Bumps on each attach/load so superseded play() promises are ignored. */
  private playEpoch = 0;
  /** Serializes attach(); stale async attach bodies bail when this advances. */
  private attachSeq = 0;
  /** Last successfully requested catalog track id (0 = unknown). */
  private attachedTrackId = 0;

  constructor() {
    this.audio = new Audio();
    this.audio.preload = "auto";
    // Do not set crossOrigin for same-origin relative streams. Forcing
    // "use-credentials" turns the load into a CORS request; without ACAO headers
    // (typical behind nginx Basic Auth / same-origin cookie apps) browsers often
    // withhold duration/metadata and break the seek bar even when audio plays.
    // Same-origin MediaElementSource still works for the spectrum analyser.
    this.audio.volume = this.volume;
    this.audio.addEventListener("play", () => this.playStateHandler?.(true));
    this.audio.addEventListener("pause", () => this.playStateHandler?.(false));
    this.audio.addEventListener("ended", () => {
      if (isDev) console.debug("[lt-player] audio ended");
      this.endedHandler?.();
    });
  }

  setPlayStateHandler(handler: ((playing: boolean) => void) | null) {
    this.playStateHandler = handler;
  }

  setEndedHandler(handler: (() => void) | null) {
    this.endedHandler = handler;
  }

  private ensureGraph() {
    if (this.connected) return;
    try {
      const Ctx =
        window.AudioContext ||
        (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
      if (!Ctx) return;
      this.ctx = new Ctx();
      const source = this.ctx.createMediaElementSource(this.audio);
      this.gain = this.ctx.createGain();
      this.analyser = this.ctx.createAnalyser();
      this.analyser.fftSize = 64;
      this.analyser.smoothingTimeConstant = 0.75;
      this.gain.gain.value = this.muted ? 0.0001 : Math.max(this.volume, 0.0001);
      source.connect(this.gain);
      this.gain.connect(this.analyser);
      this.analyser.connect(this.ctx.destination);
      this.connected = true;
      // Prefer Web Audio gain; keep element volume at 1 when graph is live.
      this.audio.volume = 1;
    } catch {
      // Graceful: keep direct <audio> path.
      this.ctx = null;
      this.gain = null;
      this.analyser = null;
    }
  }

  getAnalyser(): AnalyserNode | null {
    this.ensureGraph();
    return this.analyser;
  }

  getVolume(): number {
    return this.volume;
  }

  isMuted(): boolean {
    return this.muted;
  }

  setVolume(v: number) {
    this.volume = Math.min(1, Math.max(0, v));
    if (this.volume > 0) {
      this.muted = false;
      this.preMuteVolume = this.volume;
    } else {
      this.muted = true;
    }
    this.applyGain();
  }

  toggleMute(): boolean {
    if (this.muted) {
      this.muted = false;
      this.volume = this.preMuteVolume > 0 ? this.preMuteVolume : 0.85;
    } else {
      this.preMuteVolume = this.volume > 0 ? this.volume : 0.85;
      this.muted = true;
    }
    this.applyGain();
    return this.muted;
  }

  /** Snap GainNode (or element.volume) to the current mute/volume level. Never leave the fade floor. */
  private applyGain() {
    const level = this.muted ? 0 : this.volume;
    if (this.gain && this.ctx) {
      const now = this.ctx.currentTime;
      const g = this.gain.gain;
      g.cancelScheduledValues(now);
      // Mute uses a true zero; audible levels stay above the exponential-ramp floor.
      g.setValueAtTime(this.muted ? 0 : Math.max(level, 0.0001), now);
      this.audio.volume = 1;
    } else {
      this.audio.volume = level;
    }
  }

  async softFadeIn(ms = 180) {
    this.ensureGraph();
    if (!this.ctx || !this.gain || this.muted) {
      this.applyGain();
      return;
    }
    const target = Math.max(this.volume, 0.0001);
    const g = this.gain.gain;
    const now = this.ctx.currentTime;
    g.cancelScheduledValues(now);
    g.setValueAtTime(0.001, now);
    try {
      g.exponentialRampToValueAtTime(target, now + ms / 1000);
      await new Promise((r) => setTimeout(r, ms));
    } catch {
      /* ramp unsupported — fall through to snap */
    }
    // Lock final level: ramps can be cancelled by a later attach/pause.
    this.applyGain();
  }

  async softFadeOut(ms = 120) {
    if (!this.ctx || !this.gain) return;
    const g = this.gain.gain;
    const now = this.ctx.currentTime;
    g.cancelScheduledValues(now);
    g.setValueAtTime(Math.max(g.value, 0.001), now);
    try {
      g.exponentialRampToValueAtTime(0.001, now + ms / 1000);
    } catch {
      g.setValueAtTime(0.001, now);
    }
    await new Promise((r) => setTimeout(r, ms));
    // Leave at fade floor only until the caller loads the next src and applyGain/softFadeIn.
  }

  /**
   * Progressive prefetch is intentionally a no-op when the stream may be
   * FFmpeg-transcoded (Opus/MP3/AAC prefs). A Range warm-up used to spawn a
   * full encode that raced the real <audio> GET and froze the server on skip.
   * Original-file streams are warmed by the browser media element itself.
   */
  prefetchProgressive(_url?: string) {
    /* no-op — see comment above */
  }

  /** Cancel in-flight play() and bump epoch so late rejects are ignored. */
  private invalidatePlay() {
    this.playEpoch++;
    try {
      this.audio.pause();
    } catch {
      /* ignore */
    }
  }

  private async waitCanPlay(timeoutMs = 2500): Promise<void> {
    if (this.audio.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) return;
    await new Promise<void>((resolve) => {
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        this.audio.removeEventListener("canplay", finish);
        this.audio.removeEventListener("loadeddata", finish);
        this.audio.removeEventListener("error", finish);
        resolve();
      };
      this.audio.addEventListener("canplay", finish, { once: true });
      this.audio.addEventListener("loadeddata", finish, { once: true });
      this.audio.addEventListener("error", finish, { once: true });
      window.setTimeout(finish, timeoutMs);
    });
  }

  private async safePlay(onError?: (msg: string) => void, fadeIn = true): Promise<void> {
    const epoch = this.playEpoch;
    try {
      this.ensureGraph();
      await this.ctx?.resume();
      if (epoch !== this.playEpoch) {
        this.applyGain();
        return;
      }
      // softFadeOut leaves GainNode ≈0.001. Always restore unless we intentionally fade in.
      if (fadeIn && this.gain && this.ctx && !this.muted) {
        const now = this.ctx.currentTime;
        this.gain.gain.cancelScheduledValues(now);
        this.gain.gain.setValueAtTime(0.001, now);
        this.audio.volume = 1;
      } else {
        this.applyGain();
      }
      await this.audio.play();
      if (epoch !== this.playEpoch) {
        this.applyGain();
        return;
      }
      if (fadeIn) await this.softFadeIn(120);
      else this.applyGain();
      if (epoch !== this.playEpoch) {
        this.applyGain();
        return;
      }
      this.playStateHandler?.(true);
    } catch (e) {
      // Never leave the graph at the fade floor after a failed/interrupted play.
      this.applyGain();
      if (epoch !== this.playEpoch || isAbortLike(e)) {
        return;
      }
      const msg = e instanceof Error ? e.message : "playback blocked";
      if (isDev) console.warn("[lt-player] play failed", msg);
      onError?.(msg);
    }
  }

  private mediaErrorMessage(): string {
    const err = this.audio.error;
    const src = this.audio.currentSrc || this.audio.src || "";
    const srcHint = src ? ` (${src.startsWith("http") ? "absolute URL" : "relative stream"})` : "";
    if (!err) return `couldn't fetch audio stream${srcHint}`;
    switch (err.code) {
      case MediaError.MEDIA_ERR_ABORTED:
        return "playback aborted";
      case MediaError.MEDIA_ERR_NETWORK:
        return `couldn't fetch audio stream (network)${srcHint}`;
      case MediaError.MEDIA_ERR_DECODE:
        return "browser could not decode this audio format";
      case MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED:
        return `couldn't fetch audio stream (unsupported or blocked)${srcHint}`;
      default:
        return err.message || `couldn't fetch audio stream${srcHint}`;
    }
  }

  private async loadProgressive(url: string) {
    this.invalidatePlay();
    this.destroyHls();
    this.audio.src = url;
    try {
      this.audio.load();
    } catch {
      /* ignore */
    }
    await this.waitCanPlay();
  }

  private async attachHls(opts: AttachOpts, onError?: (msg: string) => void) {
    if (!opts.hlsUrl) {
      onError?.("no HLS stream URL");
      return;
    }
    if (Hls.isSupported()) {
      this.invalidatePlay();
      const epoch = this.playEpoch;
      this.hls = new Hls({
        xhrSetup: (xhr) => {
          xhr.withCredentials = true;
        },
        manifestLoadingMaxRetry: 2,
        manifestLoadingRetryDelay: 200,
        levelLoadingMaxRetry: 2,
      });
      this.hls.loadSource(opts.hlsUrl);
      this.hls.attachMedia(this.audio);
      this.hls.on(Hls.Events.MANIFEST_PARSED, () => {
        if (epoch !== this.playEpoch) return;
        void this.safePlay(onError);
      });
      this.hls.on(Hls.Events.ERROR, (_evt, data) => {
        if (!data.fatal) return;
        if (opts.progressiveUrl && this.lastKey.endsWith("|hls")) {
          void (async () => {
            this.destroyHls();
            this.lastKey = `${opts.trackId ?? 0}|${opts.progressiveUrl}`;
            await this.loadProgressive(opts.progressiveUrl!);
            await this.safePlay(onError);
          })();
          return;
        }
        onError?.(typeof data.details === "string" ? data.details : data.type);
      });
      return;
    }
    if (this.audio.canPlayType("application/vnd.apple.mpegurl")) {
      this.invalidatePlay();
      this.audio.src = opts.hlsUrl;
      try {
        this.audio.load();
      } catch {
        /* ignore */
      }
      await this.waitCanPlay();
      await this.safePlay(onError);
      return;
    }
    onError?.("HLS not supported in this browser");
  }

  async attach(opts: AttachOpts) {
    const preferProgressive = opts.preferProgressive !== false;
    // Keep stream paths same-origin relative so reverse proxies need no host rewrite.
    let progressiveUrl = toSameOriginPath(opts.progressiveUrl);
    const hlsUrl = toSameOriginPath(opts.hlsUrl);
    // Relative / same-origin: leave CORS mode off so cookies + browser Basic Auth
    // credentials apply as a normal media request (duration/metadata available).
    this.audio.removeAttribute("crossorigin");
    const trackId = opts.trackId ?? 0;
    // Skip confirm often re-calls attach for the same track. Reloading aborts the
    // in-flight Opus/ffmpeg body and Chrome reports "no supported source was found."
    if (
      opts.force &&
      trackId > 0 &&
      this.isHealthyForTrack(trackId) &&
      !this.audio.paused
    ) {
      return;
    }
    const primary =
      preferProgressive && progressiveUrl
        ? progressiveUrl
        : hlsUrl || progressiveUrl || "";
    const key = `${trackId}|${primary}`;
    if (!primary) {
      opts.onError?.("no playable stream URL");
      return;
    }
    if (key === this.lastKey && !opts.force) {
      if (this.audio.paused) {
        // Same src re-play (e.g. status poll); restore gain in case a prior fade left it down.
        this.applyGain();
        await this.safePlay(opts.onError, false);
      }
      return;
    }
    // Force-reload of the same URL (error recovery): bust caches / aborted bodies.
    if (opts.force && progressiveUrl && key === this.lastKey) {
      const join = progressiveUrl.includes("?") ? "&" : "?";
      progressiveUrl = `${progressiveUrl}${join}_=${Date.now()}`;
    }
    if (isDev) {
      console.debug("[lt-player] attach", { trackId, primary: progressiveUrl || primary });
    }
    // Stop the previous track immediately. Soft-fading while still decoding the old
    // src raced with skip polls and left the prior song audible after UI advanced.
    const seq = ++this.attachSeq;
    const wasPlaying = !this.audio.paused && this.audio.readyState > 0;
    this.invalidatePlay();
    this.destroyHls();
    this.lastKey = `${trackId}|${preferProgressive && progressiveUrl ? progressiveUrl.split("?")[0] : primary}`;
    this.attachedTrackId = trackId;
    this.ensureGraph();
    if (seq !== this.attachSeq) return;

    // Skip / track advance: progressive is per-track and starts without waiting for HLS.
    if (preferProgressive && progressiveUrl) {
      this.audio.src = progressiveUrl;
      try {
        this.audio.load();
      } catch {
        /* ignore */
      }
      if (seq !== this.attachSeq) return;
      // New media: always snap gain before play (safePlay also restores / fades).
      this.applyGain();
      const epoch = this.playEpoch;
      const reportAttachFail = (detail: string) => {
        if (isDev) console.warn("[lt-player] attach failed", detail);
        opts.onError?.(detail);
      };
      const onProgError = () => {
        if (epoch !== this.playEpoch || seq !== this.attachSeq) return;
        this.applyGain();
        const detail = this.mediaErrorMessage();
        if (isDev) console.warn("[lt-player] progressive error", detail);
        // One retry with cache-bust; avoid session HLS (often still the previous track).
        if (!opts.force && progressiveUrl) {
          void this.attach({
            ...opts,
            progressiveUrl,
            force: true,
            onError: opts.onError,
          });
          return;
        }
        reportAttachFail(detail);
      };
      this.audio.addEventListener("error", onProgError, { once: true });
      // Play immediately for skip / natural advance; keeps MediaSession continuity
      // on Android when called from the ended handler (do not await canplay first).
      // Short fade-in only when replacing an already-playing track.
      void this.safePlay((msg) => {
        if (seq !== this.attachSeq) return;
        // Interrupted load from a newer attach — not a user-facing failure.
        if (/no supported source/i.test(msg) && seq !== this.attachSeq) return;
        reportAttachFail(msg);
      }, wasPlaying);
      return;
    }

    if (hlsUrl) {
      this.applyGain();
      await this.attachHls({ ...opts, hlsUrl, progressiveUrl }, opts.onError);
      return;
    }

    if (progressiveUrl) {
      await this.loadProgressive(progressiveUrl);
      this.applyGain();
      await this.safePlay(opts.onError, false);
      return;
    }

    opts.onError?.("no playable stream URL");
  }

  getCurrentTime(): number {
    return Number.isFinite(this.audio.currentTime) ? this.audio.currentTime : 0;
  }

  getDuration(): number {
    const d = this.audio.duration;
    return Number.isFinite(d) && d > 0 ? d : 0;
  }

  seekTo(seconds: number) {
    const d = this.getDuration();
    if (!d) return;
    const t = Math.min(Math.max(0, seconds), d);
    try {
      this.audio.currentTime = t;
    } catch {
      /* ignore */
    }
  }

  /** Restart current media from the beginning (station stays alive). */
  async restartFromStart() {
    try {
      this.audio.currentTime = 0;
    } catch {
      /* ignore */
    }
    await this.safePlay(undefined, false);
  }

  pause() {
    this.invalidatePlay();
    this.audio.pause();
  }

  async play() {
    await this.safePlay(undefined, false);
  }

  async togglePlayPause(): Promise<boolean> {
    if (this.audio.paused) {
      await this.play();
      return true;
    }
    this.pause();
    return false;
  }

  isPaused(): boolean {
    return this.audio.paused;
  }

  /** True when this engine is already loading/playing the given catalog track. */
  isAttachedToTrack(trackId?: number): boolean {
    if (!trackId || trackId <= 0) return false;
    return this.attachedTrackId === trackId && this.lastKey !== "";
  }

  /**
   * True when we should not interrupt an in-flight attach for this track.
   * Buffering counts as healthy — reloading mid-stream aborts FFmpeg/Opus.
   */
  isHealthyForTrack(trackId?: number): boolean {
    if (!this.isAttachedToTrack(trackId)) return false;
    if (this.audio.error) return false;
    return true;
  }

  destroyHls() {
    if (this.hls) {
      this.hls.destroy();
      this.hls = null;
    }
  }

  destroy() {
    this.invalidatePlay();
    this.destroyHls();
    this.endedHandler = null;
    this.audio.pause();
    this.audio.removeAttribute("src");
    this.lastKey = "";
  }
}

/** Prefer same-origin relative paths; strip accidental absolute public hosts. */
function toSameOriginPath(url?: string): string | undefined {
  if (!url) return undefined;
  const u = url.trim();
  if (!u) return undefined;
  if (u.startsWith("/")) return u;
  try {
    const parsed = new URL(u, typeof window !== "undefined" ? window.location.origin : undefined);
    if (typeof window !== "undefined" && parsed.origin === window.location.origin) {
      return parsed.pathname + parsed.search;
    }
  } catch {
    /* fall through */
  }
  // Non-same-origin absolute URLs are rejected for streams (proxy / cookie safe).
  if (/^https?:\/\//i.test(u)) {
    try {
      const parsed = new URL(u);
      return parsed.pathname + parsed.search;
    } catch {
      return u;
    }
  }
  return u.startsWith("/") ? u : `/${u}`;
}

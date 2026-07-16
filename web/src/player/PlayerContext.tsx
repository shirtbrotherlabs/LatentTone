/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { ApiError, api } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import type { CatalogTrack, QueueTrack, SessionStatus, Station } from "../api/types";
import { AudioEngine } from "./audioEngine";
import {
  acquireWakeLock,
  bindMediaSessionHandlers,
  ensurePublicBaseURL,
  releaseWakeLock,
  syncMediaSession,
} from "./mediaChrome";

const SESSION_KEY = "lt_listen_session";

function isLiveStatus(status: string | undefined): boolean {
  return status === "playing" || status === "created";
}

function isBenignPlaybackError(msg: string): boolean {
  return (
    /interrupted by a (?:new )?load request/i.test(msg) ||
    /The play\(\) request was interrupted/i.test(msg) ||
    msg === "AbortError"
  );
}

/** Browser TypeError / proxy blips from session poll — clear once connectivity returns. */
function isTransientNetworkError(msg: string | null | undefined): boolean {
  if (!msg) return false;
  return (
    /^Failed to fetch$/i.test(msg) ||
    /^NetworkError/i.test(msg) ||
    /network error/i.test(msg) ||
    /Load failed/i.test(msg) ||
    msg === "poll failed" ||
    /couldn't reach server/i.test(msg)
  );
}

type PlayerState = {
  status: SessionStatus | null;
  nowTrack: CatalogTrack | null;
  nextTrack: QueueTrack | null;
  historyTracks: QueueTrack[];
  queueTracks: QueueTrack[];
  error: string | null;
  starting: boolean;
  volume: number;
  muted: boolean;
  paused: boolean;
  skipping: boolean;
  canGoBack: boolean;
  /** Current-track like/dislike for player chrome (optimistic + API). */
  trackFeedback: "like" | "dislike" | null;
  currentTime: number;
  duration: number;
  startRadio: (seedTrackId: number) => Promise<void>;
  /**
   * Resume an active station worker when possible; otherwise start a new session
   * seeded from the station's last now-playing (or seed) track.
   */
  resumeStation: (station: Station) => Promise<void>;
  /** End the listening session (Settings / Now Playing). Not the floating transport pause. */
  stop: () => Promise<void>;
  feedback: (signal: string) => Promise<void>;
  /** Previous track in session history, or restart current from 0. */
  goBack: () => Promise<void>;
  playNext: (trackId: number) => Promise<void>;
  /** Drop a track from the Up next queue (does not stop the station). */
  removeFromQueue: (trackId: number) => Promise<void>;
  refreshMeta: (trackId: number) => Promise<CatalogTrack | null>;
  setVolume: (v: number) => void;
  toggleMute: () => void;
  togglePlayPause: () => Promise<void>;
  seekTo: (seconds: number) => void;
  getAnalyser: () => AnalyserNode | null;
};

const PlayerContext = createContext<PlayerState | null>(null);

export function PlayerProvider({ children }: { children: ReactNode }) {
  const { user, loading: authLoading } = useAuth();
  const engineRef = useRef<AudioEngine | null>(null);
  const [status, setStatus] = useState<SessionStatus | null>(null);
  const [nowTrack, setNowTrack] = useState<CatalogTrack | null>(null);
  const [historyTracks, setHistoryTracks] = useState<QueueTrack[]>([]);
  const [queueTracks, setQueueTracks] = useState<QueueTrack[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [volume, setVolumeState] = useState(0.85);
  const [muted, setMuted] = useState(false);
  const [paused, setPaused] = useState(true);
  const [skipping, setSkipping] = useState(false);
  const [trackFeedback, setTrackFeedback] = useState<"like" | "dislike" | null>(null);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const sessionIdRef = useRef<string | null>(null);
  const trackCacheRef = useRef<Map<number, CatalogTrack>>(new Map());
  /** Client-side fallback history when server has no can_go_back yet. */
  const clientHistoryRef = useRef<number[]>([]);
  const lastNowPlayingRef = useRef<number | null>(null);
  /** True while audio should hold a screen Wake Lock (play + live session). */
  const holdWakeLockRef = useRef(false);
  /** Guard against overlapping natural-end advances. */
  const advancingRef = useRef(false);
  const goBackRef = useRef<() => Promise<void>>(async () => undefined);
  const feedbackRef = useRef<(signal: string) => Promise<void>>(async () => undefined);
  const statusRef = useRef<SessionStatus | null>(null);
  statusRef.current = status;

  const reportEngineError = useCallback((msg: string) => {
    if (isBenignPlaybackError(msg)) return;
    setError(msg);
  }, []);

  /** Bind MediaSession + request Wake Lock from a user-gesture play path. */
  const armPlaybackChrome = useCallback(() => {
    bindMediaSessionHandlers({
      play: async () => {
        await engineRef.current?.play();
        setPaused(false);
        holdWakeLockRef.current = true;
        void acquireWakeLock();
      },
      pause: () => {
        engineRef.current?.pause();
        setPaused(true);
        holdWakeLockRef.current = false;
        void releaseWakeLock();
      },
      previousTrack: () => void goBackRef.current(),
      nextTrack: () => void feedbackRef.current("skip"),
    });
    holdWakeLockRef.current = true;
    void acquireWakeLock();
  }, []);

  useEffect(() => {
    void ensurePublicBaseURL();
  }, []);

  useEffect(() => {
    const eng = new AudioEngine();
    eng.setPlayStateHandler((playing) => setPaused(!playing));
    // Natural end → advance via complete (not skip). Keep same <audio> + play() for Android.
    eng.setEndedHandler(() => {
      if (advancingRef.current) return;
      const st = statusRef.current;
      if (!st || !isLiveStatus(st.status)) return;
      advancingRef.current = true;
      void feedbackRef
        .current("complete")
        .catch((e) => {
          setError(e instanceof Error ? e.message : "couldn't advance to next track");
        })
        .finally(() => {
          advancingRef.current = false;
        });
    });
    engineRef.current = eng;
    const onTime = () => {
      setCurrentTime(eng.getCurrentTime());
      setDuration(eng.getDuration());
    };
    eng.audio.addEventListener("timeupdate", onTime);
    eng.audio.addEventListener("durationchange", onTime);
    eng.audio.addEventListener("loadedmetadata", onTime);
    return () => {
      eng.audio.removeEventListener("timeupdate", onTime);
      eng.audio.removeEventListener("durationchange", onTime);
      eng.audio.removeEventListener("loadedmetadata", onTime);
      eng.setPlayStateHandler(null);
      eng.setEndedHandler(null);
      eng.destroy();
      engineRef.current = null;
      void releaseWakeLock();
    };
  }, []);

  // Reset / hydrate thumbs when the now-playing track changes.
  useEffect(() => {
    const tid = status?.now_playing?.track_id;
    if (!tid) {
      setTrackFeedback(null);
      return;
    }
    const fb = status.now_playing?.feedback;
    setTrackFeedback(fb === "like" || fb === "dislike" ? fb : null);
    // Only when track id changes — do not clobber optimistic thumbs on every poll.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.now_playing?.track_id]);

  // When the server later reports like/dislike for the current track, sync up.
  useEffect(() => {
    const fb = status?.now_playing?.feedback;
    if (fb === "like" || fb === "dislike") {
      setTrackFeedback(fb);
    }
  }, [status?.now_playing?.feedback, status?.now_playing?.track_id]);

  const refreshMeta = useCallback(async (trackId: number) => {
    try {
      const cached = trackCacheRef.current.get(trackId);
      if (cached) {
        setNowTrack(cached);
        return cached;
      }
      const t = await api.getTrack(trackId);
      trackCacheRef.current.set(trackId, t);
      setNowTrack(t);
      return t;
    } catch {
      return null;
    }
  }, []);

  // After auth/me is known: reconnect live session or hydrate last-played chrome.
  useEffect(() => {
    if (authLoading) return;
    if (!user) {
      sessionIdRef.current = null;
      sessionStorage.removeItem(SESSION_KEY);
      setStatus(null);
      setNowTrack(null);
      setHistoryTracks([]);
      setQueueTracks([]);
      setTrackFeedback(null);
      setPaused(true);
      holdWakeLockRef.current = false;
      void releaseWakeLock();
      void syncMediaSession(null, false);
      engineRef.current?.pause();
      engineRef.current?.destroyHls();
      return;
    }
    let cancelled = false;
    const userID = user.id;

    const hydrateIdleTrack = (station: Station) => {
      const t = station.now_playing ?? station.seed_track;
      if (t) {
        trackCacheRef.current.set(t.id, t);
        setNowTrack(t);
        return;
      }
      const tid = station.now_playing_id ?? station.seed_track_id;
      if (tid) void refreshMeta(tid);
    };

    void (async () => {
      // Prefer an in-tab live session; do not clobber if one is already attached.
      if (sessionIdRef.current) {
        try {
          const s = await api.getSession(sessionIdRef.current);
          if (cancelled || userID !== user.id) return;
          if (isLiveStatus(s.status)) {
            setStatus(s);
            return;
          }
          sessionStorage.removeItem(SESSION_KEY);
          sessionIdRef.current = null;
        } catch {
          sessionStorage.removeItem(SESSION_KEY);
          sessionIdRef.current = null;
        }
      }
      const saved = sessionStorage.getItem(SESSION_KEY);
      if (saved) {
        try {
          const s = await api.getSession(saved);
          if (cancelled || userID !== user.id) return;
          if (isLiveStatus(s.status)) {
            sessionIdRef.current = s.id;
            setStatus(s);
            return;
          }
          sessionStorage.removeItem(SESSION_KEY);
        } catch {
          sessionStorage.removeItem(SESSION_KEY);
        }
      }
      try {
        const { stations } = await api.listStations(12);
        if (cancelled || userID !== user.id) return;
        const live = stations.find((st) => isLiveStatus(st.status));
        if (live) {
          try {
            const s = await api.getSession(live.id);
            if (cancelled || userID !== user.id) return;
            if (isLiveStatus(s.status)) {
              sessionIdRef.current = s.id;
              sessionStorage.setItem(SESSION_KEY, s.id);
              setStatus(s);
              return;
            }
          } catch {
            /* fall through to idle hydrate */
          }
        }
        if (stations[0]) hydrateIdleTrack(stations[0]);
      } catch {
        /* ignore — leave empty idle chrome */
      }
    })();

    return () => {
      cancelled = true;
    };
    // Intentionally only when auth identity settles — not on every refreshMeta identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authLoading, user?.id]);

  useEffect(() => {
    const id = status?.id;
    if (!id || status?.status === "stopped") return;
    let cancelled = false;
    let failStreak = 0;
    const tick = async () => {
      try {
        const s = await api.getSession(id);
        if (cancelled) return;
        failStreak = 0;
        setStatus(s);
        // Poll used to leave "Failed to fetch" stuck after brief outages (rebuilds, 502s).
        setError((prev) => (isTransientNetworkError(prev) ? null : prev));
      } catch (e) {
        if (cancelled) return;
        const msg = e instanceof Error ? e.message : "poll failed";
        if (msg === "AbortError" || /aborted/i.test(msg)) return;
        failStreak += 1;
        // Ignore a single blip (HTTP/2 cancel / momentary proxy hiccup).
        if (failStreak < 2) return;
        setError(isTransientNetworkError(msg) ? "couldn't reach server" : msg);
      }
    };
    const t = window.setInterval(() => void tick(), 2000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, [status?.id, status?.status]);

  useEffect(() => {
    if (!status?.now_playing?.track_id) {
      // Keep last nowTrack for idle player chrome when stopped / no session.
      if (status != null && isLiveStatus(status.status)) {
        setNowTrack(null);
        lastNowPlayingRef.current = null;
      }
      return;
    }
    const tid = status.now_playing.track_id;
    const prev = lastNowPlayingRef.current;
    if (prev != null && prev !== tid && isLiveStatus(status.status)) {
      clientHistoryRef.current.push(prev);
      if (clientHistoryRef.current.length > 40) {
        clientHistoryRef.current = clientHistoryRef.current.slice(-40);
      }
    }
    lastNowPlayingRef.current = tid;
    void refreshMeta(tid);
  }, [status?.now_playing?.track_id, status?.status, refreshMeta]);

  const resolveTrackList = useCallback(
    async (
      refs: {
        track_id: number;
        source?: string;
        feedback?: string;
        play_count?: number;
      }[],
    ) => {
      const resolved: QueueTrack[] = [];
      for (const q of refs) {
        let track = trackCacheRef.current.get(q.track_id);
        if (!track) {
          try {
            track = await api.getTrack(q.track_id);
            trackCacheRef.current.set(q.track_id, track);
          } catch {
            track = {
              id: q.track_id,
              title: `Track #${q.track_id}`,
              artist: "",
              album: "",
            };
          }
        }
        resolved.push({
          ...track,
          source: q.source,
          feedback: q.feedback ?? track.feedback,
          play_count: q.play_count ?? track.play_count,
        });
      }
      return resolved;
    },
    [],
  );

  const historyKey =
    !status || status.status === "stopped"
      ? ""
      : (status.history ?? [])
          .map((q) => `${q.track_id}:${q.feedback ?? ""}:${q.play_count ?? 0}`)
          .join(",");

  const queueKey =
    !status || status.status === "stopped"
      ? ""
      : (status.queue ?? [])
          .map((q) => `${q.track_id}:${q.source ?? ""}:${q.feedback ?? ""}:${q.play_count ?? 0}`)
          .join(",");

  useEffect(() => {
    if (!historyKey) {
      setHistoryTracks([]);
      return;
    }
    const history = status?.history ?? [];
    let cancelled = false;
    void resolveTrackList(history).then((resolved) => {
      if (!cancelled) setHistoryTracks(resolved);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [historyKey, resolveTrackList]);

  useEffect(() => {
    if (!queueKey) {
      setQueueTracks([]);
      return;
    }
    const queue = status?.queue ?? [];
    let cancelled = false;
    void resolveTrackList(queue).then((resolved) => {
      if (!cancelled) setQueueTracks(resolved);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queueKey, resolveTrackList]);

  useEffect(() => {
    if (!status || status.status === "stopped") return;
    const eng = engineRef.current;
    if (!eng) return;
    const trackId = status.now_playing?.track_id;
    void eng.attach({
      hlsUrl: status.hls_url,
      progressiveUrl: status.progressive_url,
      trackId,
      preferProgressive: true,
      onError: reportEngineError,
    });
  }, [
    status?.id,
    status?.hls_url,
    status?.progressive_url,
    status?.now_playing?.track_id,
    status?.status,
    reportEngineError,
  ]);

  // Prefetch next progressive URL so skip can start from a warm cache.
  useEffect(() => {
    const nextId = status?.queue?.[0]?.track_id;
    if (!nextId || status?.status === "stopped") return;
    engineRef.current?.prefetchProgressive(`/api/v1/tracks/${nextId}/stream`);
  }, [status?.queue, status?.status]);

  const startRadio = useCallback(
    async (seedTrackId: number) => {
      setStarting(true);
      setError(null);
      clientHistoryRef.current = [];
      lastNowPlayingRef.current = null;
      armPlaybackChrome();
      try {
        if (sessionIdRef.current) {
          try {
            await api.stopSession(sessionIdRef.current);
          } catch {
            /* ignore */
          }
        }
        const s = await api.createSession(seedTrackId);
        sessionIdRef.current = s.id;
        sessionStorage.setItem(SESSION_KEY, s.id);
        setStatus(s);
        setPaused(false);
      } catch (e) {
        setError(e instanceof Error ? e.message : "failed to start");
        holdWakeLockRef.current = false;
        void releaseWakeLock();
        throw e;
      } finally {
        setStarting(false);
      }
    },
    [armPlaybackChrome],
  );

  const resumeStation = useCallback(
    async (station: Station) => {
      if (isLiveStatus(station.status)) {
        try {
          const s = await api.getSession(station.id);
          if (isLiveStatus(s.status)) {
            armPlaybackChrome();
            sessionIdRef.current = s.id;
            sessionStorage.setItem(SESSION_KEY, s.id);
            setStatus(s);
            setPaused(false);
            setError(null);
            return;
          }
        } catch {
          /* start a fresh station from last track */
        }
      }
      const seed =
        station.now_playing?.id ??
        station.now_playing_id ??
        station.seed_track?.id ??
        station.seed_track_id;
      if (!seed) throw new Error("station has no seed track");
      await startRadio(seed);
    },
    [startRadio, armPlaybackChrome],
  );

  const stop = useCallback(async () => {
    const id = sessionIdRef.current || status?.id;
    if (!id) return;
    try {
      const s = await api.stopSession(id);
      setStatus(s);
    } finally {
      sessionIdRef.current = null;
      sessionStorage.removeItem(SESSION_KEY);
      clientHistoryRef.current = [];
      // Keep nowTrack / last now-playing for idle chrome ("pick up where you left off").
      engineRef.current?.pause();
      engineRef.current?.destroyHls();
      setPaused(true);
      holdWakeLockRef.current = false;
      void releaseWakeLock();
      void syncMediaSession(null, false);
    }
  }, [status?.id]);

  const feedback = useCallback(
    async (signal: string) => {
      const id = sessionIdRef.current || status?.id;
      if (!id) throw new Error("no active session");
      const isSkip = signal === "skip";
      const isAdvance = isSkip || signal === "complete" || signal === "dislike";
      if (isSkip || signal === "dislike") setSkipping(true);
      if (signal === "like" || signal === "dislike") {
        setTrackFeedback(signal);
      }
      let didOptimisticAttach = false;
      try {
        // Optimistic: flip to queue head + attach same <audio> before round-trip.
        // Critical for Android natural-end (complete) so play() stays in media continuity.
        if (isAdvance && status?.queue?.[0]?.track_id) {
          const nextId = status.queue[0].track_id;
          const rest = status.queue.slice(1);
          const leaving = status.now_playing?.track_id;
          const prevHistory = status.history ?? [];
          const history =
            leaving && leaving > 0
              ? [...prevHistory, { track_id: leaving }].slice(-8)
              : prevHistory;
          const progressiveUrl = `/api/v1/tracks/${nextId}/stream`;
          setError(null);
          setStatus({
            ...status,
            now_playing: { track_id: nextId },
            history,
            queue: rest,
            progressive_url: progressiveUrl,
            can_go_back: true,
            last_feedback: {
              signal,
              track_id: status.now_playing?.track_id ?? 0,
              at: new Date().toISOString(),
            },
          });
          const cached = trackCacheRef.current.get(nextId);
          if (cached) setNowTrack(cached);
          setPaused(false);
          holdWakeLockRef.current = true;
          void acquireWakeLock();
          didOptimisticAttach = true;
          void engineRef.current?.attach({
            progressiveUrl,
            hlsUrl: status.hls_url,
            trackId: nextId,
            preferProgressive: true,
            onError: reportEngineError,
          });
        } else if (signal === "complete" && (!status?.queue || status.queue.length === 0)) {
          setError("couldn't advance — queue empty; waiting for refill");
        }
        const s = await api.feedback(id, signal);
        setStatus(s);
        if (signal === "like") {
          const fb = s.now_playing?.feedback;
          if (fb === "like" || fb === "dislike") setTrackFeedback(fb);
          else setTrackFeedback("like");
        } else if (signal === "dislike") {
          // Dislike advances off the current track — thumbs reflect the new now-playing.
          const fb = s.now_playing?.feedback;
          setTrackFeedback(fb === "like" || fb === "dislike" ? fb : null);
        }
        // Server advanced when client had no queue head yet — attach now.
        if (
          (signal === "complete" || signal === "dislike") &&
          !didOptimisticAttach &&
          s.now_playing?.track_id
        ) {
          const tid = s.now_playing.track_id;
          const progressiveUrl = s.progressive_url || `/api/v1/tracks/${tid}/stream`;
          setError(null);
          setPaused(false);
          holdWakeLockRef.current = true;
          void acquireWakeLock();
          void engineRef.current?.attach({
            progressiveUrl,
            hlsUrl: s.hls_url,
            trackId: tid,
            preferProgressive: true,
            onError: reportEngineError,
          });
        }
      } catch (e) {
        const msg = e instanceof Error ? e.message : "feedback failed";
        setError(
          signal === "complete"
            ? `couldn't advance to next track: ${msg}`
            : msg,
        );
        try {
          const fresh = await api.getSession(id);
          setStatus(fresh);
          const fb = fresh.now_playing?.feedback;
          setTrackFeedback(fb === "like" || fb === "dislike" ? fb : null);
        } catch {
          /* ignore */
        }
        throw e;
      } finally {
        if (isSkip || signal === "dislike") setSkipping(false);
      }
    },
    [status, reportEngineError],
  );

  const goBack = useCallback(async () => {
    const id = sessionIdRef.current || status?.id;
    if (!id || !status || status.status === "stopped") return;

    try {
      const s = await api.sessionBack(id);
      setStatus(s);
      return;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        await engineRef.current?.restartFromStart();
        return;
      }
      // Older backends without /back: client history, else restart.
      if (e instanceof ApiError && e.status === 404) {
        const prev = clientHistoryRef.current.pop();
        if (prev && prev > 0) {
          const current = status.now_playing?.track_id;
          const rest = (status.queue ?? []).filter((q) => q.track_id !== prev);
          const queue = current
            ? [{ track_id: current }, ...rest.filter((q) => q.track_id !== current)]
            : rest;
          const hist = (status.history ?? []).filter((q) => q.track_id !== prev);
          setStatus({
            ...status,
            now_playing: { track_id: prev },
            history: hist,
            queue,
            progressive_url: `/api/v1/tracks/${prev}/stream`,
            can_go_back: clientHistoryRef.current.length > 0,
          });
          void engineRef.current?.attach({
            progressiveUrl: `/api/v1/tracks/${prev}/stream`,
            hlsUrl: status.hls_url,
            trackId: prev,
            preferProgressive: true,
            onError: reportEngineError,
          });
          return;
        }
        await engineRef.current?.restartFromStart();
        return;
      }
      setError(e instanceof Error ? e.message : "back failed");
    }
  }, [status, reportEngineError]);

  const playNext = useCallback(
    async (trackId: number) => {
      const id = sessionIdRef.current || status?.id;
      if (!id || status?.status === "stopped") {
        await startRadio(trackId);
        return;
      }
      setError(null);
      const s = await api.injectQueue(id, trackId, "next");
      setStatus(s);
    },
    [status?.id, status?.status, startRadio],
  );

  const removeFromQueue = useCallback(
    async (trackId: number) => {
      const id = sessionIdRef.current || status?.id;
      if (!id || status?.status === "stopped") {
        throw new Error("no active session");
      }
      setError(null);
      const s = await api.removeFromQueue(id, trackId);
      setStatus(s);
    },
    [status?.id, status?.status],
  );

  const nextTrack = queueTracks[0] ?? null;
  const canGoBack = !!(status?.can_go_back || clientHistoryRef.current.length > 0);

  const setVolume = useCallback((v: number) => {
    engineRef.current?.setVolume(v);
    setVolumeState(engineRef.current?.getVolume() ?? v);
    setMuted(engineRef.current?.isMuted() ?? false);
  }, []);

  const toggleMute = useCallback(() => {
    const eng = engineRef.current;
    if (!eng) return;
    eng.toggleMute();
    setMuted(eng.isMuted());
    setVolumeState(eng.getVolume());
  }, []);

  const togglePlayPause = useCallback(async () => {
    const eng = engineRef.current;
    if (!eng) return;
    if (eng.isPaused()) {
      armPlaybackChrome();
    }
    const playing = await eng.togglePlayPause();
    if (typeof playing === "boolean") {
      setPaused(!playing);
      holdWakeLockRef.current = playing;
      if (playing) void acquireWakeLock();
      else void releaseWakeLock();
    }
  }, [armPlaybackChrome]);

  const getAnalyser = useCallback(() => engineRef.current?.getAnalyser() ?? null, []);

  const seekTo = useCallback((seconds: number) => {
    engineRef.current?.seekTo(seconds);
    setCurrentTime(engineRef.current?.getCurrentTime() ?? seconds);
  }, []);

  goBackRef.current = goBack;
  feedbackRef.current = feedback;

  // Release Wake Lock when the live session ends or user pauses (battery).
  useEffect(() => {
    const live = !!status && isLiveStatus(status.status);
    if (!live || paused) {
      holdWakeLockRef.current = false;
      void releaseWakeLock();
    }
  }, [paused, status?.status]);

  // Re-acquire on visibility restore while still playing (user previously armed play).
  useEffect(() => {
    const onVis = () => {
      if (document.visibilityState === "visible" && holdWakeLockRef.current) {
        void acquireWakeLock();
      }
    };
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
  }, []);

  useEffect(() => {
    const playing = !paused && !!status && isLiveStatus(status.status);
    void syncMediaSession(nowTrack, playing);
  }, [nowTrack, paused, status?.status, status?.now_playing?.track_id]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.code !== "Space" && e.key !== " ") return;
      const t = e.target as HTMLElement | null;
      if (
        t &&
        (t.tagName === "INPUT" ||
          t.tagName === "TEXTAREA" ||
          t.tagName === "SELECT" ||
          t.isContentEditable)
      ) {
        return;
      }
      const active = status && status.status !== "stopped";
      if (!active) return;
      e.preventDefault();
      void togglePlayPause();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [status, togglePlayPause]);

  const value = useMemo(
    () => ({
      status,
      nowTrack,
      nextTrack,
      historyTracks,
      queueTracks,
      error,
      starting,
      volume,
      muted,
      paused,
      skipping,
      canGoBack,
      trackFeedback,
      currentTime,
      duration,
      startRadio,
      resumeStation,
      stop,
      feedback,
      goBack,
      playNext,
      removeFromQueue,
      refreshMeta,
      setVolume,
      toggleMute,
      togglePlayPause,
      seekTo,
      getAnalyser,
    }),
    [
      status,
      nowTrack,
      nextTrack,
      historyTracks,
      queueTracks,
      error,
      starting,
      volume,
      muted,
      paused,
      skipping,
      canGoBack,
      trackFeedback,
      currentTime,
      duration,
      startRadio,
      resumeStation,
      stop,
      feedback,
      goBack,
      playNext,
      removeFromQueue,
      refreshMeta,
      setVolume,
      toggleMute,
      togglePlayPause,
      seekTo,
      getAnalyser,
    ],
  );

  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>;
}

export function usePlayer() {
  const ctx = useContext(PlayerContext);
  if (!ctx) throw new Error("usePlayer outside provider");
  return ctx;
}

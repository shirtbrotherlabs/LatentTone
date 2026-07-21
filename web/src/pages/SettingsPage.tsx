/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 * Last-Modified: 2026-07-20
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type {
  DuplicateGroup,
  EmbedStatus,
  RadioPrefs,
  ScanSchedule,
  ScanStatus,
  StreamPrefs,
} from "../api/types";
import { useAuth } from "../auth/AuthContext";
import { usePlayer } from "../player/PlayerContext";

type ReadySample = { t: number; ready: number };

/** Human duration for ETA copy (e.g. "about 2h 15m"). */
function formatEta(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "";
  if (seconds < 45) return "less than a minute";
  if (seconds < 3600) {
    const m = Math.max(1, Math.round(seconds / 60));
    return m === 1 ? "about 1 minute" : `about ${m} minutes`;
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  if (m <= 0) return h === 1 ? "about 1 hour" : `about ${h} hours`;
  if (h >= 10) return `about ${h} hours`;
  return `about ${h}h ${m}m`;
}

function estimateIdentityEta(
  samples: ReadySample[],
  remaining: number,
  running: boolean,
): string | null {
  if (!running || remaining <= 0) return null;
  if (samples.length < 2) return "estimating…";
  const first = samples[0];
  const last = samples[samples.length - 1];
  const dt = (last.t - first.t) / 1000;
  const dr = last.ready - first.ready;
  if (dt < 4 || dr < 1) return "estimating…";
  const rate = dr / dt; // tracks ready per second
  if (rate <= 0) return "estimating…";
  return formatEta(remaining / rate);
}

type RadioToggleKey = keyof Pick<
  RadioPrefs,
  "radio_bridge" | "artist_cooldown" | "query_jitter" | "artist_penalty" | "bounded_random"
>;

const RADIO_TOGGLES: { key: RadioToggleKey; label: string; hint: string }[] = [
  {
    key: "radio_bridge",
    label: "Radio Bridge",
    hint: "Every 5–7 songs, jump via a liked track into a new neighborhood (default on).",
  },
  {
    key: "artist_cooldown",
    label: "Artist cooldown",
    hint: "Avoid repeating artists from the last few plays when alternatives exist.",
  },
  {
    key: "query_jitter",
    label: "Query jitter",
    hint: "Add slight noise to the search vector so ANN does not stick in one pocket.",
  },
  {
    key: "artist_penalty",
    label: "Artist penalty",
    hint: "Temporarily down-rank a just-played artist; decays over the next tracks.",
  },
  {
    key: "bounded_random",
    label: "Bounded random",
    hint: "Weighted pick among top neighbors instead of always taking #1.",
  },
];

const STREAM_FORMATS: { value: StreamPrefs["stream_format"]; label: string }[] = [
  { value: "original", label: "Original" },
  { value: "mp3", label: "MP3" },
  { value: "aac", label: "AAC" },
  { value: "opus", label: "Opus" },
];

export function SettingsPage() {
  const { user } = useAuth();
  const { status, stop } = usePlayer();
  const isAdmin = !!user?.is_admin;
  const [scan, setScan] = useState<ScanStatus | null>(null);
  const [schedule, setSchedule] = useState<ScanSchedule | null>(null);
  const [scheduleHours, setScheduleHours] = useState("24");
  const [embed, setEmbed] = useState<EmbedStatus | null>(null);
  const [radio, setRadio] = useState<RadioPrefs | null>(null);
  const [stream, setStream] = useState<StreamPrefs | null>(null);
  const [dupes, setDupes] = useState<DuplicateGroup[] | null>(null);
  const [dupesRule, setDupesRule] = useState("");
  const [dupesBusy, setDupesBusy] = useState(false);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [passwordMsg, setPasswordMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState<
    | "scan"
    | "schedule"
    | "embed-start"
    | "embed-stop"
    | "radio"
    | "stream"
    | "end-station"
    | "password"
    | null
  >(null);
  const [error, setError] = useState<string | null>(null);
  const [identityEta, setIdentityEta] = useState<string | null>(null);
  const readySamples = useRef<ReadySample[]>([]);

  const refresh = useCallback(async () => {
    try {
      const [scanStatus, embedStatus, sched] = await Promise.all([
        api.scanStatus(),
        api.embedStatus(),
        api.getScanSchedule(),
      ]);
      setScan(scanStatus);
      setEmbed(embedStatus);
      setSchedule(sched);
      if (sched?.interval_seconds) {
        setScheduleHours(String(Math.max(1, Math.round(sched.interval_seconds / 3600))));
      }
      if (embedStatus?.running) {
        const next = [
          ...readySamples.current,
          { t: Date.now(), ready: embedStatus.ready },
        ].slice(-12);
        readySamples.current = next;
        const left = Math.max(0, embedStatus.pending + embedStatus.processing);
        setIdentityEta(estimateIdentityEta(next, left, true));
      } else {
        readySamples.current = [];
        setIdentityEta(null);
      }
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "status failed");
    }
  }, []);

  const loadRadio = useCallback(async () => {
    if (!user) {
      setRadio(null);
      return;
    }
    try {
      setRadio(await api.getRadioPrefs());
    } catch (e) {
      setError(e instanceof Error ? e.message : "radio prefs failed");
    }
  }, [user]);

  const loadStream = useCallback(async () => {
    if (!user) {
      setStream(null);
      return;
    }
    try {
      setStream(await api.getStreamPrefs());
    } catch (e) {
      setError(e instanceof Error ? e.message : "stream prefs failed");
    }
  }, [user]);

  useEffect(() => {
    void refresh();
    const t = window.setInterval(() => void refresh(), 2500);
    return () => window.clearInterval(t);
  }, [refresh]);

  useEffect(() => {
    void loadRadio();
  }, [loadRadio]);

  useEffect(() => {
    void loadStream();
  }, [loadStream]);

  const startScan = async (force = false) => {
    setBusy("scan");
    setError(null);
    try {
      await api.scanStart(force);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "scan start failed");
    } finally {
      setBusy(null);
    }
  };

  const saveSchedule = async (patch: { enabled?: boolean; interval_seconds?: number }) => {
    setBusy("schedule");
    setError(null);
    try {
      const next = await api.patchScanSchedule(patch);
      setSchedule(next);
      if (next.interval_seconds) {
        setScheduleHours(String(Math.max(1, Math.round(next.interval_seconds / 3600))));
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "schedule update failed");
    } finally {
      setBusy(null);
    }
  };

  const startEmbed = async () => {
    setBusy("embed-start");
    setError(null);
    try {
      await api.embedStart();
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "embed start failed");
    } finally {
      setBusy(null);
    }
  };

  const stopEmbed = async () => {
    setBusy("embed-stop");
    setError(null);
    try {
      await api.embedStop();
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "embed stop failed");
    } finally {
      setBusy(null);
    }
  };

  const endStation = async () => {
    setBusy("end-station");
    setError(null);
    try {
      await stop();
    } catch (e) {
      setError(e instanceof Error ? e.message : "end station failed");
    } finally {
      setBusy(null);
    }
  };

  const patchRadio = async (patch: Partial<RadioPrefs>) => {
    setBusy("radio");
    setError(null);
    try {
      setRadio(await api.patchRadioPrefs(patch));
    } catch (e) {
      setError(e instanceof Error ? e.message : "radio prefs save failed");
      await loadRadio();
    } finally {
      setBusy(null);
    }
  };

  const patchStream = async (patch: Partial<StreamPrefs>) => {
    setBusy("stream");
    setError(null);
    try {
      setStream(await api.patchStreamPrefs(patch));
    } catch (e) {
      setError(e instanceof Error ? e.message : "stream prefs save failed");
      await loadStream();
    } finally {
      setBusy(null);
    }
  };

  const changePassword = async () => {
    setBusy("password");
    setError(null);
    setPasswordMsg(null);
    try {
      await api.changePassword(currentPassword, newPassword);
      setCurrentPassword("");
      setNewPassword("");
      setPasswordMsg("Password updated.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "password change failed");
    } finally {
      setBusy(null);
    }
  };

  const artists = scan?.artists ?? 0;
  const albums = scan?.albums ?? 0;
  const tracks = scan?.tracks ?? embed?.catalog_tracks ?? 0;
  const identityReady = embed?.ready ?? 0;
  const identityTotal = Math.max(tracks, embed?.catalog_tracks ?? 0, identityReady);
  const identityLeft = Math.max(0, (embed?.pending ?? 0) + (embed?.processing ?? 0));
  const identityPct =
    identityTotal > 0 ? Math.min(100, Math.round((100 * identityReady) / identityTotal)) : 0;

  return (
    <section>
      <h1 className="page-title">Settings</h1>
      <p className="page-lead">Account, stream defaults, Radio preferences, and library jobs.</p>

      <div className="settings-grid">
        <div className="tile">
          <h3>Signed in</h3>
          <p>Username: {user?.username}</p>
          <p>User id: {user?.id}</p>
          <p>Role: {isAdmin ? "admin" : "user"}</p>
          <p>
            Listening session:{" "}
            {status && status.status !== "stopped" ? status.status : "none"}
          </p>
          {status && status.status !== "stopped" ? (
            <div className="toolbar" style={{ marginTop: "0.75rem" }}>
              <button
                type="button"
                className="btn btn-danger"
                disabled={busy === "end-station"}
                onClick={() => void endStation()}
              >
                {busy === "end-station" ? "Ending…" : "End station"}
              </button>
            </div>
          ) : null}
          <form
            className="settings-stream"
            style={{ marginTop: "1rem" }}
            onSubmit={(e) => {
              e.preventDefault();
              void changePassword();
            }}
          >
            <h4 style={{ margin: 0 }}>Change password</h4>
            <label>
              Current password
              <input
                type="password"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
                disabled={busy === "password"}
              />
            </label>
            <label>
              New password
              <input
                type="password"
                autoComplete="new-password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                disabled={busy === "password"}
                minLength={8}
              />
            </label>
            <button
              type="submit"
              className="btn"
              disabled={busy === "password" || !currentPassword || newPassword.length < 8}
            >
              {busy === "password" ? "Saving…" : "Update password"}
            </button>
            {passwordMsg ? <p className="muted">{passwordMsg}</p> : null}
          </form>
        </div>

        <div className="tile settings-stream-tile">
          <h3>Stream defaults</h3>
          <p className="muted">
            Progressive and HLS encode target. Default is original (no transcode) when the
            format is browser-safe; unsafe containers auto-fall back to MP3. Opus progressive
            uses Ogg/Opus; HLS fallback stays AAC.
          </p>
          {stream ? (
            <div className="settings-stream">
              <label>
                Format
                <select
                  value={stream.stream_format}
                  disabled={busy === "stream"}
                  onChange={(e) => void patchStream({ stream_format: e.target.value })}
                >
                  {STREAM_FORMATS.map((f) => (
                    <option key={f.value} value={f.value}>
                      {f.label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Bitrate (kbps)
                <input
                  type="number"
                  min={64}
                  max={320}
                  step={32}
                  value={stream.bitrate_kbps}
                  disabled={busy === "stream"}
                  onChange={(e) => void patchStream({ bitrate_kbps: Number(e.target.value) })}
                />
              </label>
            </div>
          ) : (
            <p className="muted" style={{ marginTop: "0.75rem" }}>
              {user ? "Loading stream prefs…" : "Sign in to edit stream defaults."}
            </p>
          )}
        </div>

        <div className="tile settings-radio-tile">
          <h3>Radio preferences</h3>
          <p className="muted">
            Shape endless Radio so nearby ANN neighbors do not loop the same artist. Defaults
            keep diversification on.
          </p>
          {radio ? (
            <ul className="settings-check-list">
              {RADIO_TOGGLES.map((row) => (
                <li key={row.key}>
                  <label className="settings-check">
                    <input
                      type="checkbox"
                      checked={radio[row.key]}
                      disabled={busy === "radio"}
                      onChange={(e) => void patchRadio({ [row.key]: e.target.checked })}
                    />
                    <span>
                      <strong>{row.label}</strong>
                      <span className="muted">{row.hint}</span>
                    </span>
                  </label>
                </li>
              ))}
              <li>
                <label className="settings-check settings-check-alpha">
                  <span>
                    <strong>Jitter strength (α)</strong>
                    <span className="muted">
                      Query noise scale when jitter is enabled (default 0.05).
                    </span>
                  </span>
                  <input
                    type="range"
                    min={0.01}
                    max={0.25}
                    step={0.01}
                    value={radio.jitter_alpha}
                    disabled={busy === "radio" || !radio.query_jitter}
                    onChange={(e) =>
                      void patchRadio({ jitter_alpha: Number(e.target.value) })
                    }
                  />
                  <span className="settings-alpha-val">{radio.jitter_alpha.toFixed(2)}</span>
                </label>
              </li>
            </ul>
          ) : (
            <p className="muted" style={{ marginTop: "0.75rem" }}>
              {user ? "Loading preferences…" : "Sign in to edit Radio preferences."}
            </p>
          )}
        </div>

        <div className="tile">
          <h3>Library</h3>
          <p className="library-summary">
            <strong>{tracks.toLocaleString()}</strong> tracks
            <span className="muted">
              {" "}
              · {artists.toLocaleString()} artists · {albums.toLocaleString()} albums
            </span>
          </p>
          {embed ? (
            <div className="identity-summary">
              <div className="identity-summary-head">
                <span>Acoustic identity</span>
                <span className="muted">{identityPct}%</span>
              </div>
              <div
                className="identity-bar"
                role="progressbar"
                aria-valuenow={identityPct}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label="Acoustic identity progress"
              >
                <div className="identity-bar-fill" style={{ width: `${identityPct}%` }} />
              </div>
              <p className="muted identity-summary-detail">
                {identityReady.toLocaleString()} of {identityTotal.toLocaleString()} tracks ready
                {identityLeft > 0 ? ` · ${identityLeft.toLocaleString()} left` : ""}
              </p>
              {embed.running ? (
                <p className="identity-eta">
                  Scanning now
                  {identityEta && identityEta !== "estimating…"
                    ? ` — ${identityEta} left`
                    : identityEta
                      ? ` — ${identityEta}`
                      : ""}
                </p>
              ) : identityLeft > 0 ? (
                <p className="muted identity-eta">Not finished — start an acoustic scan to continue.</p>
              ) : identityTotal > 0 ? (
                <p className="identity-eta identity-eta-done">Complete</p>
              ) : null}
            </div>
          ) : (
            <p className="muted" style={{ marginTop: "0.75rem" }}>
              Loading library status…
            </p>
          )}
        </div>

        <div className="tile">
          <h3>Library scan</h3>
          <p className="muted">
            Catalog / metadata reconcile over the mounted library.
            {scan?.running ? " · running" : " · idle"}
            {!scan?.running && scan?.last?.includes("upserted=0")
              ? " · unchanged files are skipped (use Force rescan to re-read tags)"
              : ""}
          </p>
          {scan?.last ? <p className="muted">Last: {scan.last}</p> : null}
          {schedule ? (
            <p className="muted">
              Schedule:{" "}
              {schedule.enabled
                ? `every ${Math.max(1, Math.round(schedule.interval_seconds / 3600))}h`
                : "disabled"}
              {schedule.next_run_at ? ` · next ${schedule.next_run_at}` : ""}
            </p>
          ) : null}
          {isAdmin ? (
            <>
              <div className="toolbar" style={{ marginTop: "0.75rem" }}>
                <button
                  type="button"
                  className="btn"
                  disabled={!!busy || !!scan?.running}
                  onClick={() => void startScan(false)}
                >
                  {busy === "scan" || scan?.running ? "Scanning…" : "Start scan"}
                </button>
                <button
                  type="button"
                  className="btn"
                  disabled={!!busy || !!scan?.running}
                  onClick={() => void startScan(true)}
                  title="Re-read tags even when file mtime/size unchanged"
                >
                  Force rescan
                </button>
              </div>
              <div className="toolbar" style={{ marginTop: "0.75rem", flexWrap: "wrap", gap: "0.5rem" }}>
                <label className="muted" style={{ display: "flex", alignItems: "center", gap: "0.4rem" }}>
                  <input
                    type="checkbox"
                    checked={!!schedule?.enabled}
                    disabled={!!busy || !schedule}
                    onChange={(e) => void saveSchedule({ enabled: e.target.checked })}
                  />
                  Periodic scan
                </label>
                <label className="muted" style={{ display: "flex", alignItems: "center", gap: "0.4rem" }}>
                  Every
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={scheduleHours}
                    disabled={!!busy || !schedule?.enabled}
                    style={{ width: "4rem" }}
                    onChange={(e) => setScheduleHours(e.target.value)}
                  />
                  hours
                </label>
                <button
                  type="button"
                  className="btn"
                  disabled={!!busy || !schedule?.enabled}
                  onClick={() => {
                    const h = Math.max(1, Math.round(Number(scheduleHours) || 24));
                    void saveSchedule({ interval_seconds: h * 3600 });
                  }}
                >
                  {busy === "schedule" ? "Saving…" : "Save interval"}
                </button>
              </div>
            </>
          ) : (
            <p className="muted" style={{ marginTop: "0.75rem" }}>
              Status only — an admin must start or schedule library scans.
            </p>
          )}
        </div>

        <div className="tile">
          <h3>Scan acoustic identity</h3>
          <p className="muted">
            Build the listening fingerprint for each track
            {embed?.running
              ? identityEta && identityEta !== "estimating…"
                ? ` · in progress, ${identityEta} left`
                : " · in progress"
              : identityLeft > 0
                ? ` · ${identityLeft.toLocaleString()} tracks still need a scan`
                : " · idle"}
          </p>
          {embed?.last && !embed.running ? <p className="muted">Last: {embed.last}</p> : null}
          {isAdmin ? (
            <div className="toolbar" style={{ marginTop: "0.75rem" }}>
              <button
                type="button"
                className="btn"
                disabled={!!busy || !!embed?.running}
                onClick={() => void startEmbed()}
              >
                {busy === "embed-start" || embed?.running ? "Embedding…" : "Start acoustic scan"}
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={!!busy || !embed?.running}
                onClick={() => void stopEmbed()}
              >
                {busy === "embed-stop" ? "Stopping…" : "Stop acoustic scan"}
              </button>
            </div>
          ) : (
            <p className="muted" style={{ marginTop: "0.75rem" }}>
              Status only — an admin must start or stop acoustic scans.
            </p>
          )}
        </div>
      </div>

      {error ? <p className="error">{error}</p> : null}

      <div className="tile" style={{ marginTop: "1rem" }}>
        <h2 className="tile-title">Possible duplicates</h2>
        <p className="muted" style={{ marginTop: 0 }}>
          Groups where title, album, and artist match after ignoring capitalization/punctuation,
          and durations are within 1 second. Acoustic embeddings are not used.
        </p>
        <button
          type="button"
          className="btn"
          disabled={dupesBusy}
          onClick={() => {
            setDupesBusy(true);
            setError(null);
            void api
              .listDuplicates(100)
              .then((r) => {
                setDupes(r.groups);
                setDupesRule(r.rule);
              })
              .catch((e) => setError(e instanceof Error ? e.message : "failed"))
              .finally(() => setDupesBusy(false));
          }}
        >
          {dupesBusy ? "Scanning…" : dupes ? "Refresh" : "Find duplicates"}
        </button>
        {dupesRule ? (
          <p className="muted" style={{ fontSize: "0.85rem" }}>
            Rule: {dupesRule}
          </p>
        ) : null}
        {dupes && dupes.length === 0 ? <p className="muted">No duplicate groups found.</p> : null}
        {dupes?.map((g) => (
          <div key={`${g.artist}|${g.album}|${g.title}|${g.duration_ms}`} className="dup-group">
            <strong>
              {g.artist} — {g.title}
            </strong>
            <div className="muted">
              {g.album} · {g.count} files · ~{Math.round(g.duration_ms / 1000)}s
            </div>
            <ul>
              {g.tracks.map((t) => (
                <li key={t.id}>
                  <Link to={`/library/tracks/${t.id}`}>#{t.id}</Link>{" "}
                  {(t as { path?: string }).path || t.title}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}

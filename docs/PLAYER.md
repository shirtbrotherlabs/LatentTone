# Product player notes (Phase 4)

## Continuity path

1. Progressive first: `GET /api/v1/tracks/{id}/stream` for near-instant track switches.
2. Fallback: **Hls.js** against session `hls_url` (`/api/v1/sessions/{id}/hls/index.m3u8`) when progressive fails or is unavailable (`credentials: same-origin` / `xhr.withCredentials`).
3. Optional **Web Audio** `GainNode` soft fade on attach when `AudioContext` + `createMediaElementSource` succeed; otherwise plain `<audio>` playback.

Safari may use native HLS on `<audio>` when MSE/Hls.js is unavailable.

## Stream encode prefs (`/api/v1/me/stream-prefs`)

Per-user defaults persisted in SQLite (`user_stream_prefs`):

| `stream_format` | Progressive behavior | HLS audio |
|-----------------|----------------------|-----------|
| `original` (default) | Serve file bytes when the container is browser-safe (`mp3`, `m4a`/`aac`, `flac`, `ogg`/`opus`, `wav`, …). Auto-transcode **MP3** for unsafe containers (`wma`, `ape`, …). | AAC @ bitrate |
| `mp3` | Always FFmpeg → MP3 @ `bitrate_kbps` | MP3 @ bitrate |
| `aac` | Always FFmpeg → AAC/ADTS @ `bitrate_kbps` | AAC @ bitrate |

Default `bitrate_kbps` is **192** (clamped 64–320). Edit in Settings → Stream defaults.

## Android / lock-screen chrome

* **Screen Wake Lock** — requested on user-gesture play / start / resume; released on pause, stop, or end station; re-acquired on `visibilitychange` when still playing. Soft-fails when unsupported.
* **Wake Lock requires a secure context** — HTTPS (or `localhost`). Behind a reverse proxy, terminate TLS at the edge and set `PUBLIC_BASE_URL` to that HTTPS origin (see README).
* **Media Session** — `navigator.mediaSession` metadata from now-playing (title, artist, album, absolute artwork via `public_base_url`); action handlers for play, pause, previoustrack, nexttrack. Soft-fails when unsupported.
* **Natural end → next** — when `<audio>` fires `ended`, the SPA posts feedback signal `complete` (advance without skip penalty), keeps the same media element, sets a same-origin relative `/api/v1/tracks/{id}/stream` src, and calls `play()` immediately for Android autoplay continuity. Prefetch uses a tiny Range `fetch`, not a second `<audio>` (avoids opaque “couldn't fetch” failures).

Runtime config: `GET /api/v1/config` → `{ "public_base_url": "…" }` (artwork absolute URLs only; stream paths stay relative).

## Public base URL

Set `PUBLIC_BASE_URL` (or `LATENTTONE_PUBLIC_URL` / `public_base_url` in `scanner.yaml`) to the canonical reverse-proxied origin, e.g. `https://latent.lt.lkeng.org/`. The SPA loads it from `GET /api/v1/config` and uses it for **MediaSession artwork** absolute URLs. Do not hardcode `localhost` in production proxy deployments.

## Android / mobile playback chrome

* **Screen Wake Lock** — requested on Play / Start station / Resume (user gesture). Re-acquired on `visibilitychange` when the tab becomes visible while still playing. Released on pause / stop / logout. Requires a **secure context** (HTTPS or localhost); fails soft on plain HTTP.
* **Media Session API** — lock-screen / headset controls (`play`, `pause`, `previoustrack`, `nexttrack` → player back / skip) and now-playing metadata (title, artist, album, artwork). Artwork URLs are absolute via `PUBLIC_BASE_URL`.
* A minimal `manifest.webmanifest` is shipped under `/app/` (no service worker yet).

## Known limitations

* Autoplay may require a user gesture on first start (browser policy).
* Crossfade between consecutive queue items is a short gain ramp on re-attach, not a dual-buffer mix of two decoders.
* Cookie `lt_session` must reach segment GETs — serve the SPA same-origin under `/app/` (default Compose).
* Phase 3 `/dev/stream` probe is debug-only (`enable_stream_probe: false` in product config).
* Progressive transcodes are not byte-range seekable (`Accept-Ranges: none`); seeking works for original progressive and HLS.
* Wake Lock on Android Chrome needs HTTPS through the reverse proxy (`PUBLIC_BASE_URL` should be `https://…`).
* Screen Wake Lock is unavailable on plain HTTP (non-localhost) — use HTTPS in production.
* Progressive streams must stay **same-origin relative** (`/api/v1/tracks/{id}/stream`) so cookies reach the proxy; do not point `<audio src>` at a different absolute host than the SPA.

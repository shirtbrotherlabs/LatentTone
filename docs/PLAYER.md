# Product player notes (Phase 4)

## Continuity path

1. Progressive first: `GET /api/v1/tracks/{id}/stream` for near-instant track switches.
2. Fallback: **Hls.js** against session `hls_url` (`/api/v1/sessions/{id}/hls/index.m3u8`) when progressive fails or is unavailable (`credentials: 'include'` / `xhr.withCredentials = true`).
3. Optional **Web Audio** `GainNode` soft fade on attach when `AudioContext` + `createMediaElementSource` succeed; otherwise plain `<audio>` playback.
4. **Gapless-oriented advance**: the SPA prefetches the next queue track on a standby `<audio>`, and ~0.45s before natural end posts `complete` so the server advances while `promotePrefetch` starts the warm URL (no dual-decoder crossfade mix). Falls back to a normal attach when the standby is not ready (common for cold FFmpeg transcodes).

Safari may use native HLS on `<audio>` when MSE/Hls.js is unavailable.

## Stream encode prefs (`/api/v1/me/stream-prefs`)

Per-user defaults persisted in MariaDB (`user_stream_prefs`):

| `stream_format` | Progressive behavior | HLS audio |
|-----------------|----------------------|-----------|
| `original` (default) | Serve file bytes when the container is browser-safe (`mp3`, `m4a`/`aac`, `flac`, `ogg`/`opus`, `wav`, …). Auto-transcode **MP3** for unsafe containers (`wma`, `ape`, …). | AAC @ bitrate |
| `mp3` | Always FFmpeg → MP3 @ `bitrate_kbps` | MP3 @ bitrate |
| `aac` | Always FFmpeg → AAC/ADTS @ `bitrate_kbps` | AAC @ bitrate |
| `opus` | Always FFmpeg → Opus (Ogg) @ `bitrate_kbps` | AAC @ bitrate (HLS fallback; Opus-in-TS is poorly supported) |

Default `bitrate_kbps` is **192** (clamped 64–320). Edit in Settings → Stream defaults.

## Android / lock-screen chrome

* **Screen Wake Lock** — requested on user-gesture play / start / resume; released on pause, stop, or end station; re-acquired on `visibilitychange` when still playing. Soft-fails when unsupported.
* **Wake Lock requires a secure context** — HTTPS (or `localhost`). Behind a reverse proxy, terminate TLS at the edge and set `PUBLIC_BASE_URL` to that HTTPS origin (see README).
* **Media Session** — `navigator.mediaSession` metadata from now-playing (title, artist, album, absolute artwork via `public_base_url`); action handlers for play, pause, previoustrack, nexttrack. Soft-fails when unsupported.
* **Natural end → next** — when remaining time is under ~0.45s and the next progressive URL is warm on a standby element, the SPA posts `complete` early and promotes the prefetch for near-gapless handoff. Otherwise `<audio>` `ended` posts `complete` and attaches the next stream as before.

Runtime config: `GET /api/v1/config` → `{ "public_base_url": "…" }` (artwork absolute URLs only; stream paths stay relative).

## Public base URL

Set `PUBLIC_BASE_URL` (or `LATENTTONE_PUBLIC_URL` / `public_base_url` in `scanner.yaml`) to the canonical reverse-proxied origin, e.g. `https://latent.lt.lkeng.org/`. The SPA loads it from `GET /api/v1/config` and uses it for **MediaSession artwork** absolute URLs. Do not hardcode `localhost` in production proxy deployments.

## Android / mobile playback chrome

* **Screen Wake Lock** — requested on Play / Start station / Resume (user gesture). Re-acquired on `visibilitychange` when the tab becomes visible while still playing. Released on pause / stop / logout. Requires a **secure context** (HTTPS or localhost); fails soft on plain HTTP.
* **Media Session API** — lock-screen / headset controls (`play`, `pause`, `previoustrack`, `nexttrack` → player back / skip) and now-playing metadata (title, artist, album, artwork). Artwork URLs are absolute via `PUBLIC_BASE_URL`.
* A minimal `manifest.webmanifest` is shipped under `/app/` (no service worker yet).

## Known limitations

* Autoplay may require a user gesture on first start (browser policy).
* Crossfade between consecutive queue items is a short gain ramp on re-attach when cold; warm prefetch uses promote-without-fade for nearer gapless handoff (not a dual-decoder mix of two simultaneous tracks).
* Cookie `lt_session` must reach segment GETs — serve the SPA same-origin under `/app/` (default Compose).
* Phase 3 `/dev/stream` probe is debug-only (`enable_stream_probe: false` in product config).
* Progressive transcodes are not byte-range seekable (`Accept-Ranges: none`); seeking works for original progressive and HLS.
* The next-track Range prefetch mainly warms original progressive files. For explicit MP3/AAC/Opus
  transcode prefs, `/stream` is a live FFmpeg pipe, so a tiny Range request cannot produce a reusable
  partial cache for the later `<audio>` request; reducing skip latency there needs server-side
  pre-encoded chunks or a next-track HLS cache.
* Wake Lock on Android Chrome needs HTTPS through the reverse proxy (`PUBLIC_BASE_URL` should be `https://…`).
* Screen Wake Lock is unavailable on plain HTTP (non-localhost) — use HTTPS in production.
* HLS packaging always passes FFmpeg `-vn` so embedded cover art (MP3/FLAC attached pictures) is not encoded as video — odd cover dimensions previously made libx264 abort and left the session HLS dir empty.
* Progressive transcode FFmpeg uses small probe sizes, `-map 0:a:0`, `-threads 1`, and `-flush_packets 1` for faster time-to-first-byte. Embed/ML/Lance helpers run at nice 15 and embed worker count reserves CPU cores so background identity scans do not starve playback encodes. Acoustic embed keeps **warm** ML (`ml_embed_helper.py serve`) and Lance (`lance_helper.py serve`) processes for the run — models and the LanceDB connection stay loaded; Lance upserts flush in `batch_size` chunks. The SPA keeps a healthy in-flight attach across skip confirm/poll (does not abort and restart FFmpeg for the same track).
* Progressive streams must stay **same-origin relative** (`/api/v1/tracks/{id}/stream`) so cookies reach the proxy; do not point `<audio src>` at a different absolute host than the SPA.
* **nginx HTTP Basic Auth**: protect the site at the edge if you want, but keep the SPA, `/api/`, `/covers/`, and stream/HLS paths on the **same origin** after auth. Browsers cache Basic credentials for the origin and attach them to `<audio>` / XHR once the realm is unlocked. Prefer not to force CORS on media (`crossOrigin`) for same-origin streams — that requires `Access-Control-Allow-*` headers LatentTone does not emit, and commonly leaves the seek bar stuck (`duration` unknown) even when sound plays. Catalog `duration_ms` is used as a fallback when the media element has no duration (typical for live FFmpeg progressive pipes).
* Missing app sessions use **403** on JSON `/api/*` routes (not 401) and responses strip `WWW-Authenticate`, so the login page’s `GET /api/v1/auth/me` probe does not re-trigger the browser’s HTTP Basic Auth prompt. Invalid login credentials still return 401 from `POST /api/v1/auth/login`.

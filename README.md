# LatentTone

LatentTone is an open-source, self-hosted music server designed for automated audio discovery and continuous playback. The project shifts the self-hosted media paradigm from manual playlist management to algorithmic, seed-based stream generation.

**Current status — Phase 4:** product SPA at `/app/` (auth chrome, listen loop, floating player, library browse, playlists) on top of Phase 3 auth/stream/session/feedback and Phase 3C playlists. `/` redirects (302) to `/app/`. Operator catalog HTML lives at `/browse` for scan/embed ops.

---

## What you get

* Config-driven scanner (`configs/scanner.yaml`) → SQLite catalog + cover paths
* Config-driven embedder (`configs/metadata.yaml`) → Essentia + catalog/filesig features in SQLite; LanceDB mirror for ANN
* Browse UI: Artist → Album → Track, covers, feature JSON on track page, neighbor playlist generator, Start/Stop embed
* Auth API (argon2id): register / login / logout / me — HTTP-only cookie + Bearer opaque token (ADR-005)
* Listening sessions + short-poll status; feedback (`like` / `dislike` / `skip` / `ban`) steers per-user queue (ADR-007)
* HLS under `/data/hls/{session_id}` + progressive `GET /api/v1/tracks/{id}/stream` (ADR-006)
* **Product SPA** at `/app/` — login/register, listen + floating player, catalog browse (Artist / Album / Track / Year), playlists, track actions (play next / thumbs / radio / playlist-from-track)
* Catalog JSON under `/api/v1/catalog/*` for the SPA (Phase 1–2 data; scanner still owned by operator tools)
* Queue inject `POST /api/v1/sessions/{id}/queue` (V5b play next)
* Optional flag-gated `/dev/stream` probe (Gate C1) — **debug only**, off by default; use the SPA for listening
* Optional flag-gated `/api/docs` Swagger UI (Phase 3B) — contract browser / Try-it; **not** a stream-smoke substitute
* Default embed samples a **random subset** (`max_tracks: 16` with Essentia); full-library embed is opt-in (`sample_mode: all`) and can take a long time

### Quick start (Docker Compose)

```bash
cp .env.example .env
# MUSIC_LIBRARY=/path/to/library  (mounted :ro — required)
docker compose up --build -d browse
```

**Product client:** <http://localhost:8080/> (redirects to `/app/`)  
Operator catalog inspector: <http://localhost:8080/browse>

### Reverse proxy / public URL

When LatentTone sits behind HTTPS (recommended for Android Wake Lock and lock-screen artwork), set the canonical origin in `.env`:

```bash
PUBLIC_BASE_URL=https://latent.lt.lkeng.org/
```

Compose passes this into the `browse` container. The Go server also accepts `LATENTTONE_PUBLIC_URL` or `public_base_url` in `configs/scanner.yaml`. Clients read it from `GET /api/v1/config` (no auth). Use this for MediaSession cover URLs and any absolute links — do not leave production pointed at `localhost`.

```bash
# One-shot catalog scan
docker compose --profile scan run --rm scan

# Embed a random subset (Essentia is CPU-heavy; default max_tracks is small)
docker compose --profile embed run --rm embed

# Stop a running embed (CLI / job control file)
docker compose run --rm browse embed --meta /config/metadata.yaml --stop

# Phase 3 stream smoke (auth → session → stream → skip ×2)
./scripts/stream_smoke.sh

# Layered developer regression (test-checkout)
./scripts/test_checkout.sh --suite browse    # default: go test + scan + SPA/operator routes
./scripts/test_checkout.sh --suite stream    # + Gate B stream/skip (required for session/audio changes)
./scripts/test_checkout.sh --suite full      # + embed + neighbor playlist (pre-tag / Gate D–class)
./scripts/test_checkout.sh --ref HEAD --suite fast
```

| Suite | Includes |
|-------|----------|
| `fast` | `go test -mod=vendor ./...` |
| `browse` | `fast` + `smoke.sh` (`/`→`/app/`, `/browse`, config) |
| `stream` | `browse` + `stream_smoke.sh` (progressive audio + **two skips** with post-skip bytes) |
| `full` | `stream` + Compose `embed` + neighbor / from-neighbor API |

Compose mounts `${MUSIC_LIBRARY}:/music:ro` — never mount the library read-write.

### Binary commands

```bash
latenttone scan  --config /config/scanner.yaml
latenttone embed --meta /config/metadata.yaml
latenttone embed --meta /config/metadata.yaml --stop
latenttone serve --config /config/scanner.yaml --meta /config/metadata.yaml
```

### API (auth)

```bash
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secretpass"}'
# → { "user": {...}, "token": "<opaque>" }  (+ Set-Cookie: lt_session)

TOKEN=...  # from register/login
curl -sS http://localhost:8080/api/v1/auth/me -H "Authorization: Bearer $TOKEN"
```

### API (session / stream / feedback)

```bash
# Create listening session from seed track
curl -sS -X POST http://localhost:8080/api/v1/sessions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"seed_track_id":418}'

# Short-poll status
curl -sS http://localhost:8080/api/v1/sessions/$SID -H "Authorization: Bearer $TOKEN"

# Progressive audio (smoke / fallback)
curl -sS -OJ -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/tracks/418/stream

# HLS playlist (session-scoped)
curl -sS -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/sessions/$SID/hls/index.m3u8

# Skip / like
curl -sS -X POST http://localhost:8080/api/v1/sessions/$SID/feedback \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"signal":"skip"}'
```

Catalog browse and `/api/v1/playlists` remain unauthenticated operator tools. Session / stream / feedback and `/api/v1/me/playlists*` require auth (`auth_mode: authenticated` default; `open` is solo-dev only).

### API (user playlists — Phase 3C)

Auth-bound CRUD under `/api/v1/me/playlists*` (ADR-008). Neighbor generate stays at `/api/v1/playlists`.

```bash
TOKEN=...  # from register/login

# Create empty user playlist
curl -sS -X POST http://localhost:8080/api/v1/me/playlists \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Late night"}'

# List mine
curl -sS http://localhost:8080/api/v1/me/playlists -H "Authorization: Bearer $TOKEN"

# Add tracks / reorder / remove
curl -sS -X POST http://localhost:8080/api/v1/me/playlists/$PID/tracks \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"track_ids":[418,419]}'
curl -sS -X PUT http://localhost:8080/api/v1/me/playlists/$PID/tracks/order \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"track_ids":[419,418]}'
curl -sS -X DELETE http://localhost:8080/api/v1/me/playlists/$PID/tracks/418 \
  -H "Authorization: Bearer $TOKEN"

# Rename / get / delete
curl -sS -X PATCH http://localhost:8080/api/v1/me/playlists/$PID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Renamed"}'
curl -sS http://localhost:8080/api/v1/me/playlists/$PID -H "Authorization: Bearer $TOKEN"
curl -sS -X DELETE http://localhost:8080/api/v1/me/playlists/$PID \
  -H "Authorization: Bearer $TOKEN"

# Optional: promote a neighbor playlist into my library
curl -sS -X POST http://localhost:8080/api/v1/me/playlists/from-neighbor \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"playlist_id":1,"name":"Saved neighbors"}'
```

### Operator install notes

| Mount / path | Role |
|--------------|------|
| `${MUSIC_LIBRARY}:/music:ro` | Library (read-only) |
| `${DATA_DIR:-./data}:/data` | SQLite, LanceDB, HLS under `/data/hls` |
| `configs/scanner.yaml` | Auth, `spa_root`, probe flags, `public_base_url` |
| Port `8080` | SPA `/app/`, APIs `/api/v1/*`, operator UI `/` |

See **Reverse proxy / public URL** above for `PUBLIC_BASE_URL` / TLS. Multi-arch: [`docs/MULTIARCH.md`](docs/MULTIARCH.md). Player / Android notes: [`docs/PLAYER.md`](docs/PLAYER.md).

### Gate C1 stream probe (`/dev/stream`)

Flag-gated verification UI (not the product client). **Off by default** in product `configs/scanner.yaml` — prefer `/app/` for listen/feedback.

**Enable via Compose (recommended for smoke):**

```bash
# Uses configs/scanner-stream-smoke.yaml (enable_stream_probe: true)
docker compose --profile stream-smoke up -d --build browse-stream
# Open http://localhost:8080/dev/stream  (also linked from Home when enabled)
```

**Enable on the default browse service:** set `enable_stream_probe: true` in `configs/scanner.yaml` (or mount `scanner-stream-smoke.yaml`), then rebuild/restart `browse`.

When enabled, catalog Home shows a non-product banner and nav includes **Stream probe**. Use login/register → start session → progressive (or HLS) playback → like / skip / dislike; the page short-polls session status for now playing + queue.

### API docs (Swagger UI)

Flag-gated OpenAPI 3 + Swagger UI for Phase 2+3 product APIs. Off by default in `configs/scanner.yaml`. On in `configs/scanner-stream-smoke.yaml`.

**Enable:**

```bash
# Stream-smoke Compose profile (also enables /dev/stream)
docker compose --profile stream-smoke up -d --build browse-stream
# Open http://localhost:8080/api/docs/

# Or set enable_api_docs: true in configs/scanner.yaml and restart browse
```

**Try it out:**

1. `POST /api/v1/auth/login` (or register) via curl or Swagger.
2. Copy the JSON `token`.
3. Click **Authorize** → Bearer `<token>`.
4. Call `GET /api/v1/auth/me`, then session/stream ops as needed.

Cookie `lt_session` works for same-origin clients; Swagger defaults to Bearer. HLS / progressive Try-it returns playlist text or binary downloads — use `/dev/stream` or Gate B for real listening. Spec file: [`api/openapi.yaml`](api/openapi.yaml) (also `GET /api/openapi.yaml` when docs are enabled).

### API (neighbor playlists)

```bash
# Create playlist from a seed track (k-NN; seed is track #0)
curl -sS -X POST http://localhost:8080/api/v1/playlists \
  -H 'Content-Type: application/json' \
  -d '{"seed_track_id":418,"length":20}'

# Fetch playlist
curl -sS http://localhost:8080/api/v1/playlists/1
```

Browse UI: track page → **Generate neighbor playlist** (when embedding status is `ready`).

### API (acoustic scan status)

```bash
# Live identity-scan progress + per-scanner coverage (Essentia / YAMNet / MusiCNN)
curl -sS http://localhost:8080/api/embed/status
```

Home page polls this endpoint and shows catalog coverage + this-run counters for each enabled acoustic scanner.

### Development notes

* Go module: `github.com/shirtbrotherlabs/LatentTone`
* Dependencies are vendored; see [`docs/DEPENDENCIES.md`](docs/DEPENDENCIES.md)
* Essentia (AGPL CLI subprocess) + LanceDB (Python helper) are runtime pins — see companion ADR-003
* Optional extractors `yamnet` / `musicnn` (ADR-004) — enable in `metadata.yaml` or Compose profile `embed-ml`
* Tests: `go test -mod=vendor ./...`

### License

GNU GPL v3 — see [`LICENSE`](LICENSE).

---

## SPA development (optional)

```bash
cd web && npm install && npm run dev
# Vite proxies /api and /covers to :8080 — run `latenttone serve` or Compose browse alongside
```

Production assets are built in the Docker `spa` stage and served from `spa_root` (`/usr/share/latenttone/app`).

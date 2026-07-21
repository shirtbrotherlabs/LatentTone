# LatentTone HTTP API

Operator and integrator notes for the JSON API. The product client is the SPA at `/app/` — use that for listening. OpenAPI contract: [`../api/openapi.yaml`](../api/openapi.yaml).

Optional Swagger UI: set `enable_api_docs: true` in `configs/scanner.yaml` (or use the Compose `stream-smoke` profile) and open `/api/docs/`.

## Auth

Default `auth_mode` is `authenticated`. Same-origin only (no CORS). The SPA uses an HttpOnly `lt_session` cookie; scripts may use the opaque Bearer token from register/login. Secure cookies when `public_base_url` is HTTPS (override with `SECURE_COOKIE`).

Missing app session on JSON `/api/*` returns **403** (not 401) and responses strip `WWW-Authenticate`, so probes do not re-open nginx HTTP Basic Auth dialogs. Invalid login credentials still return **401**.

```bash
curl -sS -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secretpass"}'
# → { "user": {...}, "token": "<opaque>" }  (+ Set-Cookie: lt_session)

TOKEN=...  # from register/login
curl -sS http://localhost:8080/api/v1/auth/me -H "Authorization: Bearer $TOKEN"
```

### Admin bootstrap

Set in `.env` (Compose passes into `browse`):

```bash
ADMIN_USERNAME=admin
ADMIN_PASSWORD=changeme-please
```

On `serve` startup, if that username is missing it is created with `is_admin=1`. Existing admins keep their password; change it in Settings (`POST /api/v1/auth/password`).

| Capability | Admin | Non-admin |
|------------|-------|-----------|
| Register / listen / playlists / radio prefs | yes | yes |
| View library + acoustic scan status | yes | yes |
| Start library scan / force rescan / edit scan schedule | yes | no (403) |
| Start/stop acoustic embed | yes | no (403) |

Library scan schedule: `GET|PATCH /api/scan/schedule` (default enabled, `interval_seconds=86400`). Startup scan is on by default; disable with `LATENTTONE_SCAN_ON_START=0`.

`GET /api/v1/auth/me` includes `is_admin`.

## Session / stream / feedback

```bash
# Create listening session from seed track
curl -sS -X POST http://localhost:8080/api/v1/sessions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"seed_track_id":418}'

# Short-poll status
curl -sS http://localhost:8080/api/v1/sessions/$SID -H "Authorization: Bearer $TOKEN"

# Progressive audio
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

Session / stream / feedback and `/api/v1/me/playlists*` require auth. Catalog browse and `/api/v1/playlists` remain available as operator tools (see OpenAPI for auth details).

Queue inject (play next): `POST /api/v1/sessions/{id}/queue`.

## User playlists

Auth-bound CRUD under `/api/v1/me/playlists*`. Neighbor generate stays at `/api/v1/playlists`.

```bash
TOKEN=...  # from register/login

curl -sS -X POST http://localhost:8080/api/v1/me/playlists \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Late night"}'

curl -sS http://localhost:8080/api/v1/me/playlists -H "Authorization: Bearer $TOKEN"

curl -sS -X POST http://localhost:8080/api/v1/me/playlists/$PID/tracks \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"track_ids":[418,419]}'
curl -sS -X PUT http://localhost:8080/api/v1/me/playlists/$PID/tracks/order \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"track_ids":[419,418]}'
curl -sS -X DELETE http://localhost:8080/api/v1/me/playlists/$PID/tracks/418 \
  -H "Authorization: Bearer $TOKEN"

curl -sS -X PATCH http://localhost:8080/api/v1/me/playlists/$PID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Renamed"}'
curl -sS http://localhost:8080/api/v1/me/playlists/$PID -H "Authorization: Bearer $TOKEN"
curl -sS -X DELETE http://localhost:8080/api/v1/me/playlists/$PID \
  -H "Authorization: Bearer $TOKEN"

curl -sS -X POST http://localhost:8080/api/v1/me/playlists/from-neighbor \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"playlist_id":1,"name":"Saved neighbors"}'
```

## Neighbor playlists

```bash
curl -sS -X POST http://localhost:8080/api/v1/playlists \
  -H 'Content-Type: application/json' \
  -d '{"seed_track_id":418,"length":20}'

curl -sS http://localhost:8080/api/v1/playlists/1
```

In the UI: track page → **Generate neighbor playlist** (when embedding status is `ready`).

## Acoustic scan status

```bash
curl -sS http://localhost:8080/api/embed/status
```

## Swagger UI

```bash
# Stream-smoke Compose profile enables /api/docs and /dev/stream
docker compose --profile stream-smoke up -d browse-stream
# Open http://localhost:8080/api/docs/
```

Or set `enable_api_docs: true` in `configs/scanner.yaml` and restart `browse`.

1. `POST /api/v1/auth/login` (or register).
2. Copy the JSON `token`.
3. **Authorize** → Bearer `<token>`.
4. Call `GET /api/v1/auth/me`, then session/stream ops.

Cookie `lt_session` works for same-origin clients; Swagger defaults to Bearer. Spec: `GET /api/openapi.yaml` when docs are enabled.

## Catalog search, duplicates, and Radio seeds

| Endpoint | Notes |
|----------|--------|
| `GET /api/v1/catalog/search/suggest?q=&limit=` | Typeahead: tracks, artists, albums (+ `cover_url`) |
| `GET /api/v1/catalog/duplicates?limit=` | Tag+duration duplicate groups (≤1s; case/punct-insensitive) |
| `GET /api/v1/catalog/genres?limit=` | Genres with track counts |
| `POST /api/v1/sessions` | Seed with `seed_track_id` **or** `seed_artist_id` / `seed_genre_id` / `seed_playlist_id` |

SPA: omnibox in the main chrome; Settings → Possible duplicates; Radio genre/artist seeds; playlist “Start radio from playlist”.

## Debug stream probe

Flag-gated `/dev/stream` (not the product client). Off by default. Prefer `/app/` for listening.

```bash
docker compose --profile stream-smoke up -d browse-stream
# http://localhost:8080/dev/stream
```

## See also

* [`PLAYER.md`](PLAYER.md) — player, streaming, reverse-proxy notes
* [`MULTIARCH.md`](MULTIARCH.md) — multi-arch image notes
* [`DEPENDENCIES.md`](DEPENDENCIES.md) — vendored dependencies

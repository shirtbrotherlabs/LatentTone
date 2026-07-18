# LatentTone

Self-hosted music server that turns your local library into a continuous, seed-based radio — pick a track, and LatentTone keeps the stream going with likes, skips, and affinity-guided up-next.

## Current status

**Public beta (`v0.4.0-beta.1`)** — ready to run from a published Docker image.

1. Point Compose at your music folder (read-only).
2. Start the stack, run a one-shot library scan.
3. Open the web app, create an account, pick a seed track, and hit play.

The product UI lives at `/app/` (the site root redirects there). Acoustic “similarity” embedding can run later in the background to improve radio quality; you can listen as soon as the catalog scan finishes.

## What you get

* **Listen** — seed-based continuous playback with a floating player that stays with you across the app
* **Steer the stream** — like, dislike, skip, play-next, and per-user radio / stream settings
* **Browse your library** — artists, albums, tracks, years, and cover art
* **Playlists** — save and edit your own lists; optional neighbor playlists from acoustic similarity
* **Accounts** — register / login; multi-user on one shared library

## Quick start

Requires Docker Compose and a folder of audio files on the host.

### 1. Create these two files

**`.env`**

```bash
MUSIC_LIBRARY=/path/to/your/music
MARIADB_PASSWORD=changeme-please
MARIADB_ROOT_PASSWORD=changeme-please-too
BROWSE_PORT=8080
PUBLIC_BASE_URL=http://localhost:8080
SECURE_COOKIE=false
ADMIN_USERNAME=admin
ADMIN_PASSWORD=changeme-please
```

Set `MUSIC_LIBRARY` to a real path. Keep the library mount **read-only** — LatentTone never needs write access to your files.

**`docker-compose.yml`**

```yaml
services:
  mariadb:
    image: mariadb:11.4
    environment:
      TZ: UTC
      MARIADB_DATABASE: latenttone
      MARIADB_USER: latenttone
      MARIADB_PASSWORD: ${MARIADB_PASSWORD:?set MARIADB_PASSWORD in .env}
      MARIADB_ROOT_PASSWORD: ${MARIADB_ROOT_PASSWORD:?set MARIADB_ROOT_PASSWORD in .env}
    volumes:
      - ${MARIADB_DATA:-./data/mariadb}:/var/lib/mysql
    healthcheck:
      test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
      interval: 5s
      timeout: 5s
      retries: 12
      start_period: 30s
    restart: unless-stopped

  browse:
    image: ghcr.io/shirtbrotherlabs/latenttone:v0.4.0-beta.1
    depends_on:
      mariadb:
        condition: service_healthy
    ports:
      - "${BROWSE_PORT:-8080}:8080"
    environment:
      TZ: UTC
      PUBLIC_BASE_URL: ${PUBLIC_BASE_URL:-http://localhost:8080}
      SECURE_COOKIE: ${SECURE_COOKIE:-false}
      ADMIN_USERNAME: ${ADMIN_USERNAME:-}
      ADMIN_PASSWORD: ${ADMIN_PASSWORD:-}
      DATABASE_DSN: latenttone:${MARIADB_PASSWORD}@tcp(mariadb:3306)/latenttone?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci
    volumes:
      - ${MUSIC_LIBRARY}:/music:ro
      - ${DATA_DIR:-./data}:/data
    command:
      - "serve"
      - "--config=/config/scanner.yaml"
      - "--meta=/config/metadata.yaml"
    healthcheck:
      test: ["CMD", "curl", "-sf", "--max-time", "30", "http://127.0.0.1:8080/api/v1/config"]
      interval: 60s
      timeout: 35s
      retries: 2
      start_period: 90s
    restart: unless-stopped
```

> If the GHCR package is still private, run `docker login ghcr.io` once, or clone this repo and use `docker compose up --build` with the included Compose file instead of the image above.

### 2. Start the server

```bash
docker compose up -d
```

Wait until `browse` is healthy (`docker compose ps`).

### 3. Scan your library (one-shot)

```bash
docker compose run --rm --entrypoint latenttone browse \
  scan --config /config/scanner.yaml
```

### 4. Play music

Open **http://localhost:8080/** → register → browse to a track → start listening.

Optional (improves radio recommendations; CPU-heavy; can run while you listen):

```bash
docker compose run --rm --entrypoint latenttone browse \
  embed --meta /config/metadata.yaml
```

Default embed samples a small random subset. Full-library embed is opt-in via `configs/metadata.yaml` (`sample_mode: all`) when you use the repo Compose overlays.

## Released images

| Tag | Meaning |
|-----|---------|
| `ghcr.io/shirtbrotherlabs/latenttone:v0.4.0-beta.1` | First public beta (`linux/amd64`) |
| `ghcr.io/shirtbrotherlabs/latenttone:beta` | Latest beta (floating) |

Pin the SemVer tag for reproducible deploys. `latest` is not published for betas.

## Security

This is a **public beta**. Application security has **not** been rigorously tested (no formal audit, limited adversarial review). If you expose LatentTone on the internet or any untrusted network, **you are on your own** — treat it as experimental software and assume bugs or misconfiguration could leak library access or account data.

**Recommendation:** do not publish port `8080` directly. Put LatentTone behind a reverse proxy with **HTTPS** and at least **HTTP Basic Authentication** (or equivalent edge auth) in front of the whole site. Keep `/app/`, `/api/`, `/covers/`, and media on the **same origin** after that auth succeeds. See [`docs/PLAYER.md`](docs/PLAYER.md) for proxy + Basic Auth notes that affect the player.

## Behind a reverse proxy

Set `PUBLIC_BASE_URL` to your public HTTPS origin (for lock-screen artwork and secure cookies). Prefer HTTPS for mobile Wake Lock.

## Going further

| Topic | Doc |
|-------|-----|
| HTTP API, auth, playlists, Swagger | [`docs/API.md`](docs/API.md) |
| Player / streaming / proxy notes | [`docs/PLAYER.md`](docs/PLAYER.md) |
| Multi-arch images | [`docs/MULTIARCH.md`](docs/MULTIARCH.md) |
| Dependencies | [`docs/DEPENDENCIES.md`](docs/DEPENDENCIES.md) |
| OpenAPI contract | [`api/openapi.yaml`](api/openapi.yaml) |

### Develop from this repo

```bash
cp .env.example .env   # set MUSIC_LIBRARY + MariaDB passwords
docker compose up --build -d browse
docker compose --profile scan run --rm scan
```

Operator catalog UI (scan/embed status): http://localhost:8080/browse  

SPA hot-reload (optional):

```bash
cd web && npm install && npm run dev
# Vite proxies /api and /covers to :8080
```

Regression harness: `./scripts/test_checkout.sh --suite browse` (see script help for `stream` / `full`).

## License

GNU GPL v3 — see [`LICENSE`](LICENSE).

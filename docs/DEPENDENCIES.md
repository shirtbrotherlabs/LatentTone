# Dependencies

Catalog of third-party dependencies used by LatentTone. Every added or upgraded dependency must be recorded here in the same change (see workspace `.cursorrules`).

**Policy**

- Open-source and license-compatible with the project `LICENSE` (GPL-3.0) only, unless the maintainer explicitly approves an exception.
- Pin to **GitHub tagged releases** (not floating `main`).
- Vendor into the repository (`vendor/`); do not rely on unpinned remote fetches for builds.

## Direct dependencies

| Name | Purpose | License | GitHub repository | Tagged release | Vendor path |
|------|---------|---------|-------------------|----------------|-------------|
| bogem/id3v2 | MP3 embedded APIC artwork extraction | MIT | https://github.com/bogem/id3v2 | v2.1.4 | `vendor/github.com/bogem/id3v2/v2` |
| dhowden/tag | MP3/MP4/OGG/FLAC metadata reading | BSD-2-Clause | https://github.com/dhowden/tag | pinned commit `3d75831295e8`¹ | `vendor/github.com/dhowden/tag` |
| fsnotify/fsnotify | Filesystem watcher for incremental scan | BSD-3-Clause | https://github.com/fsnotify/fsnotify | v1.8.0 | `vendor/github.com/fsnotify/fsnotify` |
| go-flac/go-flac | FLAC container parsing | Apache-2.0 | https://github.com/go-flac/go-flac | v2.0.4 | `vendor/github.com/go-flac/go-flac/v2` |
| go-sql-driver/mysql | `database/sql` driver for MariaDB (catalog store; SQLite hard cutover) | MPL-2.0 | https://github.com/go-sql-driver/mysql | v1.9.3 | `vendor/github.com/go-sql-driver/mysql` |
| tcolgate/mp3 | Accurate MP3 frame-duration calculation without audio decoding | MIT | https://github.com/tcolgate/mp3 | pinned commit `e79c5a46d300`¹ | `vendor/github.com/tcolgate/mp3` |
| go-yaml/yaml | `scanner.yaml` config parsing (`gopkg.in/yaml.v3`) | MIT / Apache-2.0 | https://github.com/go-yaml/yaml | v3.0.1 | `vendor/gopkg.in/yaml.v3` |
| golang.org/x/crypto | argon2id password KDF (ADR-005) | BSD-3-Clause | https://github.com/golang/crypto | v0.31.0 | `vendor/golang.org/x/crypto` |
| swagger-ui | Embedded Swagger UI for `/api/docs` (Phase 3B) | Apache-2.0 | https://github.com/swagger-api/swagger-ui | v5.32.8 | `internal/web/apidocs/static/` (minimal dist subset; see `LICENSE`/`NOTICE`/`VERSION.txt`) |

¹ These upstream repositories publish no GitHub tags. The maintainer approved
an exception on 2026-07-16: immutable Go pseudo-versions are pinned to the
listed commits and fully vendored.

## Frontend dependencies (Phase 4 SPA · `web/`)

Pinned in `web/package.json`. Built in the Docker `spa` stage (`node:22.12.0-bookworm`); output served from `/usr/share/latenttone/app` at `/app/`. Not Go-vendored.

| Name | Purpose | License | GitHub repository | Tagged release | Notes |
|------|---------|---------|-------------------|----------------|-------|
| react | Product SPA UI | MIT | https://github.com/facebook/react | v18.3.1 | `web/` |
| react-dom | DOM renderer | MIT | https://github.com/facebook/react | v18.3.1 | `web/` |
| react-router-dom | Client routes + shell outlet | MIT | https://github.com/remix-run/react-router | v6.28.0 | basename `/app` |
| hls.js | MSE HLS playback | Apache-2.0 | https://github.com/video-dev/hls.js | v1.5.20 | cookie-aware XHR |
| vite | SPA bundler | MIT | https://github.com/vitejs/vite | v5.4.11 | build-time |
| @vitejs/plugin-react | React plugin for Vite | MIT | https://github.com/vitejs/vite-plugin-react | v4.3.3 | build-time |
| typescript | Typecheck | Apache-2.0 | https://github.com/microsoft/TypeScript | v5.6.3 | build-time |
| @types/react | React typings | MIT | https://github.com/DefinitelyTyped/DefinitelyTyped | v18.3.12 | build-time |
| @types/react-dom | react-dom typings | MIT | https://github.com/DefinitelyTyped/DefinitelyTyped | v18.3.1 | build-time |

Fonts loaded at runtime from Google Fonts (Instrument Serif, Sora) for the product UI — CDN link in `web/index.html`, not vendored.

## Notable transitive dependencies

| Name | Via | Notes |
|------|-----|--------|
| filippo.io/edwards25519 | go-sql-driver/mysql | Ed25519 support for MariaDB's `ed25519` auth plugin; BSD-3-Clause |
| golang.org/x/sys | fsnotify / x/crypto | Standard Go x/sys support package |

Rebuild vendor after dependency changes:

```bash
go mod tidy
go mod vendor
```

Phase 2 default extractors `catalog` / `filesig` add **no new third-party Go packages**. Essentia and LanceDB are **runtime** dependencies (not Go-vendored); see ADR-003.

## Runtime dependencies (container image)

| Name | Purpose | License | Source | Pin | Notes |
|------|---------|---------|--------|-----|-------|
| MariaDB | Catalog / user-state relational store (replaces SQLite) | GPL-2.0 (server) | https://mariadb.org / `docker.io/library/mariadb` | image tag `11.4` | Compose service `mariadb`; data volume `${MARIADB_DATA}:/var/lib/mysql` |
| sqlite3 (CLI) | Read-only legacy-catalog reader for `latenttone migrate-sqlite` (one-shot SQLite→MariaDB importer) | Public domain | Debian/Ubuntu package via Essentia image `apt` | image distro pin | CLI only, shelled out to by `cmd/latenttone/migrate_sqlite.go`; **no** Go SQLite driver — the app binary itself has zero SQLite dependency |
| FFmpeg | HLS segment packaging + progressive remux (ADR-006) | LGPL/GPL components | Debian/Ubuntu package via Essentia image `apt` | image distro pin | Already installed in Dockerfile (`apt-get install ffmpeg`); CLI only |
| MTG Essentia | `essentia_streaming_extractor_music` CLI (AGPL subprocess) | AGPL-3.0 | https://github.com/MTG/essentia (`v2.1_beta5`) via `ghcr.io/mtg/essentia` | digest `sha256:a8131b97632ffeabceba22249f4767bbeddba9b40172199b5ce42242b4649536` | Copied via Docker `FROM`; not linked into Go |
| lancedb (Python) | On-disk vector index upsert/search | Apache-2.0 | https://github.com/lancedb/lancedb | GitHub `python-v0.34.0` / PyPI `lancedb==0.34.0` | Installed into `/opt/latenttone-venv`; invoked by `scripts/lance_helper.py` |
| uv | Install pinned CPython 3.11 + venv on Essentia (Ubuntu 20.04) base | Apache-2.0 / MIT | https://github.com/astral-sh/uv | `0.6.14` | Build-time in Dockerfile only |

| onnxruntime (Python) | MusiCNN ONNX inference | MIT | https://github.com/microsoft/onnxruntime | `v1.20.1` / PyPI `onnxruntime==1.20.1` | Image venv `/opt/latenttone-venv` |
| tflite-runtime | YAMNet TFLite inference | Apache-2.0 | https://pypi.org/project/tflite-runtime/ | `2.14.0` | Image venv; numpy constrained `<2` |
| MSD MusiCNN ONNX | music embedding + tags | (model weights; see ADR-004) | https://essentia.upf.edu/models/feature-extractors/musicnn/ | SHA-256 `49668ffe…616e1` | `/models/musicnn/` at image build |
| YAMNet TFLite | AudioSet embeddings | Apache-2.0 lineage | https://tfhub.dev/google/lite-model/yamnet/tflite/1 | SHA-256 `141fba1c…993c3` | `/models/yamnet/` at image build |


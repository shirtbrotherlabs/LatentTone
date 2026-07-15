# Dependencies

Catalog of third-party dependencies used by LatentTone. Every added or upgraded dependency must be recorded here in the same change (see workspace `.cursorrules`).

**Policy**

- Open-source and license-compatible with the project `LICENSE` (GPL-3.0) only, unless the maintainer explicitly approves an exception.
- Pin to **GitHub tagged releases** (not floating `main`).
- Vendor into the repository (`vendor/`); do not rely on unpinned remote fetches for builds.

## Direct dependencies

| Name | Purpose | License | GitHub repository | Tagged release | Vendor path |
|------|---------|---------|-------------------|----------------|-------------|
| bogem/id3v2 | MP3 ID3 tag reading | MIT | https://github.com/bogem/id3v2 | v2.1.4 | `vendor/github.com/bogem/id3v2/v2` |
| fsnotify/fsnotify | Filesystem watcher for incremental scan | BSD-3-Clause | https://github.com/fsnotify/fsnotify | v1.8.0 | `vendor/github.com/fsnotify/fsnotify` |
| glebarez/go-sqlite | Pure-Go `database/sql` SQLite driver | MIT | https://github.com/glebarez/go-sqlite | v1.22.0 | `vendor/github.com/glebarez/go-sqlite` |
| go-flac/go-flac | FLAC container parsing | Apache-2.0 | https://github.com/go-flac/go-flac | v2.0.4 | `vendor/github.com/go-flac/go-flac/v2` |
| go-flac/flacvorbis | FLAC Vorbis comment tags | Apache-2.0 | https://github.com/go-flac/flacvorbis | v2.0.2 | `vendor/github.com/go-flac/flacvorbis/v2` |
| go-yaml/yaml | `scanner.yaml` config parsing (`gopkg.in/yaml.v3`) | MIT / Apache-2.0 | https://github.com/go-yaml/yaml | v3.0.1 | `vendor/gopkg.in/yaml.v3` |
| golang.org/x/crypto | argon2id password KDF (ADR-005) | BSD-3-Clause | https://github.com/golang/crypto | v0.31.0 | `vendor/golang.org/x/crypto` |
| swagger-ui | Embedded Swagger UI for `/api/docs` (Phase 3B) | Apache-2.0 | https://github.com/swagger-api/swagger-ui | v5.32.8 | `internal/web/apidocs/static/` (minimal dist subset; see `LICENSE`/`NOTICE`/`VERSION.txt`) |

## Notable transitive dependencies

| Name | Via | Notes |
|------|-----|--------|
| modernc.org/sqlite | glebarez/go-sqlite | Pure-Go SQLite engine (versioned with the glebarez release); sources mirrored under `vendor/modernc.org/` |
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
| FFmpeg | HLS segment packaging + progressive remux (ADR-006) | LGPL/GPL components | Debian/Ubuntu package via Essentia image `apt` | image distro pin | Already installed in Dockerfile (`apt-get install ffmpeg`); CLI only |
| MTG Essentia | `essentia_streaming_extractor_music` CLI (AGPL subprocess) | AGPL-3.0 | https://github.com/MTG/essentia (`v2.1_beta5`) via `ghcr.io/mtg/essentia` | digest `sha256:a8131b97632ffeabceba22249f4767bbeddba9b40172199b5ce42242b4649536` | Copied via Docker `FROM`; not linked into Go |
| lancedb (Python) | On-disk vector index upsert/search | Apache-2.0 | https://github.com/lancedb/lancedb | GitHub `python-v0.34.0` / PyPI `lancedb==0.34.0` | Installed into `/opt/latenttone-venv`; invoked by `scripts/lance_helper.py` |
| uv | Install pinned CPython 3.11 + venv on Essentia (Ubuntu 20.04) base | Apache-2.0 / MIT | https://github.com/astral-sh/uv | `0.6.14` | Build-time in Dockerfile only |

| onnxruntime (Python) | MusiCNN ONNX inference | MIT | https://github.com/microsoft/onnxruntime | `v1.20.1` / PyPI `onnxruntime==1.20.1` | Image venv `/opt/latenttone-venv` |
| tflite-runtime | YAMNet TFLite inference | Apache-2.0 | https://pypi.org/project/tflite-runtime/ | `2.14.0` | Image venv; numpy constrained `<2` |
| MSD MusiCNN ONNX | music embedding + tags | (model weights; see ADR-004) | https://essentia.upf.edu/models/feature-extractors/musicnn/ | SHA-256 `49668ffe…616e1` | `/models/musicnn/` at image build |
| YAMNet TFLite | AudioSet embeddings | Apache-2.0 lineage | https://tfhub.dev/google/lite-model/yamnet/tflite/1 | SHA-256 `141fba1c…993c3` | `/models/yamnet/` at image build |


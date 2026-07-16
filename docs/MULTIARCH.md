# Multi-arch image build (Phase 4)

LatentTone product images target `linux/amd64` and `linux/arm64` via Docker Buildx. Publishing to a registry is optional for `v0.4.0`; a reproducible local build is enough when registry access is unavailable.

## Prerequisites

* Docker with Buildx (`docker buildx version`)
* Enough disk for the Essentia-based runtime layers + Node SPA build stage

## One-shot multi-arch build (local load)

Build for the host architecture and load into the local Docker engine:

```bash
cd LatentTone
docker buildx create --name latenttone-multi --use 2>/dev/null || docker buildx use latenttone-multi
docker buildx build \
  --platform linux/amd64 \
  -t latenttone:dev \
  --load \
  .
```

## Multi-arch build + push (release)

Replace `REGISTRY` / tags for your operator registry:

```bash
cd LatentTone
docker buildx create --name latenttone-multi --use 2>/dev/null || docker buildx use latenttone-multi
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t REGISTRY/latenttone:v0.4.0 \
  -t REGISTRY/latenttone:phase-4 \
  --push \
  .
```

Without push, Buildx cannot `--load` multiple platforms at once. Use `--push` to a registry, or build a single platform with `--load` as above.

## Verify

```bash
docker buildx imagetools inspect REGISTRY/latenttone:v0.4.0
# expect Platform entries for linux/amd64 and linux/arm64
```

Compose continues to use `image: latenttone:dev` by default (`docker compose build browse`).

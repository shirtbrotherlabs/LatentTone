# Multi-arch image build (Phase 4 / Q13)

LatentTone product images target **`linux/amd64` and `linux/arm64`** via Docker Buildx. Release CI pushes both platforms on `v*` tags. Publishing to a registry is optional for local use; a reproducible build is enough when registry access is unavailable.

## Prerequisites

* Docker with Buildx (`docker buildx version`)
* Enough disk for the Essentia-based runtime layers + Node SPA build stage
* For arm64: QEMU/`tonistiigi/binfmt` when building amd64↔arm64 on a single host (`docker run --privileged --rm tonistiigi/binfmt --install all`)

## One-shot single-arch build (local load)

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

On Apple Silicon / ARM hosts, prefer `--platform linux/arm64` for native speed.

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

## arm64 notes

* Dockerfile is arch-agnostic; Essentia base digest and ML wheels must resolve for `arm64`.
* If an upstream layer is amd64-only, document the failure and fall back to amd64 hosts for that cut — do not silently publish a broken arm64 manifest.
* Smoke acoustic embed + one progressive stream on an arm64 machine before relying on it in production.

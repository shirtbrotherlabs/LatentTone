# syntax=docker/dockerfile:1

# Essentia music extractor (AGPL) — pinned digest for Ubuntu 20.04 / music 2.0 / Essentia v2.1_beta5
FROM ghcr.io/mtg/essentia@sha256:a8131b97632ffeabceba22249f4767bbeddba9b40172199b5ce42242b4649536 AS essentia

# Phase 4 product SPA (Vite + React)
FROM node:22.12.0-bookworm AS spa
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
COPY vendor ./vendor
COPY . .
RUN CGO_ENABLED=0 go build -mod=vendor -buildvcs=false -o /out/latenttone ./cmd/latenttone

# Runtime: Essentia base + Python 3.11 venv (LanceDB, ONNX Runtime, TFLite) + checksum-pinned ML models
FROM essentia
COPY scripts/fetch_ml_models.sh /tmp/fetch_ml_models.sh
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && curl -LsSf https://astral.sh/uv/0.6.14/install.sh | sh \
    && export PATH="/root/.local/bin:$PATH" \
    && uv python install 3.11 \
    && uv venv /opt/latenttone-venv --python 3.11 \
    && UV_LINK_MODE=copy uv pip install --python /opt/latenttone-venv/bin/python \
         "lancedb==0.34.0" "numpy<2" "onnxruntime==1.20.1" "tflite-runtime==2.14.0" \
    && chmod +x /tmp/fetch_ml_models.sh \
    && /tmp/fetch_ml_models.sh /models \
    && rm -f /tmp/fetch_ml_models.sh

ENV PATH="/opt/latenttone-venv/bin:/root/.local/bin:/usr/local/bin:${PATH}"
ENV LATENTTONE_PYTHON=/opt/latenttone-venv/bin/python
ENV ESSENTIA_EXTRACTOR=essentia_streaming_extractor_music
ENV ML_EMBED_HELPER=/usr/local/lib/latenttone/ml_embed_helper.py
ENV YAMNET_MODEL=/models/yamnet/yamnet.tflite
ENV YAMNET_CLASS_MAP=/models/yamnet/yamnet_class_map.csv
ENV MUSICNN_MODEL=/models/musicnn/msd-musicnn-1.onnx
ENV MUSICNN_META=/models/musicnn/msd-musicnn-1.json

COPY --from=build /out/latenttone /usr/local/bin/latenttone
COPY --from=spa /web/dist /usr/share/latenttone/app
COPY configs/scanner.yaml /config/scanner.yaml
COPY configs/metadata.yaml /config/metadata.yaml
COPY configs/metadata-ml.yaml /config/metadata-ml.yaml
COPY configs/essentia_profile.yaml /config/essentia_profile.yaml
COPY scripts/lance_helper.py /usr/local/lib/latenttone/lance_helper.py
COPY scripts/ml_embed_helper.py /usr/local/lib/latenttone/ml_embed_helper.py

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/latenttone"]
CMD ["serve", "--config", "/config/scanner.yaml", "--meta", "/config/metadata.yaml"]

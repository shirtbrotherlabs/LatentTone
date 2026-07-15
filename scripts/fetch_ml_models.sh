#!/usr/bin/env bash
# Copyright (C) 2026 martinsah
# SPDX-License-Identifier: GPL-3.0-only
set -euo pipefail
ROOT="${1:-/models}"
mkdir -p "$ROOT/musicnn" "$ROOT/yamnet"

fetch() {
  local url="$1" dest="$2" sha="$3"
  echo "fetch $dest"
  curl -fsSL -o "$dest" "$url"
  echo "$sha  $dest" | sha256sum -c -
}

fetch 'https://essentia.upf.edu/models/feature-extractors/musicnn/msd-musicnn-1.onnx' \
  "$ROOT/musicnn/msd-musicnn-1.onnx" \
  '49668ffec47e52e94b96f45930bb46a28a1368d4bdfb5c05378fa834aca616e1'

fetch 'https://essentia.upf.edu/models/feature-extractors/musicnn/msd-musicnn-1.json' \
  "$ROOT/musicnn/msd-musicnn-1.json" \
  '8e6b3b509f0610c0e65dce467fd459d6777509388eaddb13ed138d8ac1341ffe'

fetch 'https://tfhub.dev/google/lite-model/yamnet/tflite/1?lite-format=tflite' \
  "$ROOT/yamnet/yamnet.tflite" \
  '141fba1cdaae842c816f28edc4937e8b4f0af4c8df21862ccc6b52dc567993c3'

curl -fsSL -o "$ROOT/yamnet/yamnet_class_map.csv" \
  'https://raw.githubusercontent.com/tensorflow/models/master/research/audioset/yamnet/yamnet_class_map.csv'
echo "ok models in $ROOT"

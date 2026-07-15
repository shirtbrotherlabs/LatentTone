# ML model assets (YAMNet / MusiCNN)

Weights are **not committed**. The Dockerfile downloads and checksum-verifies them into `/models`.
See companion `docs/architecture/ADR-004-yamnet-musicnn.md` and `docs/DEPENDENCIES.md`.

## Pins

| Asset | Source | SHA-256 |
|-------|--------|---------|
| `musicnn/msd-musicnn-1.onnx` | https://essentia.upf.edu/models/feature-extractors/musicnn/msd-musicnn-1.onnx | `49668ffec47e52e94b96f45930bb46a28a1368d4bdfb5c05378fa834aca616e1` |
| `musicnn/msd-musicnn-1.json` | https://essentia.upf.edu/models/feature-extractors/musicnn/msd-musicnn-1.json | `8e6b3b509f0610c0e65dce467fd459d6777509388eaddb13ed138d8ac1341ffe` |
| `yamnet/yamnet.tflite` | https://tfhub.dev/google/lite-model/yamnet/tflite/1?lite-format=tflite | `141fba1cdaae842c816f28edc4937e8b4f0af4c8df21862ccc6b52dc567993c3` |
| `yamnet/yamnet_class_map.csv` | https://raw.githubusercontent.com/tensorflow/models/master/research/audioset/yamnet/yamnet_class_map.csv | verified at build |

Local fetch: `scripts/fetch_ml_models.sh /path/to/models`

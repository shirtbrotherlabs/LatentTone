#!/usr/bin/env python3
# Copyright (C) 2026 martinsah
# SPDX-License-Identifier: GPL-3.0-only
# Author: martinsah
# Date: 2026-07-15
# Last-Modified: 2026-07-18
"""
YAMNet / MusiCNN embedding helper for LatentTone.
Commands:
  yamnet  <audio_path> [model_path] [class_map_csv] [--raw-f32le <pcm_path>]
  musicnn <audio_path> [onnx_path] [metadata_json] [--raw-f32le <pcm_path>]
Stdout JSON: {"features": {...}, "vector": [64 floats L2-normalized]}

When --raw-f32le is set, load mono float32 LE PCM at 16 kHz from that path
(shared decode from the Go embed job) instead of re-running ffmpeg on audio_path.
"""
from __future__ import annotations

import json
import math
import os
import subprocess
import sys
import tempfile
from typing import Any

BLOCK = 64
SR = 16000
MAX_SECONDS = 45.0  # operator-friendly; matches Essentia profile spirit


def die(msg: str, code: int = 1) -> None:
    print(msg, file=sys.stderr)
    raise SystemExit(code)


def load_raw_f32le(path: str) -> Any:
    import numpy as np

    try:
        wav = np.fromfile(path, dtype=np.float32)
    except OSError as e:
        die(f"raw pcm read failed: {e}")
    if wav.size == 0:
        die("raw pcm empty")
    max_samples = int(MAX_SECONDS * SR)
    if wav.size > max_samples:
        wav = wav[:max_samples]
    return wav


def load_mono_16k(path: str, raw_f32le: str | None = None) -> Any:
    import numpy as np

    if raw_f32le:
        return load_raw_f32le(raw_f32le)

    # Decode via ffmpeg (available on Essentia runtime image / Compose stack).
    cmd = [
        "ffmpeg", "-v", "error", "-i", path,
        "-t", str(MAX_SECONDS),
        "-ac", "1", "-ar", str(SR),
        "-f", "f32le", "-",
    ]
    try:
        proc = subprocess.run(cmd, check=True, capture_output=True)
    except FileNotFoundError:
        die("ffmpeg not found (required to decode library audio)")
    except subprocess.CalledProcessError as e:
        die(f"ffmpeg decode failed: {e.stderr.decode(errors='replace').strip()}")
    wav = np.frombuffer(proc.stdout, dtype=np.float32)
    if wav.size == 0:
        die("decoded audio empty")
    return wav


def pop_raw_f32le(argv: list[str]) -> tuple[list[str], str | None]:
    """Strip trailing --raw-f32le <path> from argv; return (rest, path|None)."""
    if len(argv) >= 2 and argv[-2] == "--raw-f32le":
        return argv[:-2], argv[-1]
    return argv, None


def l2_normalize(v: Any) -> Any:
    import numpy as np

    n = float(np.linalg.norm(v))
    if n == 0 or not math.isfinite(n):
        return v.astype(np.float32)
    return (v / n).astype(np.float32)


def to_block64(native: Any) -> list[float]:
    import numpy as np

    v = np.asarray(native, dtype=np.float32).reshape(-1)
    if v.size == 0:
        out = np.zeros(BLOCK, dtype=np.float32)
    elif v.size >= BLOCK:
        out = v[:BLOCK].copy()
    else:
        out = np.zeros(BLOCK, dtype=np.float32)
        out[: v.size] = v
    return l2_normalize(out).tolist()


def emit(features: dict[str, Any], native_vec: Any) -> None:
    import numpy as np

    native = np.asarray(native_vec, dtype=np.float32).reshape(-1)
    features = dict(features)
    features["native_dim"] = int(native.size)
    features["native_l2"] = float(np.linalg.norm(native))
    features["block_dim"] = BLOCK
    print(json.dumps({"features": features, "vector": to_block64(native)}))


# ---- YAMNet (TFLite) ---------------------------------------------------------

def run_yamnet(audio_path: str, model_path: str, class_map: str, raw_f32le: str | None = None) -> None:
    import numpy as np
    from tflite_runtime.interpreter import Interpreter

    wav = load_mono_16k(audio_path, raw_f32le=raw_f32le)
    # YAMNet patch is ~0.975s; ensure minimum length
    min_len = int(0.975 * SR)
    if wav.size < min_len:
        pad = np.zeros(min_len, dtype=np.float32)
        pad[: wav.size] = wav
        wav = pad

    interp = Interpreter(model_path=model_path, num_threads=1)
    inp = interp.get_input_details()[0]
    # Dynamic waveform length
    interp.resize_tensor_input(inp["index"], [wav.size], strict=True)
    interp.allocate_tensors()
    interp.set_tensor(inp["index"], wav)
    interp.invoke()
    outs = interp.get_output_details()
    # Expected: scores [N,521] or [1,521], embeddings [N,1024] or [1,1024]
    by_size = {}
    for o in outs:
        t = interp.get_tensor(o["index"])
        by_size[tuple(t.shape)] = (o["name"], t)

    scores = None
    embeds = None
    for shape, (name, t) in by_size.items():
        if len(shape) == 2 and shape[-1] == 521:
            scores = t
        if len(shape) == 2 and shape[-1] == 1024:
            embeds = t
    if embeds is None:
        die(f"yamnet: no 1024-d embedding output; shapes={list(by_size)}")

    emb_mean = embeds.mean(axis=0)
    top = []
    if scores is not None:
        mean_scores = scores.mean(axis=0)
        names = load_class_names(class_map, expected=len(mean_scores))
        idx = np.argsort(-mean_scores)[:8]
        for i in idx:
            top.append({"index": int(i), "name": names[i] if i < len(names) else str(i),
                        "score": float(mean_scores[i])})
    emit(
        {
            "model": "yamnet",
            "sample_rate": SR,
            "waveform_seconds": float(wav.size / SR),
            "frames": int(embeds.shape[0]),
            "top_classes": top,
        },
        emb_mean,
    )


def load_class_names(path: str, expected: int) -> list[str]:
    names: list[str] = []
    if path and os.path.isfile(path):
        with open(path, newline="") as f:
            # csv: index, mid, display_name
            import csv
            r = csv.DictReader(f)
            for row in r:
                names.append(row.get("display_name") or row.get("name") or next(iter(row.values())))
    while len(names) < expected:
        names.append(str(len(names)))
    return names


# ---- MusiCNN (ONNX, Essentia mel front-end) ---------------------------------

def mel_bands_musicnn(frame: Any) -> Any:
    """Match Essentia TensorflowInputMusiCNN: 512-sample frame -> 96 log10 mel bands."""
    import numpy as np

    if frame.size != 512:
        raise ValueError("frame must be 512")
    # Hann window (Essentia Windowing default for this pipeline is unnormalized)
    win = np.hanning(512).astype(np.float32)
    windowed = frame * win
    spec = np.fft.rfft(windowed, n=512)
    power = (spec.real * spec.real + spec.imag * spec.imag).astype(np.float32)
    # Slaney Mel filterbank 96 bands, SR=16000, unit_tri, linear weighting
    fb = _slaney_mel_filterbank(n_fft=512, n_mels=96, sr=SR)
    mel = fb @ power
    # UnaryOperator shift+scale then log10: log10(mel * 10000 + 1)
    return np.log10(mel * 10000.0 + 1.0).astype(np.float32)


_FILTERBANK_CACHE: Any = None


def _slaney_mel_filterbank(n_fft: int, n_mels: int, sr: int) -> Any:
    global _FILTERBANK_CACHE
    import numpy as np

    if _FILTERBANK_CACHE is not None:
        return _FILTERBANK_CACHE
    # HTK vs Slaney: Essentia warpingFormula=slaneyMel
    def hz_to_mel(hz: float) -> float:
        # Slaney (Auditory Toolbox): linear below 1000 Hz, log above
        f_sp = 200.0 / 3
        min_log_hz = 1000.0
        min_log_mel = min_log_hz / f_sp
        logstep = math.log(6.4) / 27.0
        if hz < min_log_hz:
            return hz / f_sp
        return min_log_mel + math.log(hz / min_log_hz) / logstep

    def mel_to_hz(mel: float) -> float:
        f_sp = 200.0 / 3
        min_log_hz = 1000.0
        min_log_mel = min_log_hz / f_sp
        logstep = math.log(6.4) / 27.0
        if mel < min_log_mel:
            return f_sp * mel
        return min_log_hz * math.exp(logstep * (mel - min_log_mel))

    n_freqs = n_fft // 2 + 1
    fmax = sr / 2
    mels = np.linspace(hz_to_mel(0), hz_to_mel(fmax), n_mels + 2)
    hz = np.array([mel_to_hz(m) for m in mels], dtype=np.float64)
    fft_freqs = np.linspace(0, fmax, n_freqs)
    weights = np.zeros((n_mels, n_freqs), dtype=np.float32)
    for i in range(n_mels):
        left, center, right = hz[i], hz[i + 1], hz[i + 2]
        # unit_tri: triangles peak at 1
        rising = (fft_freqs - left) / max(center - left, 1e-12)
        falling = (right - fft_freqs) / max(right - center, 1e-12)
        weights[i] = np.maximum(0, np.minimum(rising, falling))
    _FILTERBANK_CACHE = weights
    return weights


def musicnn_patches(wav: Any, patch_frames: int = 187, hop: int = 256) -> Any:
    import numpy as np

    # Frame with hop 256, size 512
    if wav.size < 512:
        pad = np.zeros(512, dtype=np.float32)
        pad[: wav.size] = wav
        wav = pad
    frames = []
    for start in range(0, wav.size - 512 + 1, hop):
        frames.append(mel_bands_musicnn(wav[start : start + 512]))
    if not frames:
        die("musicnn: no frames")
    bands = np.stack(frames, axis=0)  # [T, 96]
    patches = []
    if bands.shape[0] < patch_frames:
        pad = np.zeros((patch_frames, 96), dtype=np.float32)
        pad[: bands.shape[0]] = bands
        patches.append(pad)
    else:
        for s in range(0, bands.shape[0] - patch_frames + 1, patch_frames):
            patches.append(bands[s : s + patch_frames])
            if len(patches) >= 8:  # cap patches for speed (first ~24s)
                break
    return np.stack(patches, axis=0).astype(np.float32)  # [P, 187, 96]


def run_musicnn(
    audio_path: str, onnx_path: str, meta_json: str, raw_f32le: str | None = None
) -> None:
    import numpy as np
    import onnxruntime as ort

    wav = load_mono_16k(audio_path, raw_f32le=raw_f32le)
    patches = musicnn_patches(wav)
    sess = ort.InferenceSession(onnx_path, providers=["CPUExecutionProvider"])
    in_name = sess.get_inputs()[0].name
    out_names = [o.name for o in sess.get_outputs()]
    # Prefer named 'embeddings' / 'activations'
    emb_name = "embeddings" if "embeddings" in out_names else out_names[-1]
    act_name = "activations" if "activations" in out_names else None

    embeds = []
    acts = []
    for p in patches:
        feeds = {in_name: p[None, ...]}  # [1,187,96]
        results = sess.run(None, feeds)
        by = {n: r for n, r in zip(out_names, results)}
        embeds.append(by[emb_name].reshape(-1))
        if act_name and act_name in by:
            acts.append(by[act_name].reshape(-1))

    emb_mean = np.mean(np.stack(embeds, axis=0), axis=0)
    top = []
    classes = []
    if meta_json and os.path.isfile(meta_json):
        with open(meta_json) as f:
            meta = json.load(f)
        classes = list(meta.get("classes") or [])
    if acts:
        act_mean = np.mean(np.stack(acts, axis=0), axis=0)
        idx = np.argsort(-act_mean)[:8]
        for i in idx:
            top.append({
                "index": int(i),
                "name": classes[i] if i < len(classes) else str(i),
                "score": float(act_mean[i]),
            })
    emit(
        {
            "model": "musicnn-msd-1",
            "sample_rate": SR,
            "waveform_seconds": float(wav.size / SR),
            "patches": int(patches.shape[0]),
            "top_tags": top,
        },
        emb_mean,
    )


def main() -> None:
    argv, raw_pcm = pop_raw_f32le(sys.argv[1:])
    if len(argv) < 2:
        die("usage: ml_embed_helper.py yamnet|musicnn <audio> [model] [extra] [--raw-f32le pcm]")
    cmd = argv[0]
    audio = argv[1]
    if not os.path.isfile(audio) and not raw_pcm:
        die(f"audio not found: {audio}")
    if raw_pcm and not os.path.isfile(raw_pcm):
        die(f"raw pcm not found: {raw_pcm}")
    if cmd == "yamnet":
        model = argv[2] if len(argv) > 2 else os.environ.get(
            "YAMNET_MODEL", "/models/yamnet/yamnet.tflite"
        )
        cmap = argv[3] if len(argv) > 3 else os.environ.get(
            "YAMNET_CLASS_MAP", "/models/yamnet/yamnet_class_map.csv"
        )
        run_yamnet(audio, model, cmap, raw_f32le=raw_pcm)
    elif cmd == "musicnn":
        model = argv[2] if len(argv) > 2 else os.environ.get(
            "MUSICNN_MODEL", "/models/musicnn/msd-musicnn-1.onnx"
        )
        meta = argv[3] if len(argv) > 3 else os.environ.get(
            "MUSICNN_META", "/models/musicnn/msd-musicnn-1.json"
        )
        run_musicnn(audio, model, meta, raw_f32le=raw_pcm)
    else:
        die(f"unknown command {cmd}")


if __name__ == "__main__":
    main()

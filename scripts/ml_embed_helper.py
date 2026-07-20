#!/usr/bin/env python3
# Copyright (C) 2026 martinsah
# SPDX-License-Identifier: GPL-3.0-only
# Author: martinsah
# Date: 2026-07-15
# Last-Modified: 2026-07-20
"""
YAMNet / MusiCNN embedding helper for LatentTone.

One-shot:
  yamnet  <audio_path> [model_path] [class_map_csv] [--raw-f32le <pcm_path>]
  musicnn <audio_path> [onnx_path] [metadata_json] [--raw-f32le <pcm_path>]
  → stdout JSON: {"features": {...}, "vector": [64 floats L2-normalized]}

Warm daemon (models stay loaded across requests):
  serve
  → stdin JSON lines, stdout JSON lines (see serve_loop).

When --raw-f32le is set, load mono float32 LE PCM at 16 kHz from that path
(shared decode from the Go embed job) instead of re-running ffmpeg on audio_path.
"""
from __future__ import annotations

import json
import math
import os
import subprocess
import sys
from typing import Any

BLOCK = 64
SR = 16000
MAX_SECONDS = 45.0  # operator-friendly; matches Essentia profile spirit

# Warm caches for serve mode (one process = one set of models).
_YAMNET: dict[str, Any] = {"model": None, "interp": None, "class_map": None, "names": None}
_MUSICNN: dict[str, Any] = {"model": None, "sess": None, "meta": None, "classes": None, "in_name": None, "out_names": None}


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


def pack_result(features: dict[str, Any], native_vec: Any) -> dict[str, Any]:
    import numpy as np

    native = np.asarray(native_vec, dtype=np.float32).reshape(-1)
    features = dict(features)
    features["native_dim"] = int(native.size)
    features["native_l2"] = float(np.linalg.norm(native))
    features["block_dim"] = BLOCK
    return {"features": features, "vector": to_block64(native)}


def emit(features: dict[str, Any], native_vec: Any) -> None:
    print(json.dumps(pack_result(features, native_vec)))


# ---- YAMNet (TFLite) ---------------------------------------------------------

def ensure_yamnet(model_path: str, class_map: str) -> Any:
    from tflite_runtime.interpreter import Interpreter

    if _YAMNET["interp"] is None or _YAMNET["model"] != model_path:
        interp = Interpreter(model_path=model_path, num_threads=1)
        _YAMNET["interp"] = interp
        _YAMNET["model"] = model_path
        _YAMNET["class_map"] = None
        _YAMNET["names"] = None
    if _YAMNET["class_map"] != class_map:
        _YAMNET["class_map"] = class_map
        _YAMNET["names"] = None
    return _YAMNET["interp"]


def compute_yamnet(
    audio_path: str, model_path: str, class_map: str, raw_f32le: str | None = None
) -> dict[str, Any]:
    import numpy as np

    wav = load_mono_16k(audio_path, raw_f32le=raw_f32le)
    min_len = int(0.975 * SR)
    if wav.size < min_len:
        pad = np.zeros(min_len, dtype=np.float32)
        pad[: wav.size] = wav
        wav = pad

    interp = ensure_yamnet(model_path, class_map)
    inp = interp.get_input_details()[0]
    interp.resize_tensor_input(inp["index"], [wav.size], strict=True)
    interp.allocate_tensors()
    interp.set_tensor(inp["index"], wav)
    interp.invoke()
    outs = interp.get_output_details()
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
        raise RuntimeError(f"yamnet: no 1024-d embedding output; shapes={list(by_size)}")

    emb_mean = embeds.mean(axis=0)
    top = []
    if scores is not None:
        mean_scores = scores.mean(axis=0)
        if _YAMNET["names"] is None or len(_YAMNET["names"]) < len(mean_scores):
            _YAMNET["names"] = load_class_names(class_map, expected=len(mean_scores))
        names = _YAMNET["names"]
        idx = np.argsort(-mean_scores)[:8]
        for i in idx:
            top.append(
                {
                    "index": int(i),
                    "name": names[i] if i < len(names) else str(i),
                    "score": float(mean_scores[i]),
                }
            )
    return pack_result(
        {
            "model": "yamnet",
            "sample_rate": SR,
            "waveform_seconds": float(wav.size / SR),
            "frames": int(embeds.shape[0]),
            "top_classes": top,
        },
        emb_mean,
    )


def run_yamnet(audio_path: str, model_path: str, class_map: str, raw_f32le: str | None = None) -> None:
    try:
        out = compute_yamnet(audio_path, model_path, class_map, raw_f32le=raw_f32le)
    except Exception as e:
        die(str(e))
    print(json.dumps(out))


def load_class_names(path: str, expected: int) -> list[str]:
    names: list[str] = []
    if path and os.path.isfile(path):
        with open(path, newline="") as f:
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
    win = np.hanning(512).astype(np.float32)
    windowed = frame * win
    spec = np.fft.rfft(windowed, n=512)
    power = (spec.real * spec.real + spec.imag * spec.imag).astype(np.float32)
    fb = _slaney_mel_filterbank(n_fft=512, n_mels=96, sr=SR)
    mel = fb @ power
    return np.log10(mel * 10000.0 + 1.0).astype(np.float32)


_FILTERBANK_CACHE: Any = None


def _slaney_mel_filterbank(n_fft: int, n_mels: int, sr: int) -> Any:
    global _FILTERBANK_CACHE
    import numpy as np

    if _FILTERBANK_CACHE is not None:
        return _FILTERBANK_CACHE

    def hz_to_mel(hz: float) -> float:
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
        rising = (fft_freqs - left) / max(center - left, 1e-12)
        falling = (right - fft_freqs) / max(right - center, 1e-12)
        weights[i] = np.maximum(0, np.minimum(rising, falling))
    _FILTERBANK_CACHE = weights
    return weights


def musicnn_patches(wav: Any, patch_frames: int = 187, hop: int = 256) -> Any:
    import numpy as np

    if wav.size < 512:
        pad = np.zeros(512, dtype=np.float32)
        pad[: wav.size] = wav
        wav = pad
    frames = []
    for start in range(0, wav.size - 512 + 1, hop):
        frames.append(mel_bands_musicnn(wav[start : start + 512]))
    if not frames:
        raise RuntimeError("musicnn: no frames")
    bands = np.stack(frames, axis=0)
    patches = []
    if bands.shape[0] < patch_frames:
        pad = np.zeros((patch_frames, 96), dtype=np.float32)
        pad[: bands.shape[0]] = bands
        patches.append(pad)
    else:
        for s in range(0, bands.shape[0] - patch_frames + 1, patch_frames):
            patches.append(bands[s : s + patch_frames])
            if len(patches) >= 8:
                break
    return np.stack(patches, axis=0).astype(np.float32)


def ensure_musicnn(onnx_path: str, meta_json: str) -> Any:
    import onnxruntime as ort

    if _MUSICNN["sess"] is None or _MUSICNN["model"] != onnx_path:
        sess = ort.InferenceSession(onnx_path, providers=["CPUExecutionProvider"])
        _MUSICNN["sess"] = sess
        _MUSICNN["model"] = onnx_path
        _MUSICNN["in_name"] = sess.get_inputs()[0].name
        _MUSICNN["out_names"] = [o.name for o in sess.get_outputs()]
    if _MUSICNN["meta"] != meta_json:
        _MUSICNN["meta"] = meta_json
        classes: list[str] = []
        if meta_json and os.path.isfile(meta_json):
            with open(meta_json) as f:
                meta = json.load(f)
            classes = list(meta.get("classes") or [])
        _MUSICNN["classes"] = classes
    return _MUSICNN["sess"]


def compute_musicnn(
    audio_path: str, onnx_path: str, meta_json: str, raw_f32le: str | None = None
) -> dict[str, Any]:
    import numpy as np

    wav = load_mono_16k(audio_path, raw_f32le=raw_f32le)
    patches = musicnn_patches(wav)
    sess = ensure_musicnn(onnx_path, meta_json)
    in_name = _MUSICNN["in_name"]
    out_names = _MUSICNN["out_names"]
    emb_name = "embeddings" if "embeddings" in out_names else out_names[-1]
    act_name = "activations" if "activations" in out_names else None

    embeds = []
    acts = []
    for p in patches:
        feeds = {in_name: p[None, ...]}
        results = sess.run(None, feeds)
        by = {n: r for n, r in zip(out_names, results)}
        embeds.append(by[emb_name].reshape(-1))
        if act_name and act_name in by:
            acts.append(by[act_name].reshape(-1))

    emb_mean = np.mean(np.stack(embeds, axis=0), axis=0)
    top = []
    classes = list(_MUSICNN["classes"] or [])
    if acts:
        act_mean = np.mean(np.stack(acts, axis=0), axis=0)
        idx = np.argsort(-act_mean)[:8]
        for i in idx:
            top.append(
                {
                    "index": int(i),
                    "name": classes[i] if i < len(classes) else str(i),
                    "score": float(act_mean[i]),
                }
            )
    return pack_result(
        {
            "model": "musicnn-msd-1",
            "sample_rate": SR,
            "waveform_seconds": float(wav.size / SR),
            "patches": int(patches.shape[0]),
            "top_tags": top,
        },
        emb_mean,
    )


def run_musicnn(
    audio_path: str, onnx_path: str, meta_json: str, raw_f32le: str | None = None
) -> None:
    try:
        out = compute_musicnn(audio_path, onnx_path, meta_json, raw_f32le=raw_f32le)
    except Exception as e:
        die(str(e))
    print(json.dumps(out))


# ---- Warm serve loop ---------------------------------------------------------

def serve_loop() -> None:
    """Persistent JSON-line protocol; keeps TFLite/ONNX sessions warm."""
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError as e:
            print(json.dumps({"ok": False, "error": f"bad json: {e}"}), flush=True)
            continue
        rid = req.get("id")
        cmd = str(req.get("cmd") or "")
        if cmd == "shutdown":
            print(json.dumps({"id": rid, "ok": True, "shutdown": True}), flush=True)
            return
        if cmd == "ping":
            print(json.dumps({"id": rid, "ok": True, "pong": True}), flush=True)
            continue
        try:
            if cmd == "yamnet":
                out = compute_yamnet(
                    str(req.get("audio") or ""),
                    str(req.get("model") or os.environ.get("YAMNET_MODEL", "/models/yamnet/yamnet.tflite")),
                    str(req.get("extra") or os.environ.get("YAMNET_CLASS_MAP", "/models/yamnet/yamnet_class_map.csv")),
                    raw_f32le=req.get("raw_f32le") or None,
                )
            elif cmd == "musicnn":
                out = compute_musicnn(
                    str(req.get("audio") or ""),
                    str(req.get("model") or os.environ.get("MUSICNN_MODEL", "/models/musicnn/msd-musicnn-1.onnx")),
                    str(req.get("extra") or os.environ.get("MUSICNN_META", "/models/musicnn/msd-musicnn-1.json")),
                    raw_f32le=req.get("raw_f32le") or None,
                )
            else:
                raise RuntimeError(f"unknown command {cmd}")
            print(
                json.dumps(
                    {
                        "id": rid,
                        "ok": True,
                        "features": out["features"],
                        "vector": out["vector"],
                    }
                ),
                flush=True,
            )
        except Exception as e:
            print(json.dumps({"id": rid, "ok": False, "error": str(e)}), flush=True)


def main() -> None:
    if len(sys.argv) >= 2 and sys.argv[1] == "serve":
        serve_loop()
        return

    argv, raw_pcm = pop_raw_f32le(sys.argv[1:])
    if len(argv) < 2:
        die("usage: ml_embed_helper.py serve | yamnet|musicnn <audio> [model] [extra] [--raw-f32le pcm]")
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

/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

const DEPS = [
  { name: "React", note: "Product SPA UI" },
  { name: "React Router", note: "Client routes + shell outlet" },
  { name: "hls.js", note: "MSE HLS playback" },
  { name: "Vite / TypeScript", note: "SPA build toolchain" },
  { name: "FFmpeg", note: "HLS packaging + progressive remux" },
  { name: "Essentia", note: "Acoustic feature extraction (AGPL subprocess)" },
  { name: "LanceDB", note: "On-disk vector index" },
  { name: "MariaDB", note: "Catalog database" },
  { name: "ONNX Runtime / TFLite", note: "MusiCNN + YAMNet embeddings" },
  { name: "Swagger UI", note: "Optional /api/docs explorer" },
];

export function AboutPage() {
  return (
    <section>
      <h1 className="page-title">About</h1>
      <p className="page-lead">
        LatentTone is a self-hosted music server for seed-based continuous listening over a local
        library.
      </p>

      <div className="settings-grid">
        <div className="tile">
          <h3>Attribution</h3>
          <p>
            Created by <strong>Martin Klingensmith</strong> (<code>martinsah</code>).
          </p>
          <p style={{ marginTop: "0.75rem" }}>
            <a
              className="external-link"
              href="https://github.com/shirtbrotherlabs/LatentTone"
              target="_blank"
              rel="noreferrer"
            >
              github.com/shirtbrotherlabs/LatentTone
            </a>
          </p>
        </div>

        <div className="tile">
          <h3>License</h3>
          <p>
            LatentTone is released under the{" "}
            <a
              className="external-link"
              href="https://www.gnu.org/licenses/gpl-3.0.html"
              target="_blank"
              rel="noreferrer"
            >
              GNU GPL-3.0
            </a>
            . See <code>LICENSE</code> in the repository.
          </p>
        </div>

        <div className="tile">
          <h3>Mobile playback</h3>
          <p>
            Screen Wake Lock and Media Session keep listening usable on Android when the screen locks.
            Wake Lock needs a secure context — serve the app over <strong>HTTPS</strong> via your reverse
            proxy and set <code>PUBLIC_BASE_URL</code> to that origin (see <code>docs/PLAYER.md</code>).
          </p>
        </div>

        <div className="tile" style={{ gridColumn: "1 / -1" }}>
          <h3>Dependencies</h3>
          <p className="muted" style={{ marginBottom: "0.75rem" }}>
            Notable open-source components (curated from project dependency docs — not an exhaustive
            transitive dump).
          </p>
          <ul className="about-deps">
            {DEPS.map((d) => (
              <li key={d.name}>
                <strong>{d.name}</strong>
                <span className="muted"> — {d.note}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

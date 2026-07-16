/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogTrack, Playlist, PlaylistHeader } from "../api/types";
import { TrackActions, formatDuration } from "../components/TrackActions";
import { usePlayer } from "../player/PlayerContext";

export function PlaylistsPage() {
  const [playlists, setPlaylists] = useState<PlaylistHeader[]>([]);
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();

  const reload = () =>
    api
      .listPlaylists()
      .then((r) => setPlaylists(r.playlists))
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));

  useEffect(() => {
    void reload();
  }, []);

  const onCreate = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      const pl = await api.createPlaylist(name.trim() || "Untitled");
      setName("");
      navigate(`/playlists/${pl.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed");
    }
  };

  return (
    <section>
      <h1 className="page-title">Playlists</h1>
      <p className="page-lead">Your playlists.</p>
      <form className="toolbar" onSubmit={(e) => void onCreate(e)}>
        <input
          type="text"
          placeholder="New playlist name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button className="btn" type="submit">
          Create
        </button>
      </form>
      {error ? <p className="error">{error}</p> : null}
      <div className="cover-grid">
        {playlists.map((p) => (
          <Link key={p.id} className="tile tile-cover-card" to={`/playlists/${p.id}`}>
            {p.cover_url ? (
              <img className="tile-cover" src={p.cover_url} alt="" loading="lazy" />
            ) : (
              <div className="tile-cover tile-cover-fallback" aria-hidden>
                {p.name.slice(0, 1).toUpperCase() || "P"}
              </div>
            )}
            <h3>{p.name}</h3>
            <p>
              {p.length} tracks · {p.kind}
            </p>
          </Link>
        ))}
      </div>
    </section>
  );
}

export function PlaylistDetailPage() {
  const { id } = useParams();
  const [pl, setPl] = useState<Playlist | null>(null);
  const [rename, setRename] = useState("");
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();
  const { startRadio } = usePlayer();

  const load = () => {
    const n = Number(id);
    if (!n) return;
    void api
      .getPlaylist(n)
      .then((p) => {
        setPl(p);
        setRename(p.name);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  const tracksAsCatalog = (pl?.tracks ?? []).map(
    (t): CatalogTrack => ({
      id: t.track_id,
      title: t.title || `Track ${t.track_id}`,
      artist: t.artist || "",
      album: t.album || "",
      duration_ms: t.duration_ms,
    }),
  );

  if (error) return <p className="error">{error}</p>;
  if (!pl) return <p className="muted">Loading…</p>;

  return (
    <section>
      <h1 className="page-title">{pl.name}</h1>
      <p className="page-lead">{pl.length} tracks</p>
      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault();
          void api
            .renamePlaylist(pl.id, rename)
            .then(setPl)
            .catch((err) => setError(err instanceof Error ? err.message : "failed"));
        }}
      >
        <input value={rename} onChange={(e) => setRename(e.target.value)} />
        <button className="btn" type="submit">
          Rename
        </button>
        {tracksAsCatalog[0] ? (
          <button
            type="button"
            className="btn btn-ghost"
            onClick={() => void startRadio(tracksAsCatalog[0].id)}
          >
            Listen from first track
          </button>
        ) : null}
        <button
          type="button"
          className="btn btn-danger"
          onClick={() => {
            void api.deletePlaylist(pl.id).then(() => navigate("/playlists"));
          }}
        >
          Delete
        </button>
      </form>

      <table className="track-table">
        <thead>
          <tr>
            <th>#</th>
            <th>Title</th>
            <th>Artist</th>
            <th>Time</th>
            <th />
            <th />
          </tr>
        </thead>
        <tbody>
          {tracksAsCatalog.map((t, idx) => (
            <tr key={`${t.id}-${idx}`}>
              <td className="track-meta">{idx + 1}</td>
              <td>
                <Link className="track-title" to={`/library/tracks/${t.id}`}>
                  {t.title}
                </Link>
              </td>
              <td className="track-meta">{t.artist}</td>
              <td className="track-meta">{formatDuration(t.duration_ms)}</td>
              <td>
                <TrackActions track={t} />
              </td>
              <td>
                <button
                  type="button"
                  className="btn btn-ghost"
                  onClick={() =>
                    void api
                      .removePlaylistTrack(pl.id, t.id)
                      .then(setPl)
                      .catch((err) => setError(err instanceof Error ? err.message : "failed"))
                  }
                >
                  Remove
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

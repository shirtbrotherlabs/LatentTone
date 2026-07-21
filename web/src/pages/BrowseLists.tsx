/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { CatalogAlbum, CatalogArtist, CatalogTrack } from "../api/types";
import { TrackTable } from "../components/TrackTable";
import { useBrowseScrollRestore } from "../hooks/useBrowseScrollRestore";
import { usePlayer } from "../player/PlayerContext";

/** In-memory caches so Back can restore scroll before the network round-trip. */
let artistsListCache: CatalogArtist[] | null = null;
let albumsListCache: CatalogAlbum[] | null = null;
let yearsListCache: { year: number; count: number }[] | null = null;

function artistLetter(name: string): string {
  const ch = (name.trim()[0] || "#").toUpperCase();
  return ch >= "A" && ch <= "Z" ? ch : "#";
}

function CoverThumb({ url, label }: { url?: string; label: string }) {
  if (url) {
    return <img className="tile-cover" src={url} alt="" loading="lazy" />;
  }
  return (
    <div className="tile-cover tile-cover-fallback" aria-hidden>
      {label.slice(0, 1).toUpperCase() || "?"}
    </div>
  );
}

export function ArtistsList() {
  const [artists, setArtists] = useState<CatalogArtist[]>(() => artistsListCache ?? []);
  const [error, setError] = useState<string | null>(null);
  const sectionRefs = useRef<Record<string, HTMLElement | null>>({});
  useBrowseScrollRestore("library/artists", artists.length > 0);

  useEffect(() => {
    void api
      .listArtists()
      .then((r) => {
        artistsListCache = r.artists;
        setArtists(r.artists);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, []);

  const letters = useMemo(() => {
    const present = new Set(artists.map((a) => artistLetter(a.name)));
    return ["#", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ".split("")].filter((l) => present.has(l));
  }, [artists]);

  const jump = (letter: string) => {
    sectionRefs.current[letter]?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  if (error) return <p className="error">{error}</p>;

  let lastLetter = "";
  return (
    <div className="artist-browse">
      <nav className="alpha-index" aria-label="Artist alphabet index">
        {["#", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ".split("")].map((l) => {
          const enabled = letters.includes(l);
          return (
            <button
              key={l}
              type="button"
              className={enabled ? "alpha-btn" : "alpha-btn alpha-btn-dim"}
              disabled={!enabled}
              onClick={() => jump(l)}
            >
              {l}
            </button>
          );
        })}
      </nav>
      <div className="cover-grid cover-grid-artists">
        {artists.map((a) => {
          const letter = artistLetter(a.name);
          const showAnchor = letter !== lastLetter;
          lastLetter = letter;
          return (
            <div key={a.id} className="artist-cell">
              {showAnchor ? (
                <span
                  className="letter-anchor"
                  ref={(el) => {
                    sectionRefs.current[letter] = el;
                  }}
                  data-letter={letter}
                />
              ) : null}
              <Link className="tile tile-cover-card" to={`/library/artists/${a.id}`}>
                <CoverThumb url={a.cover_url} label={a.name} />
                <h3>{a.name}</h3>
              </Link>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function ArtistDetail() {
  const { id } = useParams();
  const [name, setName] = useState("");
  const [albums, setAlbums] = useState<CatalogAlbum[]>([]);
  const [error, setError] = useState<string | null>(null);
  const { startRadio } = usePlayer();
  useEffect(() => {
    const n = Number(id);
    if (!n) return;
    void api
      .getArtist(n)
      .then((r) => {
        setName(r.name);
        setAlbums(r.albums);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, [id]);
  if (error) return <p className="error">{error}</p>;
  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 400 }}>{name}</h2>
      {id ? (
        <button
          type="button"
          className="btn"
          style={{ marginTop: "0.5rem" }}
          onClick={() => void startRadio({ seed_artist_id: Number(id) })}
        >
          Start radio from artist
        </button>
      ) : null}
      <div className="cover-grid" style={{ marginTop: "1rem" }}>
        {albums.map((al) => (
          <Link key={al.id} className="tile tile-cover-card" to={`/library/albums/${al.id}`}>
            <CoverThumb url={al.cover_url} label={al.title} />
            <h3>{al.title}</h3>
            <p>{al.year ?? "—"}</p>
          </Link>
        ))}
      </div>
    </div>
  );
}

export function AlbumsList() {
  const [albums, setAlbums] = useState<CatalogAlbum[]>(() => albumsListCache ?? []);
  const [error, setError] = useState<string | null>(null);
  useBrowseScrollRestore("library/albums", albums.length > 0);
  useEffect(() => {
    void api
      .listAlbums()
      .then((r) => {
        albumsListCache = r.albums;
        setAlbums(r.albums);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, []);
  if (error) return <p className="error">{error}</p>;
  return (
    <div className="cover-grid">
      {albums.map((al) => (
        <Link key={al.id} className="tile tile-cover-card" to={`/library/albums/${al.id}`}>
          <CoverThumb url={al.cover_url} label={al.title} />
          <h3>{al.title}</h3>
          <p>
            {al.artist}
            {al.year ? ` · ${al.year}` : ""}
          </p>
        </Link>
      ))}
    </div>
  );
}

export function AlbumDetail() {
  const { id } = useParams();
  const [album, setAlbum] = useState<CatalogAlbum | null>(null);
  const [tracks, setTracks] = useState<CatalogTrack[]>([]);
  const [error, setError] = useState<string | null>(null);
  const { startRadio } = usePlayer();
  useEffect(() => {
    const n = Number(id);
    if (!n) return;
    void api
      .getAlbum(n)
      .then((r) => {
        setAlbum(r.album);
        setTracks(r.tracks);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, [id]);
  if (error) return <p className="error">{error}</p>;
  if (!album) return <p className="muted">Loading…</p>;
  return (
    <div>
      <div className="detail-hero">
        {album.cover_url ? (
          <img className="cover" src={album.cover_url} alt="" />
        ) : (
          <div className="cover cover-fallback">LT</div>
        )}
        <div>
          <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 400, margin: 0 }}>
            {album.title}
          </h2>
          <p className="muted">
            {album.artist}
            {album.year ? ` · ${album.year}` : ""}
          </p>
          {tracks[0] ? (
            <button
              type="button"
              className="btn"
              style={{ marginTop: "0.75rem" }}
              onClick={() => void startRadio(tracks[0].id)}
            >
              Start radio from album
            </button>
          ) : null}
        </div>
      </div>
      <TrackTable tracks={tracks} showAlbum={false} showYear={false} />
    </div>
  );
}

export function TracksList() {
  const [q, setQ] = useState("");
  const [tracks, setTracks] = useState<CatalogTrack[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = (query = q) => {
    void api
      .listTracks({ limit: 300, q: query || undefined })
      .then((r) => setTracks(r.tracks))
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  };

  useEffect(() => {
    load("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSearch = (e: FormEvent) => {
    e.preventDefault();
    load(q);
  };

  return (
    <div>
      <form className="toolbar" onSubmit={onSearch}>
        <input
          type="search"
          placeholder="Search title, artist, album"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <button className="btn" type="submit">
          Search
        </button>
      </form>
      {error ? <p className="error">{error}</p> : <TrackTable tracks={tracks} />}
    </div>
  );
}

export function YearsList() {
  const [years, setYears] = useState<{ year: number; count: number }[]>(
    () => yearsListCache ?? [],
  );
  const [error, setError] = useState<string | null>(null);
  useBrowseScrollRestore("library/years", years.length > 0);
  useEffect(() => {
    void api
      .listYears()
      .then((r) => {
        yearsListCache = r.years;
        setYears(r.years);
      })
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, []);
  if (error) return <p className="error">{error}</p>;
  return (
    <div className="grid-list">
      {years.map((y) => (
        <Link key={y.year} className="tile" to={`/library/years/${y.year}`}>
          <h3>{y.year}</h3>
          <p>
            {y.count} track{y.count === 1 ? "" : "s"}
          </p>
        </Link>
      ))}
    </div>
  );
}

export function YearDetail() {
  const { year } = useParams();
  const [tracks, setTracks] = useState<CatalogTrack[]>([]);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    const y = Number(year);
    if (!y) return;
    void api
      .listTracksByYear(y)
      .then((r) => setTracks(r.tracks))
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, [year]);
  if (error) return <p className="error">{error}</p>;
  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 400 }}>Released {year}</h2>
      <TrackTable tracks={tracks} />
    </div>
  );
}

export function TrackDetailPage() {
  const { id } = useParams();
  const [track, setTrack] = useState<CatalogTrack | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { startRadio } = usePlayer();
  useEffect(() => {
    const n = Number(id);
    if (!n) return;
    void api
      .getTrack(n)
      .then(setTrack)
      .catch((e) => setError(e instanceof Error ? e.message : "failed"));
  }, [id]);
  if (error) return <p className="error">{error}</p>;
  if (!track) return <p className="muted">Loading…</p>;
  return (
    <div>
      <div className="detail-hero">
        {track.cover_url ? (
          <img className="cover" src={track.cover_url} alt="" />
        ) : (
          <div className="cover cover-fallback">LT</div>
        )}
        <div>
          <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 400, margin: 0 }}>
            {track.title}
          </h2>
          <p className="muted">
            {track.artist} · {track.album}
            {track.year ? ` · ${track.year}` : ""}
          </p>
          {track.genres ? <p className="muted">{track.genres}</p> : null}
          <div className="toolbar" style={{ marginTop: "0.75rem" }}>
            <button type="button" className="btn" onClick={() => void startRadio(track.id)}>
              Create radio station
            </button>
          </div>
        </div>
      </div>
      <TrackTable
        tracks={[track]}
        showArtist={false}
        showAlbum={false}
        showYear={false}
      />
    </div>
  );
}

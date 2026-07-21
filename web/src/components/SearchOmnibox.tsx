/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-20
 */

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { SearchSuggestion } from "../api/types";
import { usePlayer } from "../player/PlayerContext";

const RECENT_KEY = "lt_search_recent";

function loadRecent(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw) as unknown;
    return Array.isArray(arr) ? arr.filter((x) => typeof x === "string").slice(0, 8) : [];
  } catch {
    return [];
  }
}

function saveRecent(q: string) {
  const next = [q, ...loadRecent().filter((x) => x.toLowerCase() !== q.toLowerCase())].slice(0, 8);
  localStorage.setItem(RECENT_KEY, JSON.stringify(next));
}

function highlight(label: string, q: string) {
  const i = label.toLowerCase().indexOf(q.toLowerCase());
  if (i < 0 || !q) return label;
  return (
    <>
      {label.slice(0, i)}
      <mark>{label.slice(i, i + q.length)}</mark>
      {label.slice(i + q.length)}
    </>
  );
}

export function SearchOmnibox() {
  const navigate = useNavigate();
  const { startRadio } = usePlayer();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [hits, setHits] = useState<SearchSuggestion[]>([]);
  const [recent, setRecent] = useState<string[]>(() => loadRecent());
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  useEffect(() => {
    const trimmed = q.trim();
    abortRef.current?.abort();
    if (trimmed.length < 2) {
      setHits([]);
      setBusy(false);
      return;
    }
    const ac = new AbortController();
    abortRef.current = ac;
    setBusy(true);
    const t = window.setTimeout(() => {
      void api
        .searchSuggest(trimmed, 15, ac.signal)
        .then((r) => {
          setHits(r.suggestions);
          setActive(0);
          setBusy(false);
        })
        .catch((err) => {
          if (err instanceof DOMException && err.name === "AbortError") return;
          if (err?.name === "AbortError") return;
          setBusy(false);
        });
    }, 160);
    return () => {
      window.clearTimeout(t);
      ac.abort();
    };
  }, [q]);

  const goHit = async (h: SearchSuggestion) => {
    saveRecent(h.label);
    setRecent(loadRecent());
    setOpen(false);
    setQ("");
    if (h.kind === "artist") {
      navigate(`/library/artists/${h.id}`);
      return;
    }
    if (h.kind === "album") {
      navigate(`/library/albums/${h.id}`);
      return;
    }
    navigate(`/library/tracks/${h.track_id || h.id}`);
  };

  const radioFromHit = async (h: SearchSuggestion) => {
    if (h.kind === "track") {
      await startRadio(h.track_id || h.id);
      navigate("/now-playing");
      return;
    }
    if (h.kind === "artist") {
      await startRadio({ seed_artist_id: h.id });
      navigate("/now-playing");
      return;
    }
    if (h.kind === "album") {
      const al = await api.getAlbum(h.id);
      if (al.tracks[0]) {
        await startRadio(al.tracks[0].id);
        navigate("/now-playing");
      }
    }
  };

  const onKey = (e: KeyboardEvent) => {
    if (!open) return;
    const list = hits.length ? hits : [];
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => Math.min(i + 1, Math.max(0, list.length - 1)));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (list[active]) void goHit(list[active]);
      else if (q.trim().length >= 2) {
        saveRecent(q.trim());
        setRecent(loadRecent());
        navigate(`/library/tracks?q=${encodeURIComponent(q.trim())}`);
        setOpen(false);
      }
    } else if (e.key === "Escape") {
      setOpen(false);
    }
  };

  return (
    <div className="search-omnibox" ref={wrapRef}>
      <input
        type="search"
        className="search-omnibox-input"
        placeholder="Search tracks, artists, albums…"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={onKey}
        aria-autocomplete="list"
        aria-expanded={open}
      />
      {open ? (
        <div className="search-omnibox-panel" role="listbox">
          {q.trim().length < 2 ? (
            recent.length ? (
              <div className="search-omnibox-section">
                <div className="search-omnibox-heading">Recent</div>
                {recent.map((r) => (
                  <button
                    key={r}
                    type="button"
                    className="search-omnibox-row"
                    onClick={() => {
                      setQ(r);
                      setOpen(true);
                    }}
                  >
                    <span className="search-omnibox-meta">Recent</span>
                    <span>{r}</span>
                  </button>
                ))}
              </div>
            ) : (
              <p className="muted search-omnibox-empty">Type at least 2 characters</p>
            )
          ) : busy && !hits.length ? (
            <p className="muted search-omnibox-empty">Searching…</p>
          ) : hits.length === 0 ? (
            <p className="muted search-omnibox-empty">No matches</p>
          ) : (
            hits.map((h, i) => (
              <div
                key={`${h.kind}-${h.id}`}
                className={`search-omnibox-row ${i === active ? "active" : ""}`}
                role="option"
                aria-selected={i === active}
              >
                <button type="button" className="search-omnibox-main" onClick={() => void goHit(h)}>
                  {h.cover_url ? (
                    <img src={h.cover_url} alt="" className="search-omnibox-thumb" />
                  ) : (
                    <span className="search-omnibox-thumb placeholder" />
                  )}
                  <span className="search-omnibox-text">
                    <span className="search-omnibox-label">{highlight(h.label, q.trim())}</span>
                    <span className="muted">
                      <span className="search-omnibox-chip">{h.kind}</span>
                      {h.sublabel ? ` · ${h.sublabel}` : ""}
                    </span>
                  </span>
                </button>
                <button
                  type="button"
                  className="btn btn-ghost search-omnibox-radio"
                  title="Start radio"
                  onClick={() => void radioFromHit(h)}
                >
                  Radio
                </button>
              </div>
            ))
          )}
        </div>
      ) : null}
    </div>
  );
}

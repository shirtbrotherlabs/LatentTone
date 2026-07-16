/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { CatalogTrack } from "../api/types";
import { TrackActions, formatDuration } from "./TrackActions";

export type TrackTableRow = CatalogTrack & {
  track_id?: number;
  feedback?: "like" | "dislike" | string;
  play_count?: number;
};

type Props = {
  tracks: TrackTableRow[];
  showArtist?: boolean;
  showAlbum?: boolean;
  showYear?: boolean;
  showCover?: boolean;
  rowClassName?: (track: TrackTableRow, index: number) => string | undefined;
  renderTitleBadge?: (track: TrackTableRow) => ReactNode;
  renderTrailing?: (track: TrackTableRow, index: number) => ReactNode;
};

function trackId(t: TrackTableRow): number {
  return t.id ?? t.track_id ?? 0;
}

function ThumbUpMini({ filled }: { filled?: boolean }) {
  return (
    <svg className="track-table-thumb-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={filled ? 0 : 1.7}
        d="M10.8 20.2H6.4c-.9 0-1.6-.7-1.6-1.6v-6.1c0-.5.2-1 .6-1.3l5.2-4.5c.5-.4 1.2-.5 1.8-.2.7.3 1.1 1 1.1 1.8v2.3h3.7c1.3 0 2.3 1.2 2.1 2.5l-.9 5.2c-.2 1.1-1.2 1.9-2.3 1.9h-5.3z"
      />
    </svg>
  );
}

function ThumbDownMini({ filled }: { filled?: boolean }) {
  return (
    <svg className="track-table-thumb-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={filled ? 0 : 1.7}
        d="M13.2 3.8h4.4c.9 0 1.6.7 1.6 1.6v6.1c0 .5-.2 1-.6 1.3l-5.2 4.5c-.5.4-1.2.5-1.8.2-.7-.3-1.1-1-1.1-1.8v-2.3H6.8c-1.3 0-2.3-1.2-2.1-2.5l.9-5.2c.2-1.1 1.2-1.9 2.3-1.9h5.3z"
      />
    </svg>
  );
}

export function TrackTable({
  tracks,
  showArtist = true,
  showAlbum = true,
  showYear = true,
  showCover = false,
  rowClassName,
  renderTitleBadge,
  renderTrailing,
}: Props) {
  if (!tracks.length) {
    return <p className="muted">No tracks.</p>;
  }

  return (
    <table className="track-table">
      <thead>
        <tr>
          {showCover ? <th className="track-table-cover-col" aria-label="Cover" /> : null}
          <th>Title</th>
          {showArtist ? <th>Artist</th> : null}
          {showAlbum ? <th>Album</th> : null}
          {showYear ? <th className="track-table-year-col">Year</th> : null}
          <th className="track-table-duration-col">Time</th>
          <th className="track-table-plays-col">Plays</th>
          <th className="track-table-rating-col" aria-label="Rating" />
          <th className="track-table-actions-col" aria-label="Actions" />
          {renderTrailing ? <th className="track-table-trailing-col" aria-label="More" /> : null}
        </tr>
      </thead>
      <tbody>
        {tracks.map((t, idx) => {
          const id = trackId(t);
          const liked = t.feedback === "like";
          const disliked = t.feedback === "dislike";
          const plays = typeof t.play_count === "number" ? t.play_count : 0;
          const extraClass = rowClassName?.(t, idx);
          return (
            <tr key={`${id}-${idx}`} className={extraClass}>
              {showCover ? (
                <td className="track-table-cover-col">
                  {t.cover_url ? (
                    <img className="track-table-cover" src={t.cover_url} alt="" />
                  ) : (
                    <div className="track-table-cover track-table-cover-fallback" aria-hidden />
                  )}
                </td>
              ) : null}
              <td>
                <div className="track-table-title-line">
                  <Link className="track-title" to={`/library/tracks/${id}`}>
                    {t.title}
                  </Link>
                  {renderTitleBadge?.(t)}
                </div>
              </td>
              {showArtist ? (
                <td className="track-meta">
                  {t.artist_id ? (
                    <Link className="fp-link" to={`/library/artists/${t.artist_id}`}>
                      {t.artist}
                    </Link>
                  ) : (
                    t.artist
                  )}
                </td>
              ) : null}
              {showAlbum ? (
                <td className="track-meta">
                  {t.album_id ? (
                    <Link className="fp-link" to={`/library/albums/${t.album_id}`}>
                      {t.album}
                    </Link>
                  ) : (
                    t.album
                  )}
                </td>
              ) : null}
              {showYear ? <td className="track-meta track-table-year-col">{t.year ?? "—"}</td> : null}
              <td className="track-meta track-table-duration-col">{formatDuration(t.duration_ms)}</td>
              <td className="track-meta track-table-plays-col">{plays}</td>
              <td className="track-table-rating-col">
                <span
                  className="track-table-thumbs"
                  aria-label={liked ? "Liked" : disliked ? "Disliked" : "No rating"}
                >
                  <span className={liked ? "is-on" : undefined} title="Like">
                    <ThumbUpMini filled={liked} />
                  </span>
                  <span className={disliked ? "is-on" : undefined} title="Dislike">
                    <ThumbDownMini filled={disliked} />
                  </span>
                </span>
              </td>
              <td className="track-table-actions-col">
                <TrackActions track={{ ...t, id }} />
              </td>
              {renderTrailing ? (
                <td className="track-table-trailing-col">{renderTrailing(t, idx)}</td>
              ) : null}
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

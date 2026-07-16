/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

import { Link } from "react-router-dom";
import type { CatalogTrack } from "../api/types";
import { TrackActions, formatDuration } from "./TrackActions";

type Props = {
  tracks: CatalogTrack[];
  showArtist?: boolean;
  showAlbum?: boolean;
};

export function TrackTable({ tracks, showArtist = true, showAlbum = true }: Props) {
  if (!tracks.length) {
    return <p className="muted">No tracks.</p>;
  }
  return (
    <table className="track-table">
      <thead>
        <tr>
          <th>Title</th>
          {showArtist ? <th>Artist</th> : null}
          {showAlbum ? <th>Album</th> : null}
          <th>Year</th>
          <th>Time</th>
          <th aria-label="Actions" />
        </tr>
      </thead>
      <tbody>
        {tracks.map((t) => (
          <tr key={t.id}>
            <td>
              <Link className="track-title" to={`/library/tracks/${t.id}`}>
                {t.title}
              </Link>
            </td>
            {showArtist ? <td className="track-meta">{t.artist}</td> : null}
            {showAlbum ? <td className="track-meta">{t.album}</td> : null}
            <td className="track-meta">{t.year ?? "—"}</td>
            <td className="track-meta">{formatDuration(t.duration_ms)}</td>
            <td>
              <TrackActions track={t} />
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-17
 */

import { useState } from "react";
import { Link } from "react-router-dom";
import type { CatalogTrack, QueueTrack } from "../api/types";
import { TrackTable, type TrackTableRow } from "../components/TrackTable";
import { usePlayer } from "../player/PlayerContext";

function asTableRow(track: CatalogTrack | QueueTrack): TrackTableRow {
  const q = track as QueueTrack;
  return {
    ...track,
    id: track.id,
    feedback: q.feedback ?? track.feedback,
    play_count: q.play_count ?? track.play_count,
  };
}

export function ListenPage() {
  const {
    status,
    nowTrack,
    historyTracks,
    queueTracks,
    error,
    trackFeedback,
    removeFromQueue,
    feedback,
  } = usePlayer();
  const [localError, setLocalError] = useState<string | null>(null);

  const sessionActive = status && status.status !== "stopped";
  const trackId = nowTrack?.id ?? status?.now_playing?.track_id;
  const artistId = nowTrack?.artist_id;
  const albumId = nowTrack?.album_id;
  const currentAsTrack: QueueTrack | null = nowTrack
    ? {
        ...nowTrack,
        feedback: trackFeedback ?? status?.now_playing?.feedback ?? nowTrack.feedback,
        play_count: status?.now_playing?.play_count ?? nowTrack.play_count,
      }
    : trackId
      ? {
          id: trackId,
          title: `Track #${trackId}`,
          artist: "",
          album: "",
          feedback: trackFeedback ?? status?.now_playing?.feedback,
          play_count: status?.now_playing?.play_count,
        }
      : null;

  const onFeedback = (id: number, signal: "like" | "dislike" | "clear") => {
    setLocalError(null);
    void feedback(signal, id).catch((err) => {
      setLocalError(err instanceof Error ? err.message : "failed");
    });
  };

  return (
    <section>
      <h1 className="page-title page-title-sm">Now Playing</h1>

      {(localError || error) && <p className="error">{localError || error}</p>}

      {sessionActive ? (
        <div className="listen-now">
          <div className="np-header">
            {nowTrack?.cover_url ? (
              <img className="np-cover" src={nowTrack.cover_url} alt="" />
            ) : (
              <div className="np-cover np-cover-fallback" aria-hidden>
                LT
              </div>
            )}
            <div className="np-meta">
              <div className="np-label">Now playing</div>
              <h2 className="np-title">
                {trackId ? (
                  <Link className="fp-link" to={`/library/tracks/${trackId}`}>
                    {nowTrack?.title || `Track #${trackId}`}
                  </Link>
                ) : (
                  "…"
                )}
              </h2>
              <p className="np-links">
                {artistId ? (
                  <Link className="fp-link" to={`/library/artists/${artistId}`}>
                    {nowTrack?.artist || "Artist"}
                  </Link>
                ) : (
                  <span>{nowTrack?.artist || "Loading…"}</span>
                )}
                {nowTrack?.album ? (
                  <>
                    <span className="muted"> · </span>
                    {albumId ? (
                      <Link className="fp-link" to={`/library/albums/${albumId}`}>
                        {nowTrack.album}
                      </Link>
                    ) : (
                      <span>{nowTrack.album}</span>
                    )}
                  </>
                ) : null}
              </p>
            </div>
          </div>

          <div className="session-timeline">
            {historyTracks.length > 0 ? (
              <>
                <h2 className="queue-heading">Recently played</h2>
                <TrackTable
                  tracks={historyTracks.map(asTableRow)}
                  rowClassName={() => "track-table-row-past"}
                  onFeedback={onFeedback}
                />
              </>
            ) : null}

            {currentAsTrack ? (
              <>
                <h2 className="queue-heading">Now</h2>
                <TrackTable
                  tracks={[asTableRow(currentAsTrack)]}
                  rowClassName={() => "track-table-row-now"}
                  renderTitleBadge={() => <span className="queue-badge">Playing</span>}
                  onFeedback={onFeedback}
                />
              </>
            ) : null}

            <h2 className="queue-heading">Up next</h2>
            {queueTracks.length === 0 ? (
              <p className="muted">No upcoming tracks yet — affinity will fill the queue.</p>
            ) : (
              <TrackTable
                tracks={queueTracks.map(asTableRow)}
                rowClassName={() => "track-table-row-next"}
                onFeedback={onFeedback}
                renderTrailing={(t) => (
                  <button
                    type="button"
                    className="queue-remove-btn"
                    aria-label={`Remove ${t.title} from up next`}
                    title="Remove from up next"
                    onClick={() => {
                      setLocalError(null);
                      void removeFromQueue(t.id).catch((err) => {
                        setLocalError(err instanceof Error ? err.message : "failed");
                      });
                    }}
                  >
                    ×
                  </button>
                )}
              />
            )}
          </div>
        </div>
      ) : (
        <p className="muted">
          No active station. Start one from <Link to="/radio">Radio</Link>.
        </p>
      )}
    </section>
  );
}

/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-17
 */

import { useEffect, useState, type ReactNode } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { api } from "./api/client";
import { useAuth } from "./auth/AuthContext";
import { Shell } from "./components/Shell";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { ListenPage } from "./pages/ListenPage";
import { RadioPage } from "./pages/RadioPage";
import { SettingsPage } from "./pages/SettingsPage";
import { LibraryPage } from "./pages/LibraryPage";
import {
  AlbumDetail,
  AlbumsList,
  ArtistDetail,
  ArtistsList,
  TrackDetailPage,
  TracksList,
  YearDetail,
  YearsList,
} from "./pages/BrowseLists";
import { PlaylistDetailPage, PlaylistsPage } from "./pages/PlaylistsPage";
import { AboutPage } from "./pages/AboutPage";

function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  if (loading) {
    return (
      <div className="auth-layout">
        <p className="muted">Loading…</p>
      </div>
    );
  }
  if (!user) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

/** Prefer Now Playing when the user already has stations; otherwise Radio. */
function HomeRedirect() {
  const [to, setTo] = useState("/now-playing");
  const [ready, setReady] = useState(false);
  useEffect(() => {
    let cancelled = false;
    const t = window.setTimeout(() => {
      if (!cancelled) setReady(true);
    }, 800);
    void api
      .listStations(1)
      .then((r) => {
        if (cancelled) return;
        setTo(r.stations.length === 0 ? "/radio" : "/now-playing");
        setReady(true);
      })
      .catch(() => {
        if (!cancelled) setReady(true);
      });
    return () => {
      cancelled = true;
      window.clearTimeout(t);
    };
  }, []);
  if (!ready) {
    return <p className="muted">Loading…</p>;
  }
  return <Navigate to={to} replace />;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Shell />
          </RequireAuth>
        }
      >
        <Route index element={<HomeRedirect />} />
        <Route path="now-playing" element={<ListenPage />} />
        <Route path="radio" element={<RadioPage />} />
        <Route path="listen" element={<Navigate to="/now-playing" replace />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="library" element={<LibraryPage />}>
          <Route index element={<Navigate to="artists" replace />} />
          <Route path="artists" element={<ArtistsList />} />
          <Route path="artists/:id" element={<ArtistDetail />} />
          <Route path="albums" element={<AlbumsList />} />
          <Route path="albums/:id" element={<AlbumDetail />} />
          <Route path="tracks" element={<TracksList />} />
          <Route path="tracks/:id" element={<TrackDetailPage />} />
          <Route path="years" element={<YearsList />} />
          <Route path="years/:year" element={<YearDetail />} />
        </Route>
        <Route path="playlists" element={<PlaylistsPage />} />
        <Route path="playlists/:id" element={<PlaylistDetailPage />} />
        <Route path="about" element={<AboutPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

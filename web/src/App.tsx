/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

import type { ReactNode } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth/AuthContext";
import { Shell } from "./components/Shell";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { ListenPage } from "./pages/ListenPage";
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
        <Route index element={<Navigate to="/now-playing" replace />} />
        <Route path="now-playing" element={<ListenPage />} />
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

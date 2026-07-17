/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-17
 */

import { NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { FloatingPlayer } from "../player/FloatingPlayer";

export function Shell() {
  const { user, logout } = useAuth();

  return (
    <div className="shell">
      <aside className="nav">
        <div className="brand">
          LatentTone
          <span>Now Playing · Radio · Library</span>
        </div>
        <nav className="nav-links">
          <NavLink
            to="/now-playing"
            className={({ isActive }) => (isActive ? "active" : undefined)}
          >
            Now Playing
          </NavLink>
          <NavLink to="/radio" className={({ isActive }) => (isActive ? "active" : undefined)}>
            Radio
          </NavLink>
          <NavLink
            to="/library"
            className={({ isActive }) => (isActive ? "active" : undefined)}
          >
            Library
          </NavLink>
          <NavLink
            to="/playlists"
            className={({ isActive }) => (isActive ? "active" : undefined)}
          >
            Playlists
          </NavLink>
          <NavLink
            to="/settings"
            className={({ isActive }) => (isActive ? "active" : undefined)}
          >
            Settings
          </NavLink>
          <NavLink to="/about" className={({ isActive }) => (isActive ? "active" : undefined)}>
            About
          </NavLink>
        </nav>
        <div className="nav-foot">
          <div>{user?.username}</div>
          <button type="button" className="btn btn-ghost" style={{ marginTop: "0.5rem" }} onClick={() => void logout()}>
            Log out
          </button>
        </div>
      </aside>
      <main className="main">
        <Outlet />
      </main>
      {/* Persistent player lives outside the route outlet */}
      <FloatingPlayer />
    </div>
  );
}

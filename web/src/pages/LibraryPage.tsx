/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-15
 */

import { NavLink, Outlet } from "react-router-dom";

export function LibraryPage() {
  return (
    <section>
      <h1 className="page-title">Library</h1>
      <p className="page-lead">Browse by artist, album, track, or release year.</p>
      <div className="tabs">
        <NavLink to="/library/artists" className={({ isActive }) => (isActive ? "active" : undefined)}>
          By Artist
        </NavLink>
        <NavLink to="/library/albums" className={({ isActive }) => (isActive ? "active" : undefined)}>
          By Album
        </NavLink>
        <NavLink to="/library/tracks" className={({ isActive }) => (isActive ? "active" : undefined)}>
          By Track
        </NavLink>
        <NavLink to="/library/years" className={({ isActive }) => (isActive ? "active" : undefined)}>
          By Year (Released)
        </NavLink>
      </div>
      <Outlet />
    </section>
  );
}

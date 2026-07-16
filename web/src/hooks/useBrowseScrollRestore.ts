/*
 * Copyright (C) 2026 martinsah
 * SPDX-License-Identifier: GPL-3.0-only
 * Author: martinsah
 * Date: 2026-07-16
 */

import { useEffect, useRef } from "react";
import { useNavigationType } from "react-router-dom";

const STORAGE_PREFIX = "lt_browse_scroll:";

function readY(key: string): number {
  try {
    const raw = sessionStorage.getItem(STORAGE_PREFIX + key);
    const y = raw == null ? 0 : Number(raw);
    return Number.isFinite(y) && y > 0 ? y : 0;
  } catch {
    return 0;
  }
}

function writeY(key: string, y: number) {
  try {
    if (y > 0) {
      sessionStorage.setItem(STORAGE_PREFIX + key, String(Math.round(y)));
    } else {
      sessionStorage.removeItem(STORAGE_PREFIX + key);
    }
  } catch {
    /* ignore quota / private mode */
  }
}

/**
 * Restore window scroll for library browse lists on browser Back (POP).
 * Saves position while the list is mounted; ignores StrictMode remounts that
 * would otherwise clobber a saved offset with scrollY === 0.
 */
export function useBrowseScrollRestore(key: string, ready: boolean) {
  const navigationType = useNavigationType();
  const didRestore = useRef(false);

  useEffect(() => {
    const onScroll = () => writeY(key, window.scrollY);
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      // Only persist a non-zero offset on unmount so React StrictMode's
      // immediate remount at the top does not erase the saved Back target.
      const y = window.scrollY;
      if (y > 0) writeY(key, y);
    };
  }, [key]);

  useEffect(() => {
    if (!ready || didRestore.current) return;
    didRestore.current = true;

    if (navigationType !== "POP") {
      window.scrollTo(0, 0);
      return;
    }

    const y = readY(key);
    if (y <= 0) return;

    const apply = () => window.scrollTo(0, y);
    requestAnimationFrame(() => {
      requestAnimationFrame(apply);
    });
    // Covers lazy cover images shifting layout shortly after first paint.
    const t = window.setTimeout(apply, 80);
    return () => window.clearTimeout(t);
  }, [ready, navigationType, key]);
}

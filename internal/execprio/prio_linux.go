// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-17

//go:build linux

package execprio

import "syscall"

func lowerPID(pid int) {
	// Best-effort: ignore errors (e.g. permission / already exited).
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, BackgroundNice)
}

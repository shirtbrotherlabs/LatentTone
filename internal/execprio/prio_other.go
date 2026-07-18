// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-17

//go:build !linux

package execprio

func lowerPID(pid int) {}

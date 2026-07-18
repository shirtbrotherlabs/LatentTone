// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-17

package meta

import (
	"runtime"
	"testing"
)

func TestCapEmbedWorkersReservesCores(t *testing.T) {
	n := runtime.NumCPU()
	got := CapEmbedWorkers(n + 10)
	if got > n {
		t.Fatalf("cap %d exceeds NumCPU %d", got, n)
	}
	if CapEmbedWorkers(0) != 1 {
		t.Fatal("want minimum 1")
	}
	if CapEmbedWorkers(1) != 1 {
		t.Fatal("want 1 unchanged")
	}
}

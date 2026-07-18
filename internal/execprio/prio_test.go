// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-17

package execprio

import (
	"os/exec"
	"testing"
)

func TestRunTrue(t *testing.T) {
	cmd := exec.Command("true")
	if err := Run(cmd); err != nil {
		t.Fatal(err)
	}
}

// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-17

// Package execprio runs background subprocesses at reduced CPU priority so
// latency-sensitive work (on-demand FFmpeg playback) can preempt embed/ML jobs.
package execprio

import (
	"fmt"
	"os"
	"os/exec"
)

// BackgroundNice is the niceness applied to embed/ML/Lance helpers on Linux.
// Higher values yield the CPU more readily to interactive playback encodes.
const BackgroundNice = 15

// Run starts cmd, lowers its priority when supported, then waits.
// On non-Linux platforms it is equivalent to cmd.Run().
func Run(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("execprio: nil command")
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		Lower(cmd.Process)
	}
	return cmd.Wait()
}

// Lower renices an already-started process when the OS supports it.
func Lower(p *os.Process) {
	if p == nil {
		return
	}
	lowerPID(p.Pid)
}

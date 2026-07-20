// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package extract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMLPoolPingShutdown(t *testing.T) {
	helper := findHelper(t, "ml_embed_helper.py")
	py := pythonBin()
	if _, err := exec.LookPath(py); err != nil {
		t.Skip("python not available")
	}
	// Pool start requires ping; models are not loaded until first yamnet/musicnn.
	pool, err := StartMLPool(MLHelperConfig{Python: py, HelperPath: helper}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Call with missing audio should fail fast from the warm helper, not spawn a new process.
	_, err = pool.Call(ctx, "yamnet", "/nonexistent.flac", "/models/yamnet/yamnet.tflite", "/models/yamnet/yamnet_class_map.csv", "")
	if err == nil {
		t.Fatal("expected error for missing audio")
	}
}

func findHelper(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "scripts", name),
		filepath.Join("scripts", name),
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "scripts", name),
			filepath.Join(wd, "..", "..", "scripts", name),
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	t.Skipf("%s not found", name)
	return ""
}

func pythonBin() string {
	if v := os.Getenv("LATENTTONE_PYTHON"); v != "" {
		return v
	}
	return "python3"
}

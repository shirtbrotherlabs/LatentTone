// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package lance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterBatchFlush(t *testing.T) {
	helper := findLanceHelper(t)
	py := pythonBin()
	if _, err := exec.LookPath(py); err != nil {
		t.Skip("python not available")
	}
	// Need lancedb installed in that python.
	if err := exec.Command(py, "-c", "import lancedb").Run(); err != nil {
		t.Skip("lancedb not installed in python")
	}
	dir := t.TempDir()
	s := &Store{
		DBPath:     filepath.Join(dir, "ldb"),
		Table:      "track_vectors",
		HelperPath: helper,
		Python:     py,
	}
	w, err := s.StartWriter(2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	vec := make([]float32, 4)
	vec[0] = 1
	// Parallel upserts fill batch_size=2 and flush together (sequential would wait forever).
	errCh := make(chan error, 2)
	go func() {
		_, err := w.Upsert(ctx, 1, vec)
		errCh <- err
	}()
	go func() {
		_, err := w.Upsert(ctx, 2, vec)
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.Search(ctx, vec, 5, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected >=2 hits after batch flush, got %d", len(hits))
	}
}

func findLanceHelper(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "scripts", "lance_helper.py"),
		filepath.Join("scripts", "lance_helper.py"),
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "..", "..", "scripts", "lance_helper.py"))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	t.Skip("lance_helper.py not found")
	return ""
}

func pythonBin() string {
	if v := os.Getenv("LATENTTONE_PYTHON"); v != "" {
		return v
	}
	return "python3"
}

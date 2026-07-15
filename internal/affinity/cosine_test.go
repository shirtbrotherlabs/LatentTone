// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package affinity

import (
	"math"
	"testing"
)

func TestCosineOrthonormal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	if s := Cosine(a, b); math.Abs(s) > 1e-6 {
		t.Fatalf("got %v", s)
	}
	if s := Cosine(a, a); math.Abs(s-1) > 1e-6 {
		t.Fatalf("self %v", s)
	}
}

func TestCosineKnownAngle(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{float32(math.Cos(math.Pi / 4)), float32(math.Sin(math.Pi / 4))}
	s := Cosine(a, b)
	want := math.Cos(math.Pi / 4)
	if math.Abs(s-want) > 1e-5 {
		t.Fatalf("got %v want %v", s, want)
	}
}

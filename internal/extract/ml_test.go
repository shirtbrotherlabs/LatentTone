// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package extract

import "testing"

func TestMLBlockL2(t *testing.T) {
	v := make([]float32, 200)
	for i := range v {
		v[i] = float32(i + 1)
	}
	fixed := make([]float32, mlBlockDim)
	copy(fixed, v[:mlBlockDim])
	L2Normalize(fixed)
	var sum float64
	for _, x := range fixed {
		sum += float64(x) * float64(x)
	}
	if sum < 0.99 || sum > 1.01 {
		t.Fatalf("expected unit L2, got %v", sum)
	}
}

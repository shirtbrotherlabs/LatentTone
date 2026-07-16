// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db_test

import (
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestRadioPrefsPersist(t *testing.T) {
	catalog, _ := dbtest.Open(t)
	u, err := catalog.CreateUser("radio", "hash")
	if err != nil {
		t.Fatal(err)
	}
	def, err := catalog.GetRadioPrefs(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !def.RadioBridge || !def.ArtistCooldown || !def.QueryJitter || !def.ArtistPenalty || !def.BoundedRandom {
		t.Fatalf("defaults should enable diversification: %#v", def)
	}
	if def.JitterAlpha != 0.05 {
		t.Fatalf("jitter alpha %#v", def.JitterAlpha)
	}
	def.RadioBridge = false
	def.JitterAlpha = 0.12
	saved, err := catalog.UpsertRadioPrefs(def)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RadioBridge || saved.JitterAlpha != 0.12 {
		t.Fatalf("upsert %#v", saved)
	}
	again, err := catalog.GetRadioPrefs(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.RadioBridge || again.JitterAlpha != 0.12 {
		t.Fatalf("persist %#v", again)
	}
}

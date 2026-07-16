// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db_test

import (
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
)

func TestStreamPrefsPersist(t *testing.T) {
	catalog, _ := dbtest.Open(t)

	u, err := catalog.CreateUser("streamuser", "hash")
	if err != nil {
		t.Fatal(err)
	}
	def, err := catalog.GetStreamPrefs(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if def.StreamFormat != db.StreamFormatOriginal || def.BitrateKbps != db.DefaultStreamBitrateKbps {
		t.Fatalf("defaults %#v", def)
	}

	def.StreamFormat = db.StreamFormatMP3
	def.BitrateKbps = 256
	saved, err := catalog.UpsertStreamPrefs(def)
	if err != nil {
		t.Fatal(err)
	}
	if saved.StreamFormat != db.StreamFormatMP3 || saved.BitrateKbps != 256 {
		t.Fatalf("saved %#v", saved)
	}
	again, err := catalog.GetStreamPrefs(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.StreamFormat != db.StreamFormatMP3 || again.BitrateKbps != 256 {
		t.Fatalf("reload %#v", again)
	}
}

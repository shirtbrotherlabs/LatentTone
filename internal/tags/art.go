// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package tags

import (
	"encoding/binary"
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
	flac "github.com/go-flac/go-flac/v2"
)

// EmbeddedArt holds a picture extracted from an audio file's tags.
type EmbeddedArt struct {
	Data     []byte
	MimeType string
}

// Ext returns a file extension (with leading dot) for the art's MIME type.
func (a EmbeddedArt) Ext() string {
	switch strings.ToLower(strings.TrimSpace(a.MimeType)) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ".jpg"
	}
}

// ExtractArt reads embedded cover art from an audio file, preferring the
// front-cover picture. Returns ok=false when no usable picture is present.
func ExtractArt(absPath string) (EmbeddedArt, bool) {
	switch strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), ".") {
	case "mp3":
		return extractMP3Art(absPath)
	case "flac":
		return extractFLACArt(absPath)
	default:
		return EmbeddedArt{}, false
	}
}

func extractMP3Art(path string) (EmbeddedArt, bool) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true, ParseFrames: []string{"APIC"}})
	if err != nil {
		return EmbeddedArt{}, false
	}
	defer tag.Close()

	frames := tag.GetFrames(tag.CommonID("Attached picture"))
	var fallback *id3v2.PictureFrame
	for i := range frames {
		pf, ok := frames[i].(id3v2.PictureFrame)
		if !ok || len(pf.Picture) == 0 {
			continue
		}
		// PictureType 3 == front cover.
		if pf.PictureType == 3 {
			return EmbeddedArt{Data: pf.Picture, MimeType: pf.MimeType}, true
		}
		if fallback == nil {
			cp := pf
			fallback = &cp
		}
	}
	if fallback != nil {
		return EmbeddedArt{Data: fallback.Picture, MimeType: fallback.MimeType}, true
	}
	return EmbeddedArt{}, false
}

func extractFLACArt(path string) (EmbeddedArt, bool) {
	f, err := flac.ParseFile(path)
	if err != nil {
		return EmbeddedArt{}, false
	}
	var fallback *EmbeddedArt
	for _, b := range f.Meta {
		if b == nil || b.Type != flac.Picture {
			continue
		}
		art, ptype, ok := parseFLACPicture(b.Data)
		if !ok {
			continue
		}
		if ptype == 3 {
			return art, true
		}
		if fallback == nil {
			cp := art
			fallback = &cp
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return EmbeddedArt{}, false
}

// parseFLACPicture decodes a METADATA_BLOCK_PICTURE payload (same layout as an
// ID3v2 APIC frame body): type, mime, description, geometry, then image bytes.
func parseFLACPicture(data []byte) (EmbeddedArt, uint32, bool) {
	pos := 0
	readU32 := func() (uint32, bool) {
		if pos+4 > len(data) {
			return 0, false
		}
		v := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4
		return v, true
	}
	readBytes := func(n uint32) ([]byte, bool) {
		if uint64(pos)+uint64(n) > uint64(len(data)) {
			return nil, false
		}
		b := data[pos : pos+int(n)]
		pos += int(n)
		return b, true
	}

	ptype, ok := readU32()
	if !ok {
		return EmbeddedArt{}, 0, false
	}
	mimeLen, ok := readU32()
	if !ok {
		return EmbeddedArt{}, 0, false
	}
	mime, ok := readBytes(mimeLen)
	if !ok {
		return EmbeddedArt{}, 0, false
	}
	descLen, ok := readU32()
	if !ok {
		return EmbeddedArt{}, 0, false
	}
	if _, ok := readBytes(descLen); !ok { // description (unused)
		return EmbeddedArt{}, 0, false
	}
	// width, height, color depth, number of colors.
	for i := 0; i < 4; i++ {
		if _, ok := readU32(); !ok {
			return EmbeddedArt{}, 0, false
		}
	}
	dataLen, ok := readU32()
	if !ok {
		return EmbeddedArt{}, 0, false
	}
	pic, ok := readBytes(dataLen)
	if !ok || len(pic) == 0 {
		return EmbeddedArt{}, 0, false
	}
	return EmbeddedArt{Data: pic, MimeType: string(mime)}, ptype, true
}

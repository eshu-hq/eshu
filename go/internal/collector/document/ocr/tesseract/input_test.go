// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

func TestNewRejectsDigestMismatchAndUnsafeConfig(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	zeros := strings.Repeat("0", 64)

	cases := []struct {
		name   string
		mutate func(*Config)
		want   Outcome
	}{
		{name: "binary_digest_mismatch", mutate: func(c *Config) { c.BinarySHA256 = zeros }, want: OutcomeDigestMismatch},
		{name: "model_digest_mismatch", mutate: func(c *Config) { c.ModelSHA256 = zeros }, want: OutcomeDigestMismatch},
		{name: "binary_digest_missing", mutate: func(c *Config) { c.BinarySHA256 = "" }, want: OutcomeConfigInvalid},
		{name: "model_digest_malformed", mutate: func(c *Config) { c.ModelSHA256 = "sha256:xyz" }, want: OutcomeConfigInvalid},
		{name: "relative_binary", mutate: func(c *Config) { c.BinaryPath = "tesseract" }, want: OutcomeConfigInvalid},
		{name: "missing_binary", mutate: func(c *Config) { c.BinaryPath = filepath.Join(fx.tessdata, "absent") }, want: OutcomeConfigInvalid},
		{name: "missing_model", mutate: func(c *Config) { c.TessdataDir = t.TempDir() }, want: OutcomeConfigInvalid},
		{name: "missing_version", mutate: func(c *Config) { c.EngineVersion = " " }, want: OutcomeConfigInvalid},
		{name: "unsupported_language", mutate: func(c *Config) { c.Language = "eng+deu" }, want: OutcomeConfigInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := fx.config()
			tc.mutate(&cfg)
			engine, err := New(cfg)
			if engine != nil {
				t.Fatal("New returned an engine for an invalid config")
			}
			assertOutcome(t, err, tc.want)
		})
	}
}

func TestNewAcceptsPrefixedUppercaseDigest(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	cfg := fx.config()
	cfg.BinarySHA256 = "sha256:" + strings.ToUpper(fx.binaryDigest)
	if _, err := New(cfg); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestRecognizeRejectsHostileInputBeforeExec(t *testing.T) {
	t.Parallel()
	engine, _ := newTestEngine(t, func(cfg *Config) {
		cfg.MaxInputBytes = 1 << 20
		cfg.MaxPixels = 1_000_000
	})

	cases := []struct {
		name  string
		image ocr.Image
	}{
		{name: "empty", image: ocr.Image{}},
		{name: "random_bytes", image: ocr.Image{Body: []byte("\x00\x01not-an-image FAKEOCR:ok ")}},
		{name: "svg_text", image: ocr.Image{Body: []byte("<svg xmlns='http://www.w3.org/2000/svg'/> FAKEOCR:ok ")}},
		{name: "tiff", image: ocr.Image{Body: []byte("II*\x00FAKEOCR:ok ")}},
		{name: "oversize_bytes", image: ocr.Image{Body: append(fakePNG(t, 10, 10, "ok"), make([]byte, 1<<20)...)}},
		{name: "pixel_bomb_png", image: ocr.Image{Body: pngHeader(100_000, 100_000)}},
		{name: "pixel_bomb_webp", image: ocr.Image{Body: fakeWebP(16384, 16384, "ok")}},
		{name: "zero_dimension_png", image: ocr.Image{Body: pngHeader(0, 10)}},
		{name: "truncated_png", image: ocr.Image{Body: fakePNG(t, 10, 10, "ok")[:20]}},
		{name: "truncated_webp", image: ocr.Image{Body: fakeWebP(10, 10, "ok")[:18]}},
		{name: "riff_not_webp", image: ocr.Image{Body: []byte("RIFF\x10\x00\x00\x00WAVEfmt FAKEOCR:ok ")}},
		{name: "format_mismatch", image: ocr.Image{Format: "jpeg", Body: fakePNG(t, 10, 10, "ok")}},
		{name: "unknown_format_label", image: ocr.Image{Format: "bmp", Body: fakePNG(t, 10, 10, "ok")}},
		{name: "frame_index_nonzero", image: ocr.Image{FrameIndex: 1, Body: fakePNG(t, 10, 10, "ok")}},
	}
	for _, tc := range cases {
		_, err := engine.Recognize(context.Background(), tc.image)
		if err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
		if got := OutcomeOf(err); got != OutcomeInputRejected {
			t.Fatalf("%s: outcome = %q, want input_rejected (err=%v)", tc.name, got, err)
		}
	}
	if got := engine.execCount.Load(); got != 0 {
		t.Fatalf("hostile inputs reached exec %d times, want 0", got)
	}
}

func TestSniffImageReadsDimensions(t *testing.T) {
	t.Parallel()

	format, width, height, err := sniffImage(fakeWebP(321, 123, "ok"))
	if err != nil || format != "webp" || width != 321 || height != 123 {
		t.Fatalf("lossless webp = %q %dx%d err=%v", format, width, height, err)
	}
	format, width, height, err = sniffImage(webpLossy(640, 480))
	if err != nil || format != "webp" || width != 640 || height != 480 {
		t.Fatalf("lossy webp = %q %dx%d err=%v", format, width, height, err)
	}
	format, width, height, err = sniffImage(webpExtended(1000, 700))
	if err != nil || format != "webp" || width != 1000 || height != 700 {
		t.Fatalf("extended webp = %q %dx%d err=%v", format, width, height, err)
	}
	format, width, height, err = sniffImage(fakePNG(t, 7, 5, "ok"))
	if err != nil || format != "png" || width != 7 || height != 5 {
		t.Fatalf("png = %q %dx%d err=%v", format, width, height, err)
	}
}

// pngHeader builds a PNG signature plus a valid IHDR declaring the given
// dimensions and no image data, enough for image.DecodeConfig.
func pngHeader(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8], ihdr[9] = 8, 0
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	buf.Write(chunk)
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	buf.WriteString("FAKEOCR:ok ")
	return buf.Bytes()
}

func webpLossy(width, height int) []byte {
	payload := []byte{
		0x10, 0x00, 0x00, 0x9d, 0x01, 0x2a,
		byte(width), byte(width >> 8), byte(height), byte(height >> 8),
	}
	return riffWebP("VP8 ", payload)
}

func webpExtended(width, height int) []byte {
	payload := make([]byte, 10)
	w, h := uint32(width-1), uint32(height-1)
	payload[4], payload[5], payload[6] = byte(w), byte(w>>8), byte(w>>16)
	payload[7], payload[8], payload[9] = byte(h), byte(h>>8), byte(h>>16)
	return riffWebP("VP8X", payload)
}

func riffWebP(fourcc string, payload []byte) []byte {
	chunk := append([]byte(fourcc), le32(uint32(len(payload)))...)
	chunk = append(chunk, payload...)
	out := append([]byte("RIFF"), le32(uint32(4+len(chunk)))...)
	out = append(out, []byte("WEBP")...)
	return append(out, chunk...)
}

// TestFixtureBinaryIsRegularFile guards the fixture assumption that the test
// binary is a regular executable file the engine can hash and exec.
func TestFixtureBinaryIsRegularFile(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	info, err := os.Stat(fx.binary)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("test binary %q not a regular file: %v", fx.binary, err)
	}
}

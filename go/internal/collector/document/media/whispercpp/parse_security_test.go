// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

// Parsing-code security pins for the miniaudio/ggml review (#9729 gate-2
// residual). Every claim in the README "Parsing security review" section is
// pinned here: hostile WAV structure fails closed with ClassMalformedMedia
// before any sample math or allocation keyed on a declared size, boundary
// rates and layouts hold, normalization output always re-parses (so the
// miniaudio decoder in the child only ever sees bytes this package wrote),
// the resample tap table stays bounded for adversarial rates, engine JSON
// output edges fail closed without quoting the output, and bracketed
// annotations classify as recorded.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

func mustParseWAV(t *testing.T, body []byte) wavInfo {
	t.Helper()
	info, err := parseWAV(body)
	if err != nil {
		t.Fatalf("parseWAV() error = %v", err)
	}
	return info
}

func mustFailMalformed(t *testing.T, body []byte) {
	t.Helper()
	info, err := parseWAV(body)
	if err == nil {
		t.Fatalf("parseWAV() error = nil, want malformed media (info %+v)", info)
	}
	if got := ClassOf(err); got != ClassMalformedMedia {
		t.Fatalf("ClassOf(%v) = %q, want malformed_media", err, got)
	}
}

func TestParseWAVRejectsDataBeforeFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(28))
	buf.WriteString("WAVEdata")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.Write([]byte{0, 0, 0, 0})
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	buf.Write(make([]byte, 16))
	mustFailMalformed(t, buf.Bytes())
}

func TestParseWAVSkipsUnknownChunksAndTrailingBytes(t *testing.T) {
	t.Parallel()
	valid := encodeWAV(t, 16000, 1, 16, 100)
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")
	buf.WriteString("LIST")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.WriteString("abcd")
	buf.Write(valid[12:])
	buf.WriteString("TRAILING")
	info := mustParseWAV(t, buf.Bytes())
	if got, want := info.durationMillis(), int64(100)*1000/16000; got != want {
		t.Fatalf("durationMillis = %d, want %d", got, want)
	}
	if !bytes.Equal(info.data, valid[44:]) {
		t.Fatalf("data chunk is not the original samples")
	}
}

func TestParseWAVAcceptsOddSizedChunkPadding(t *testing.T) {
	t.Parallel()
	valid := encodeWAV(t, 16000, 1, 16, 50)
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(17))
	buf.Write(valid[20:36])
	buf.WriteByte(0xab)
	buf.WriteByte(0x00)
	buf.Write(valid[36:])
	info := mustParseWAV(t, buf.Bytes())
	if got, want := info.frames(), int64(50); got != want {
		t.Fatalf("frames = %d, want %d", got, want)
	}
}

func TestParseWAVEnforcesRateChannelAndWidthBounds(t *testing.T) {
	t.Parallel()
	mutateRate := func(rate uint32) []byte {
		body := append([]byte(nil), encodeWAV(t, 16000, 1, 16, 10)...)
		binary.LittleEndian.PutUint32(body[24:], rate)
		return body
	}
	for _, rate := range []uint32{4000, 192000} {
		if _, err := parseWAV(mutateRate(rate)); err != nil {
			t.Fatalf("parseWAV(rate %d) error = %v, want accepted", rate, err)
		}
	}
	for _, rate := range []uint32{0, 1, 3999, 192001} {
		mustFailMalformed(t, mutateRate(rate))
	}
	mutateChannels := func(channels uint16) []byte {
		body := append([]byte(nil), encodeWAV(t, 16000, 1, 16, 10)...)
		binary.LittleEndian.PutUint16(body[22:], channels)
		return body
	}
	if _, err := parseWAV(encodeWAV(t, 16000, 8, 16, 10)); err != nil {
		t.Fatalf("parseWAV(8 channels) error = %v, want accepted", err)
	}
	mustFailMalformed(t, mutateChannels(0))
	mustFailMalformed(t, mutateChannels(9))
	mutateBits := func(bits uint16) []byte {
		body := append([]byte(nil), encodeWAV(t, 16000, 1, 16, 10)...)
		binary.LittleEndian.PutUint16(body[34:], bits)
		return body
	}
	mustFailMalformed(t, mutateBits(0))
	mustFailMalformed(t, mutateBits(7))
	mustFailMalformed(t, mutateBits(64))
	for _, bits := range []int{8, 16, 24, 32} {
		mustParseWAV(t, encodeWAV(t, 16000, 1, bits, 10))
	}
}

func TestParseWAVRiffSizeFieldIsNotTrusted(t *testing.T) {
	t.Parallel()
	for _, size := range []uint32{8, 0x7fffffff} {
		body := append([]byte(nil), encodeWAV(t, 16000, 1, 16, 10)...)
		binary.LittleEndian.PutUint32(body[4:], size)
		mustParseWAV(t, body)
	}
}

func TestParseWAVRejectsShortExtensibleFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(40))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(24))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE))
	buf.Write(make([]byte, 22))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	mustFailMalformed(t, buf.Bytes())
}

func TestParseWAVAcceptsHeaderOnlyDataChunk(t *testing.T) {
	t.Parallel()
	info := mustParseWAV(t, encodeWAV(t, 16000, 1, 16, 0))
	if got := info.frames(); got != 0 {
		t.Fatalf("frames = %d, want 0", got)
	}
	if got := info.durationMillis(); got != 0 {
		t.Fatalf("durationMillis = %d, want 0", got)
	}
	out, err := normalizeWAV(context.Background(), info)
	if err != nil {
		t.Fatalf("normalizeWAV() error = %v", err)
	}
	if len(out) != wavHeaderBytes {
		t.Fatalf("len(normalized) = %d, want header-only %d", len(out), wavHeaderBytes)
	}
	mustParseWAV(t, out)
}

func TestNormalizeWAVOutputAlwaysReparses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rate, channels, bits, frames int
	}{
		{8000, 2, 16, 8000},
		{22050, 1, 8, 22050},
		{48000, 1, 24, 48000},
		{44100, 4, 32, 4410},
		{192000, 2, 16, 19200},
	}
	for _, tc := range cases {
		info := mustParseWAV(t, encodeWAV(t, tc.rate, tc.channels, tc.bits, tc.frames))
		out, err := normalizeWAV(context.Background(), info)
		if err != nil {
			t.Fatalf("normalizeWAV(%+v) error = %v", tc, err)
		}
		if !isCanonicalWAV(out) {
			t.Fatalf("normalizeWAV(%+v) output is not canonical", tc)
		}
		reparsed := mustParseWAV(t, out)
		if got := reparsed.frames(); got != int64(len(out[wavHeaderBytes:])/2) {
			t.Fatalf("reparsed frames = %d, want %d", got, len(out[wavHeaderBytes:])/2)
		}
	}
}

func TestNormalizeWAVAllocationMatchesActualData(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rate   int
		frames int
	}{
		{rate: 4000, frames: 400},
		{rate: 8000, frames: 400},
		{rate: 11025, frames: 400},
		{rate: 192000, frames: 400},
		// Truncation-order witness: floor(7*16000/44100)=2 emits 48 bytes,
		// while the multiply-first reading gives 49. The want formula below
		// parentshesizes the single divide to match normalizeWAV.
		{rate: 44100, frames: 7},
	}
	for _, tc := range cases {
		info := mustParseWAV(t, encodeWAV(t, tc.rate, 1, 16, tc.frames))
		out, err := normalizeWAV(context.Background(), info)
		if err != nil {
			t.Fatalf("normalizeWAV(rate %d, frames %d) error = %v", tc.rate, tc.frames, err)
		}
		want := wavHeaderBytes + 2*(tc.frames*targetSampleRate/tc.rate)
		if len(out) != want {
			t.Fatalf("rate %d frames %d: len = %d, want %d (allocation must derive from actual data frames)", tc.rate, tc.frames, len(out), want)
		}
	}
}

func TestMonoSampleReadsBoundaryFramesExactly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		format uint16
		bits   int
		sample []byte
		want   int32
	}{
		{name: "8-bit silence", format: 1, bits: 8, sample: []byte{0x80}, want: 0},
		{name: "8-bit minimum", format: 1, bits: 8, sample: []byte{0x00}, want: -32768},
		{name: "8-bit maximum", format: 1, bits: 8, sample: []byte{0xff}, want: 32512},
		{name: "16-bit minimum", format: 1, bits: 16, sample: []byte{0x00, 0x80}, want: -32768},
		{name: "16-bit maximum", format: 1, bits: 16, sample: []byte{0xff, 0x7f}, want: 32767},
		{name: "24-bit minimum", format: 1, bits: 24, sample: []byte{0x00, 0x00, 0x80}, want: -32768},
		{name: "24-bit maximum", format: 1, bits: 24, sample: []byte{0xff, 0xff, 0x7f}, want: 32767},
		{name: "32-bit minimum", format: 1, bits: 32, sample: []byte{0x00, 0x00, 0x00, 0x80}, want: -32768},
		{name: "32-bit maximum", format: 1, bits: 32, sample: []byte{0xff, 0xff, 0xff, 0x7f}, want: 32767},
		{name: "float positive infinity clamps", format: 3, bits: 32, sample: []byte{0x00, 0x00, 0x80, 0x7f}, want: 32767},
		{name: "float negative infinity clamps", format: 3, bits: 32, sample: []byte{0x00, 0x00, 0x80, 0xff}, want: -32767},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := customWAV(t, tc.format, 0, 16000, 1, tc.bits, func(buf *bytes.Buffer) {
				buf.Write(tc.sample)
			})
			info := mustParseWAV(t, body)
			if got := info.frames(); got != 1 {
				t.Fatalf("frames = %d, want 1", got)
			}
			if got := info.monoSample(0); got != tc.want {
				t.Fatalf("monoSample(0) = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewResamplerTableStaysBounded(t *testing.T) {
	t.Parallel()
	maxHalfTaps := int64(math.Ceil(float64(resampleZeroCrossings) / (resampleRolloff * float64(targetSampleRate) / 192000)))
	maxTaps := int64(resampleMaxTable) * 2 * maxHalfTaps
	for _, rate := range []int{4000, 8000, 11025, 16000, 22050, 22051, 44100, 48000, 96000, 191999, 192000} {
		r := newResampler(rate)
		if int64(len(r.taps)) > resampleMaxTable {
			t.Fatalf("rate %d: tap phases = %d, want at most %d", rate, len(r.taps), resampleMaxTable)
		}
		var total int64
		for _, row := range r.taps {
			total += int64(len(row))
		}
		if total > maxTaps {
			t.Fatalf("rate %d: tap int32 count = %d, want at most %d", rate, total, maxTaps)
		}
		first := r.at(0, 64, func(int64) int32 { return 1000 })
		again := newResampler(rate).at(0, 64, func(int64) int32 { return 1000 })
		if first != again {
			t.Fatalf("rate %d: resampler output is not deterministic", rate)
		}
	}
}

func TestParseOutputAcceptsUnorderedAndExtremeOffsets(t *testing.T) {
	t.Parallel()
	raw := `{"transcription": [{"offsets": {"from": 5000, "to": 5000}, "text": " zero"}, {"offsets": {"from": 0, "to": 9223372036854775807}, "text": " huge"}]}`
	parsed, err := parseOutput([]byte(raw), Limits{MaxSegments: 10, MaxSegmentChars: 4096}, &runStats{})
	if err != nil {
		t.Fatalf("parseOutput() error = %v", err)
	}
	if len(parsed.segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(parsed.segments))
	}
	if parsed.segments[0].SegmentID != "seg-0001" || parsed.segments[0].Text != "zero" {
		t.Fatalf("first segment = %+v, want input order with seg-0001", parsed.segments[0])
	}
	if parsed.segments[1].EndMillis != math.MaxInt64 {
		t.Fatalf("second segment end = %d, want MaxInt64 stored as-is", parsed.segments[1].EndMillis)
	}
}

func TestParseOutputToleratesDelAndInvalidUTF8(t *testing.T) {
	t.Parallel()
	raw := append([]byte(`{"transcription": [{"offsets": {"from": 0, "to": 100}, "text": "A`+"\x7fB"), 0xff, 0xfe)
	raw = append(raw, []byte(`C"}]}`)...)
	parsed, err := parseOutput(raw, Limits{MaxSegments: 10, MaxSegmentChars: 4096}, &runStats{})
	if err != nil {
		t.Fatalf("parseOutput() error = %v, want invalid bytes tolerated without failure", err)
	}
	if len(parsed.segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(parsed.segments))
	}
	text := parsed.segments[0].Text
	if strings.Contains(text, "\xff") || strings.Contains(text, "\xfe") {
		t.Fatalf("segment text still holds raw invalid bytes: %q", text)
	}
	if !strings.Contains(text, "A\x7fB") || !strings.ContainsRune(text, '\uFFFD') {
		t.Fatalf("segment text = %q, want DEL kept and invalid bytes replaced", text)
	}
}

func TestParseOutputKeepsUnclosedAnnotationLikeText(t *testing.T) {
	t.Parallel()
	raw := `{"transcription": [{"offsets": {"from": 0, "to": 100}, "text": "[[[broken"}]}`
	parsed, err := parseOutput([]byte(raw), Limits{MaxSegments: 10, MaxSegmentChars: 4096}, &runStats{})
	if err != nil {
		t.Fatalf("parseOutput() error = %v", err)
	}
	if len(parsed.segments) != 1 || parsed.segments[0].Text != "[[[broken" {
		t.Fatalf("segments = %+v, want the unclosed text kept as speech", parsed.segments)
	}
	// A long run of brackets must classify without hanging the run.
	hostile := strings.Repeat("[", 10000)
	if nonSpeechPattern.MatchString(hostile) {
		t.Fatalf("10KB bracket run classified as non-speech, want kept")
	}
}

func TestTranscribeRunsEngineOnHeaderOnlyInput(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "empty", Limits{})
	_, err := engine.Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: encodeWAV(t, 16000, 1, 16, 0)})
	if got := ClassOf(err); got != ClassNoSpeech {
		t.Fatalf("ClassOf(%v) = %q, want no_speech", err, got)
	}
	if !errors.Is(err, docmedia.ErrNoSpeech) {
		t.Fatalf("header-only input must wrap media.ErrNoSpeech: %v", err)
	}
}

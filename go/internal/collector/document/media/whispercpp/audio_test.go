// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func TestNormalizeWAVProducesCanonicalSixteenKilohertzMono(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		rate       int
		channels   int
		bits       int
		frames     int
		wantFrames int
	}{
		{name: "16k mono 16-bit passthrough", rate: 16000, channels: 1, bits: 16, frames: 16000, wantFrames: 16000},
		{name: "8k stereo 16-bit upsample", rate: 8000, channels: 2, bits: 16, frames: 8000, wantFrames: 16000},
		{name: "48k mono 24-bit downsample", rate: 48000, channels: 1, bits: 24, frames: 48000, wantFrames: 16000},
		{name: "22050 mono 8-bit", rate: 22050, channels: 1, bits: 8, frames: 22050, wantFrames: 16000},
		{name: "44.1k quad 32-bit", rate: 44100, channels: 4, bits: 32, frames: 4410, wantFrames: 1600},
		{name: "rate coprime with 16k", rate: 22051, channels: 1, bits: 16, frames: 22051, wantFrames: 16000},
		{name: "192k stereo 16-bit", rate: 192000, channels: 2, bits: 16, frames: 19200, wantFrames: 1600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info, err := parseWAV(encodeWAV(t, tc.rate, tc.channels, tc.bits, tc.frames))
			if err != nil {
				t.Fatalf("parseWAV() error = %v", err)
			}
			if got, want := info.durationMillis(), int64(tc.frames)*1000/int64(tc.rate); got != want {
				t.Fatalf("durationMillis = %d, want %d", got, want)
			}
			out, err := normalizeWAV(context.Background(), info)
			if err != nil {
				t.Fatalf("normalizeWAV() error = %v", err)
			}
			if !isCanonicalWAV(out) {
				t.Fatalf("output is not 16 kHz mono 16-bit WAV")
			}
			if got := (len(out) - 44) / 2; got != tc.wantFrames {
				t.Fatalf("output frames = %d, want %d", got, tc.wantFrames)
			}
			again, err := normalizeWAV(context.Background(), info)
			if err != nil || !bytes.Equal(out, again) {
				t.Fatalf("normalizeWAV is not deterministic (err %v)", err)
			}
		})
	}
}

func TestNormalizeWAVPassthroughKeepsSamples(t *testing.T) {
	t.Parallel()
	body := encodeWAV(t, 16000, 1, 16, 320)
	info, err := parseWAV(body)
	if err != nil {
		t.Fatalf("parseWAV() error = %v", err)
	}
	out, err := normalizeWAV(context.Background(), info)
	if err != nil {
		t.Fatalf("normalizeWAV() error = %v", err)
	}
	if !bytes.Equal(out[44:], body[44:]) {
		t.Fatalf("16 kHz mono 16-bit samples changed during normalization")
	}
}

func TestParseWAVDecodesFloatAndExtensibleFormats(t *testing.T) {
	t.Parallel()
	floatBody := customWAV(t, 3, 0, 16000, 1, 32, func(buf *bytes.Buffer) {
		for _, v := range []float32{0, 0.5, -0.5, 2, float32(math.NaN())} {
			_ = binary.Write(buf, binary.LittleEndian, math.Float32bits(v))
		}
	})
	info, err := parseWAV(floatBody)
	if err != nil {
		t.Fatalf("parseWAV(float) error = %v", err)
	}
	out, err := normalizeWAV(context.Background(), info)
	if err != nil {
		t.Fatalf("normalizeWAV(float) error = %v", err)
	}
	want := []int16{0, 16384, -16384, 32767, 0}
	for i, w := range want {
		if got := int16(binary.LittleEndian.Uint16(out[44+2*i:])); got != w {
			t.Fatalf("float sample %d = %d, want %d", i, got, w)
		}
	}
	extensible := customWAV(t, 0xFFFE, 1, 16000, 1, 16, func(buf *bytes.Buffer) {
		_ = binary.Write(buf, binary.LittleEndian, int16(1234))
	})
	if _, err := parseWAV(extensible); err != nil {
		t.Fatalf("parseWAV(extensible PCM) error = %v", err)
	}
}

func TestParseWAVRejectsHostileHeaders(t *testing.T) {
	t.Parallel()
	valid := encodeWAV(t, 16000, 1, 16, 100)
	mutate := func(offset int, value []byte) []byte {
		body := append([]byte(nil), valid...)
		copy(body[offset:], value)
		return body
	}
	cases := map[string][]byte{
		"empty":                {},
		"not riff":             mutate(0, []byte("RIFX")),
		"not wave":             mutate(8, []byte("AVI ")),
		"zero channels":        mutate(22, []byte{0, 0}),
		"too many channels":    mutate(22, []byte{0xff, 0}),
		"tiny sample rate":     mutate(24, []byte{1, 0, 0, 0}),
		"unsupported bits":     mutate(34, []byte{12, 0}),
		"block align mismatch": mutate(32, []byte{9, 0}),
		"data beyond body":     mutate(40, []byte{0xff, 0xff, 0xff, 0x7f}),
		"partial frame":        append(mutate(40, []byte{201, 0, 0, 0}), 0),
		"no data chunk":        valid[:36],
		"fmt chunk overflow":   mutate(16, []byte{0xff, 0xff, 0xff, 0xff}),
		"compressed format":    mutate(20, []byte{0x55, 0}),
		"extensible non-pcm":   customWAV(t, 0xFFFE, 0x55, 16000, 1, 16, func(*bytes.Buffer) {}),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseWAV(body); err == nil {
				t.Fatalf("parseWAV() error = nil, want malformed media")
			}
		})
	}
}

// customWAV writes a WAV with an explicit format tag. A formatTag of 0xFFFE
// writes WAVE_FORMAT_EXTENSIBLE with subFormat as the GUID's leading tag.
func customWAV(t *testing.T, formatTag, subFormat uint16, rate, channels, bits int, data func(*bytes.Buffer)) []byte {
	t.Helper()
	var samples bytes.Buffer
	data(&samples)
	fmtSize := 16
	if formatTag == 0xFFFE {
		fmtSize = 40
	}
	blockAlign := channels * bits / 8
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4+8+fmtSize+8+samples.Len()))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(fmtSize))
	_ = binary.Write(&buf, binary.LittleEndian, formatTag)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(rate*blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bits))
	if formatTag == 0xFFFE {
		_ = binary.Write(&buf, binary.LittleEndian, uint16(22))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(bits))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		_ = binary.Write(&buf, binary.LittleEndian, subFormat)
		buf.Write(make([]byte, 14))
	}
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(samples.Len()))
	buf.Write(samples.Bytes())
	return buf.Bytes()
}

// TestNormalizeWAVFiltersAliasingWhenDownsampling pins the band-limited
// resampler. The first version used plain linear interpolation, which folded
// a 10 kHz tone at 22.05 kHz back into the speech band at 0.589 of its input
// RMS. A 10 kHz tone must now be suppressed and a 1 kHz tone must pass.
func TestNormalizeWAVFiltersAliasingWhenDownsampling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		freq     float64
		minRatio float64
		maxRatio float64
	}{
		{freq: 10000, minRatio: 0, maxRatio: 0.05},
		{freq: 1000, minRatio: 0.9, maxRatio: 1.1},
	} {
		in := toneWAV(t, 22050, tc.freq, 22050)
		info, err := parseWAV(in)
		if err != nil {
			t.Fatalf("parseWAV() error = %v", err)
		}
		out, err := normalizeWAV(context.Background(), info)
		if err != nil {
			t.Fatalf("normalizeWAV() error = %v", err)
		}
		ratio := rms(out[wavHeaderBytes:]) / rms(in[wavHeaderBytes:])
		if ratio < tc.minRatio || ratio > tc.maxRatio {
			t.Fatalf("%.0f Hz tone RMS ratio = %.3f, want [%.2f, %.2f]", tc.freq, ratio, tc.minRatio, tc.maxRatio)
		}
	}
}

func toneWAV(t *testing.T, rate int, freq float64, frames int) []byte {
	t.Helper()
	body := encodeWAV(t, rate, 1, 16, frames)
	for i := range frames {
		v := int16(math.Round(16000 * math.Sin(2*math.Pi*freq*float64(i)/float64(rate))))
		binary.LittleEndian.PutUint16(body[wavHeaderBytes+2*i:], uint16(v))
	}
	return body
}

func rms(samples []byte) float64 {
	var sum float64
	n := len(samples) / 2
	// Skip the filter edges so ramp-in does not dominate.
	for i := n / 10; i < n-n/10; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(samples[2*i:])))
		sum += v * v
	}
	return math.Sqrt(sum / float64(n-2*(n/10)))
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"encoding/binary"
	"math"
)

const (
	targetSampleRate  = 16000
	wavHeaderBytes    = 44
	minSampleRate     = 4000
	maxSampleRate     = 192000
	maxChannels       = 8
	formatPCM         = 1
	formatIEEEFloat   = 3
	formatExtensible  = 0xFFFE
	contextCheckEvery = 1 << 16
)

// wavInfo is the validated layout of one WAV body. data aliases the input
// slice; it is read only.
type wavInfo struct {
	format     uint16
	channels   int
	sampleRate int
	bits       int
	blockAlign int
	data       []byte
}

func (w wavInfo) frames() int64 { return int64(len(w.data) / w.blockAlign) }

func (w wavInfo) durationMillis() int64 {
	return w.frames() * 1000 / int64(w.sampleRate)
}

// parseWAV validates RIFF/WAVE structure with bounds-checked chunk walking.
// It accepts PCM 8/16/24/32-bit, IEEE float 32-bit, and the extensible forms
// of both; everything else is malformed media for this engine.
func parseWAV(body []byte) (wavInfo, error) {
	if len(body) < 12 || string(body[0:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		return wavInfo{}, classified(ClassMalformedMedia, "missing RIFF/WAVE header")
	}
	var info wavInfo
	haveFormat := false
	offset := 12
	for offset+8 <= len(body) {
		id := string(body[offset : offset+4])
		size := int64(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		start := offset + 8
		if size > int64(len(body)-start) {
			return wavInfo{}, classified(ClassMalformedMedia, "chunk extends past body")
		}
		end := start + int(size)
		switch id {
		case "fmt ":
			parsed, err := parseFormat(body[start:end])
			if err != nil {
				return wavInfo{}, err
			}
			info = parsed
			haveFormat = true
		case "data":
			if !haveFormat {
				return wavInfo{}, classified(ClassMalformedMedia, "data chunk before fmt chunk")
			}
			if int(size)%info.blockAlign != 0 {
				return wavInfo{}, classified(ClassMalformedMedia, "data chunk holds a partial frame")
			}
			info.data = body[start:end]
			return info, nil
		}
		offset = end + int(size&1)
	}
	return wavInfo{}, classified(ClassMalformedMedia, "missing fmt or data chunk")
}

func parseFormat(chunk []byte) (wavInfo, error) {
	if len(chunk) < 16 {
		return wavInfo{}, classified(ClassMalformedMedia, "fmt chunk too short")
	}
	info := wavInfo{
		format:     binary.LittleEndian.Uint16(chunk[0:2]),
		channels:   int(binary.LittleEndian.Uint16(chunk[2:4])),
		sampleRate: int(binary.LittleEndian.Uint32(chunk[4:8])),
		blockAlign: int(binary.LittleEndian.Uint16(chunk[12:14])),
		bits:       int(binary.LittleEndian.Uint16(chunk[14:16])),
	}
	if info.format == formatExtensible {
		if len(chunk) < 40 {
			return wavInfo{}, classified(ClassMalformedMedia, "extensible fmt chunk too short")
		}
		info.format = binary.LittleEndian.Uint16(chunk[24:26])
	}
	switch {
	case info.channels < 1 || info.channels > maxChannels:
		return wavInfo{}, classified(ClassMalformedMedia, "unsupported channel count")
	case info.sampleRate < minSampleRate || info.sampleRate > maxSampleRate:
		return wavInfo{}, classified(ClassMalformedMedia, "unsupported sample rate")
	case info.format == formatPCM && info.bits != 8 && info.bits != 16 && info.bits != 24 && info.bits != 32:
		return wavInfo{}, classified(ClassMalformedMedia, "unsupported PCM sample width")
	case info.format == formatIEEEFloat && info.bits != 32:
		return wavInfo{}, classified(ClassMalformedMedia, "unsupported float sample width")
	case info.format != formatPCM && info.format != formatIEEEFloat:
		return wavInfo{}, classified(ClassMalformedMedia, "compressed or unknown WAV encoding")
	case info.blockAlign != info.channels*info.bits/8:
		return wavInfo{}, classified(ClassMalformedMedia, "block align does not match layout")
	}
	return info, nil
}

// normalizeWAV downmixes to mono and resamples to 16 kHz with the
// band-limited integer resampler in resample.go (16 kHz input is copied), so
// the bytes handed to the engine are identical for identical input on every
// architecture. The engine never parses the caller's original container.
func normalizeWAV(ctx context.Context, info wavInfo) ([]byte, error) {
	srcFrames := info.frames()
	dstFrames := srcFrames * targetSampleRate / int64(info.sampleRate)
	out := make([]byte, wavHeaderBytes+2*dstFrames)
	writeCanonicalHeader(out, dstFrames)
	if info.sampleRate == targetSampleRate {
		for i := int64(0); i < dstFrames; i++ {
			if i%contextCheckEvery == 0 {
				if err := contextError(ctx); err != nil {
					return nil, err
				}
			}
			binary.LittleEndian.PutUint16(out[wavHeaderBytes+2*i:], uint16(int16(info.monoSample(i)))) // #nosec G115 -- monoSample is on a 16-bit scale by construction; int16 to uint16 stores the two's complement PCM sample
		}
		return out, nil
	}
	resample := newResampler(info.sampleRate)
	for i := int64(0); i < dstFrames; i++ {
		if i%contextCheckEvery == 0 {
			if err := contextError(ctx); err != nil {
				return nil, err
			}
		}
		binary.LittleEndian.PutUint16(out[wavHeaderBytes+2*i:], uint16(resample.at(i, srcFrames, info.monoSample))) // #nosec G115 -- resampler.at clamps to int16; int16 to uint16 stores the two's complement PCM sample
	}
	return out, nil
}

// monoSample returns the channel average of one frame on a 16-bit scale.
func (w wavInfo) monoSample(frame int64) int32 {
	base := int(frame) * w.blockAlign
	width := w.bits / 8
	var sum int64
	for c := range w.channels {
		sum += int64(w.sample(w.data[base+c*width:]))
	}
	return int32(sum / int64(w.channels)) // #nosec G115 -- the mean of int32 samples stays within int32
}

func (w wavInfo) sample(b []byte) int32 {
	switch {
	case w.format == formatIEEEFloat:
		f := float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
		if math.IsNaN(f) {
			return 0
		}
		f = math.Max(-1, math.Min(1, f))
		return int32(math.Round(f * 32767))
	case w.bits == 8:
		return (int32(b[0]) - 128) << 8
	case w.bits == 16:
		return int32(int16(binary.LittleEndian.Uint16(b))) // #nosec G115 -- uint16 to int16 reinterprets a signed 16-bit PCM sample
	case w.bits == 24:
		return int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24) >> 16 // #nosec G115 -- the three bytes are placed in the top of a uint32 so the sign bit lands in bit 31, then shifted to 16 bits
	default:
		return int32(binary.LittleEndian.Uint32(b)) >> 16 // #nosec G115 -- uint32 to int32 reinterprets a signed 32-bit PCM sample, then shifted to 16 bits
	}
}

func writeCanonicalHeader(out []byte, frames int64) {
	dataSize := uint32(2 * frames) // #nosec G115 -- frames is bounded by MaxDurationMillis (at most 30 minutes at 16 kHz) checked before normalizeWAV, so 2*frames fits uint32
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], 36+dataSize)
	copy(out[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], formatPCM)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], targetSampleRate)
	binary.LittleEndian.PutUint32(out[28:32], targetSampleRate*2)
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], dataSize)
}

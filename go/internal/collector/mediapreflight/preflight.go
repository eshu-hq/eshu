// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediapreflight

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// FormatMP3 identifies an MP3 audio source.
	FormatMP3 = "mp3"
	// FormatWAV identifies a WAV audio source.
	FormatWAV = "wav"
	// FormatM4A identifies an M4A audio source.
	FormatM4A = "m4a"
	// FormatOGG identifies an Ogg audio source.
	FormatOGG = "ogg"
	// FormatMP4 identifies an MP4 video source.
	FormatMP4 = "mp4"
	// FormatMOV identifies a MOV video source.
	FormatMOV = "mov"
	// FormatWEBM identifies a WebM video source.
	FormatWEBM = "webm"
	// FormatMKV identifies an MKV video source.
	FormatMKV = "mkv"
)

const (
	defaultMaxSourceBytes    = int64(250 << 20)
	defaultMaxDurationMillis = int64(30 * 60 * 1000)
	maxReadBytes             = int64(250 << 20)
)

// WarningClass is a stable, low-cardinality media preflight failure class.
type WarningClass string

const (
	// WarningUnsupportedFormat marks media formats outside this preflight.
	WarningUnsupportedFormat WarningClass = "unsupported_format"
	// WarningUnsupportedCodec marks media codecs or containers not handled by metadata preflight.
	WarningUnsupportedCodec WarningClass = "unsupported_codec"
	// WarningMalformedMedia marks media containers that cannot be decoded as metadata.
	WarningMalformedMedia WarningClass = "malformed_media"
	// WarningResourceLimitExceeded marks source-byte or duration limits.
	WarningResourceLimitExceeded WarningClass = "resource_limit_exceeded"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout WarningClass = "timeout"
	// WarningTranscriptNoSpeech marks metadata indicating no audio stream is present.
	WarningTranscriptNoSpeech WarningClass = "transcript_no_speech"
	// WarningExternalReferenceSkipped marks external references that cannot be followed.
	WarningExternalReferenceSkipped WarningClass = "external_reference_skipped"
	// WarningSensitiveValueRedacted marks sensitive-looking values detected in media metadata bytes.
	WarningSensitiveValueRedacted WarningClass = "sensitive_value_redacted"
	// WarningMetadataRedacted marks media metadata fields that must not be persisted.
	WarningMetadataRedacted WarningClass = "metadata_redacted"
)

// Options bounds media preflight work.
type Options struct {
	MaxSourceBytes    int64
	MaxDurationMillis int64
}

// Warning records one bounded media preflight failure class.
type Warning struct {
	Class WarningClass `json:"class"`
	Count int          `json:"count"`
}

// Result summarizes metadata-only media preflight.
type Result struct {
	Format                 string    `json:"format"`
	Safe                   bool      `json:"safe"`
	Warnings               []Warning `json:"warnings,omitempty"`
	SourceBytes            int64     `json:"source_bytes"`
	DurationMillis         int64     `json:"duration_millis,omitempty"`
	AudioStreamCount       int       `json:"audio_stream_count,omitempty"`
	ExternalReferenceCount int       `json:"external_reference_count"`
	SensitiveValueCount    int       `json:"sensitive_value_count"`
	MetadataRedactionCount int       `json:"metadata_redaction_count"`
	NoAudioStreamCount     int       `json:"no_audio_stream_count"`
}

type recorder struct {
	result *Result
	seen   map[WarningClass]int
}

// Preflight classifies a media source without extracting transcripts, samples, or frames.
func Preflight(ctx context.Context, sourceName string, reader io.ReaderAt, size int64, options Options) (Result, error) {
	if reader == nil {
		return Result{}, fmt.Errorf("reader must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := normalizeOptions(options)
	result := Result{
		Format:      formatForSource(sourceName),
		Safe:        true,
		SourceBytes: size,
	}
	rec := recorder{result: &result, seen: map[WarningClass]int{}}

	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return rec.finalize(), err
	}
	if result.Format == "" {
		rec.warn(WarningUnsupportedFormat)
		return rec.finalize(), nil
	}
	if size < 0 || size > opts.MaxSourceBytes || size > maxReadBytes {
		rec.warn(WarningResourceLimitExceeded)
		return rec.finalize(), nil
	}
	body, ok := readExact(reader, size, &rec)
	if !ok {
		return rec.finalize(), nil
	}
	rec.classifyMedia(ctx, body, opts)
	return rec.finalize(), nil
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	if options.MaxDurationMillis <= 0 {
		options.MaxDurationMillis = defaultMaxDurationMillis
	}
	return options
}

func readExact(reader io.ReaderAt, size int64, rec *recorder) ([]byte, bool) {
	body := make([]byte, size)
	n, err := reader.ReadAt(body, 0)
	if err != nil && (err != io.EOF || n != len(body)) {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	return body, true
}

func (r *recorder) classifyMedia(ctx context.Context, body []byte, options Options) {
	if err := ctx.Err(); err != nil {
		r.warn(WarningTimeout)
		return
	}
	r.scanMetadataMarkers(body)
	if r.result.Format != FormatWAV {
		r.warn(WarningUnsupportedCodec)
		return
	}
	duration, streams, ok := parseWAVMetadata(body)
	if !ok {
		r.warn(WarningMalformedMedia)
		return
	}
	r.result.DurationMillis = duration
	r.result.AudioStreamCount = streams
	if duration > options.MaxDurationMillis {
		r.warn(WarningResourceLimitExceeded)
	}
}

func parseWAVMetadata(body []byte) (int64, int, bool) {
	if len(body) < 44 || !bytes.Equal(body[0:4], []byte("RIFF")) || !bytes.Equal(body[8:12], []byte("WAVE")) {
		return 0, 0, false
	}
	offset := 12
	var channels uint16
	var byteRate uint32
	var dataSize uint32
	hasFormat := false
	hasData := false
	for offset+8 <= len(body) {
		chunkID := string(body[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 || offset+chunkSize > len(body) {
			return 0, 0, false
		}
		chunk := body[offset : offset+chunkSize]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return 0, 0, false
			}
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			byteRate = binary.LittleEndian.Uint32(chunk[8:12])
			hasFormat = true
		case "data":
			dataSize = uint32(chunkSize)
			hasData = true
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if !hasFormat || !hasData || channels == 0 || byteRate == 0 {
		return 0, 0, false
	}
	return int64(dataSize) * 1000 / int64(byteRate), int(channels), true
}

func (r *recorder) scanMetadataMarkers(body []byte) {
	lower := strings.ToLower(string(body))
	if hasAny(lower, "http://", "https://", "ftp://", "file://") {
		r.result.ExternalReferenceCount++
		r.warn(WarningExternalReferenceSkipped)
	}
	if hasAny(lower, "credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key") {
		r.result.SensitiveValueCount++
		r.warn(WarningSensitiveValueRedacted)
	}
	if hasAny(lower, "title=", "artist=", "album=", "comment=", "encoder=", "metadata", "location=", "user=") {
		r.result.MetadataRedactionCount++
		r.warn(WarningMetadataRedacted)
	}
	if hasAny(lower, "no_audio_stream", "audio_streams=0") {
		r.result.NoAudioStreamCount++
		r.warn(WarningTranscriptNoSpeech)
	}
}

func (r *recorder) warn(class WarningClass) {
	if count, ok := r.seen[class]; ok {
		r.seen[class] = count + 1
		for i := range r.result.Warnings {
			if r.result.Warnings[i].Class == class {
				r.result.Warnings[i].Count++
				break
			}
		}
	} else {
		r.seen[class] = 1
		r.result.Warnings = append(r.result.Warnings, Warning{Class: class, Count: 1})
	}
	r.result.Safe = false
}

func (r *recorder) finalize() Result {
	if len(r.result.Warnings) > 0 {
		r.result.Safe = false
		sort.Slice(r.result.Warnings, func(left, right int) bool {
			return r.result.Warnings[left].Class < r.result.Warnings[right].Class
		})
	}
	return *r.result
}

func formatForSource(sourceName string) string {
	switch strings.ToLower(filepath.Ext(sourceName)) {
	case ".mp3":
		return FormatMP3
	case ".wav":
		return FormatWAV
	case ".m4a":
		return FormatM4A
	case ".ogg":
		return FormatOGG
	case ".mp4":
		return FormatMP4
	case ".mov":
		return FormatMOV
	case ".webm":
		return FormatWEBM
	case ".mkv":
		return FormatMKV
	default:
		return ""
	}
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

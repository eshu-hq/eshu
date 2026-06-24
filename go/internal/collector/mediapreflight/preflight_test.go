// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediapreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreflightAcceptsSafeWAVMetadata(t *testing.T) {
	t.Parallel()

	body := wavBody(1, 1)
	result, err := Preflight(context.Background(), "walkthrough.wav", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if got, want := result.Format, FormatWAV; got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if !result.Safe {
		t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
	}
	if result.DurationMillis == 0 || result.AudioStreamCount == 0 {
		t.Fatalf("expected bounded WAV metadata, got %#v", result)
	}
}

func TestPreflightClassifiesMediaFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sourceName string
		want       string
	}{
		{sourceName: "incident.mp3", want: FormatMP3},
		{sourceName: "demo.m4a", want: FormatM4A},
		{sourceName: "review.ogg", want: FormatOGG},
		{sourceName: "walkthrough.mp4", want: FormatMP4},
		{sourceName: "capture.mov", want: FormatMOV},
		{sourceName: "demo.webm", want: FormatWEBM},
		{sourceName: "archive.mkv", want: FormatMKV},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.sourceName, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader(opaqueContainer()), int64(len(opaqueContainer())), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if result.Format != tt.want {
				t.Fatalf("Format = %q, want %q", result.Format, tt.want)
			}
			assertWarning(t, result, WarningUnsupportedCodec)
		})
	}
}

func TestPreflightClassifiesUnsupportedMalformedAndLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       []byte
		options    Options
		wantClass  WarningClass
	}{
		{name: "unsupported_format", sourceName: "notes.txt", body: []byte("ignored"), wantClass: WarningUnsupportedFormat},
		{name: "corrupt_wav", sourceName: "broken.wav", body: []byte("not media"), wantClass: WarningMalformedMedia},
		{name: "oversized_source", sourceName: "large.wav", body: wavBody(1, 1), options: Options{MaxSourceBytes: 4}, wantClass: WarningResourceLimitExceeded},
		{name: "duration_limit", sourceName: "long.wav", body: wavBody(2, 1), options: Options{MaxDurationMillis: 1000}, wantClass: WarningResourceLimitExceeded},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader(tt.body), int64(len(tt.body)), tt.options)
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, tt.wantClass)
		})
	}
}

func TestPreflightClassifiesMetadataSensitiveExternalAndNoAudioMarkers(t *testing.T) {
	t.Parallel()

	body := append(wavBody(1, 1), []byte("NO_AUDIO_STREAM title=private credential_marker https://example.invalid/ref")...)
	result, err := Preflight(context.Background(), "walkthrough.wav", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningTranscriptNoSpeech)
	assertWarning(t, result, WarningMetadataRedacted)
	assertWarning(t, result, WarningSensitiveValueRedacted)
	assertWarning(t, result, WarningExternalReferenceSkipped)
	if result.NoAudioStreamCount == 0 || result.MetadataRedactionCount == 0 ||
		result.SensitiveValueCount == 0 || result.ExternalReferenceCount == 0 {
		t.Fatalf("expected bounded marker counts, got %#v", result)
	}
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	body := wavBody(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "walkthrough.wav", bytes.NewReader(body), int64(len(body)), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func TestPreflightResultJSONOmitsSourceAndMediaContent(t *testing.T) {
	t.Parallel()

	body := append(wavBody(1, 1), []byte("member-name-must-not-leak https://example.invalid/link credential_marker")...)
	result, err := Preflight(context.Background(), "private-source-name.wav", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v, want nil", err)
	}
	jsonText := string(encoded)
	for _, disallowed := range []string{"member-name-must-not-leak", "private-source-name", "example.invalid", "credential_marker"} {
		if strings.Contains(jsonText, disallowed) {
			t.Fatalf("result JSON leaked %q: %s", disallowed, jsonText)
		}
	}
}

func wavBody(seconds uint32, sampleRate uint32) []byte {
	byteRate := sampleRate * 2
	dataSize := seconds * byteRate
	body := make([]byte, 44+dataSize)
	copy(body[0:4], "RIFF")
	putUint32(body[4:8], uint32(len(body)-8))
	copy(body[8:12], "WAVE")
	copy(body[12:16], "fmt ")
	putUint32(body[16:20], 16)
	putUint16(body[20:22], 1)
	putUint16(body[22:24], 1)
	putUint32(body[24:28], sampleRate)
	putUint32(body[28:32], byteRate)
	putUint16(body[32:34], 2)
	putUint16(body[34:36], 16)
	copy(body[36:40], "data")
	putUint32(body[40:44], dataSize)
	return body
}

func opaqueContainer() []byte {
	return []byte("container-header-without-decoder")
}

func putUint16(dst []byte, value uint16) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
}

func putUint32(dst []byte, value uint32) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
	dst[2] = byte(value >> 16)
	dst[3] = byte(value >> 24)
}

func assertWarning(t *testing.T, result Result, class WarningClass) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class && warning.Count > 0 {
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}

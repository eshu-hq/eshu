// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dtmf

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

func TestTranscribeDecodesSeparatedTones(t *testing.T) {
	t.Parallel()

	body := encodeToneWAV(t, []toneSpan{
		{digit: '5', startMillis: 200, endMillis: 500},
		{digit: '9', startMillis: 2500, endMillis: 2800},
	}, 3200)

	engine := Engine{}
	result, err := engine.Transcribe(context.Background(), docmedia.Media{
		SourceName: "docs/qa-tones.wav",
		Format:     "wav",
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v, want nil", err)
	}
	if result.EngineName != EngineName {
		t.Fatalf("EngineName = %q, want %q", result.EngineName, EngineName)
	}
	if result.EngineVersion == "" {
		t.Fatal("EngineVersion = empty, want implementation version")
	}
	if len(result.Segments) != 2 {
		t.Fatalf("len(Segments) = %d, want 2", len(result.Segments))
	}
	texts := []string{result.Segments[0].Text, result.Segments[1].Text}
	if texts[0] != "5" || texts[1] != "9" {
		t.Fatalf("segment texts = %q, want [5 9]", texts)
	}
	assertMillisNear(t, "segment[0].StartMillis", result.Segments[0].StartMillis, 200, 75)
	assertMillisNear(t, "segment[0].EndMillis", result.Segments[0].EndMillis, 500, 75)
	assertMillisNear(t, "segment[1].StartMillis", result.Segments[1].StartMillis, 2500, 75)
	assertMillisNear(t, "segment[1].EndMillis", result.Segments[1].EndMillis, 2800, 75)
	for i, segment := range result.Segments {
		if segment.Confidence <= 0.5 || segment.Confidence > 1.0 {
			t.Fatalf("segment[%d].Confidence = %v, want (0.5, 1.0]", i, segment.Confidence)
		}
		if segment.EndMillis < segment.StartMillis {
			t.Fatalf("segment[%d] has inverted timing: %+v", i, segment)
		}
	}
}

func TestTranscribeWithholdsMultiDigitRuns(t *testing.T) {
	t.Parallel()

	body := encodeToneWAV(t, []toneSpan{
		{digit: '1', startMillis: 100, endMillis: 300},
		{digit: '2', startMillis: 500, endMillis: 700},
		{digit: '3', startMillis: 900, endMillis: 1100},
	}, 1500)

	engine := Engine{}
	result, err := engine.Transcribe(context.Background(), docmedia.Media{
		SourceName: "docs/qa-tones.wav",
		Format:     "wav",
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v, want nil", err)
	}
	for _, segment := range result.Segments {
		if strings.Contains(segment.Text, "123") {
			t.Fatalf("segment leaked withheld digit run: %+v", segment)
		}
		if hasConsecutiveDigits(segment.Text, 2) {
			t.Fatalf("segment persisted multi-digit run %q", segment.Text)
		}
	}
	if len(result.Segments) != 0 {
		t.Fatalf("len(Segments) = %d, want 0 for withheld run", len(result.Segments))
	}
}

func TestTranscribeDecodes8BitAndStereoPCM(t *testing.T) {
	t.Parallel()

	t.Run("8bit_mono", func(t *testing.T) {
		t.Parallel()
		samples := toneSamples(t, []toneSpan{{digit: '3', startMillis: 100, endMillis: 400}}, 1000, 8000)
		body := encodeWAV(t, samples, 8000, 8, 1)
		result, err := (Engine{}).Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: body})
		if err != nil {
			t.Fatalf("Transcribe() error = %v, want nil", err)
		}
		if len(result.Segments) != 1 || result.Segments[0].Text != "3" {
			t.Fatalf("Segments = %+v, want one 3 segment", result.Segments)
		}
	})
	t.Run("16bit_stereo", func(t *testing.T) {
		t.Parallel()
		samples := toneSamples(t, []toneSpan{{digit: '0', startMillis: 100, endMillis: 400}}, 1000, 8000)
		body := encodeWAV(t, samples, 8000, 16, 2)
		result, err := (Engine{}).Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: body})
		if err != nil {
			t.Fatalf("Transcribe() error = %v, want nil", err)
		}
		if len(result.Segments) != 1 || result.Segments[0].Text != "0" {
			t.Fatalf("Segments = %+v, want one 0 segment", result.Segments)
		}
	})
}

func TestTranscribeEmitsNoIdentityFacts(t *testing.T) {
	t.Parallel()

	body := encodeToneWAV(t, []toneSpan{
		{digit: '7', startMillis: 100, endMillis: 400},
	}, 1000)

	engine := Engine{}
	result, err := engine.Transcribe(context.Background(), docmedia.Media{
		SourceName: "docs/qa-tones.wav",
		Format:     "wav",
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v, want nil", err)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("len(Segments) = %d, want 1", len(result.Segments))
	}
	segment := result.Segments[0]
	if segment.SegmentID != "seg-1" {
		t.Fatalf("SegmentID = %q, want opaque seg-1", segment.SegmentID)
	}
	if segment.SpeakerLabel != "" {
		t.Fatalf("SpeakerLabel = %q, want empty", segment.SpeakerLabel)
	}
	if len(segment.MentionHints) != 0 {
		t.Fatalf("MentionHints = %+v, want none", segment.MentionHints)
	}
	if result.Language != "" {
		t.Fatalf("Language = %q, want empty (no lexical claim)", result.Language)
	}
}

func TestTranscribeIgnoresTonelessNoise(t *testing.T) {
	t.Parallel()

	body := encodeNoiseWAV(t, 2000)

	engine := Engine{}
	result, err := engine.Transcribe(context.Background(), docmedia.Media{
		SourceName: "docs/qa-noise.wav",
		Format:     "wav",
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v, want nil", err)
	}
	if len(result.Segments) != 0 {
		t.Fatalf("len(Segments) = %d, want 0 for toneless audio", len(result.Segments))
	}
}

func TestTranscribeRejectsUnboundedWork(t *testing.T) {
	t.Parallel()

	body := encodeToneWAV(t, []toneSpan{
		{digit: '5', startMillis: 100, endMillis: 400},
	}, 3200)

	t.Run("input_bytes_cap", func(t *testing.T) {
		t.Parallel()
		engine := Engine{Options: Options{MaxInputBytes: 100}}
		if _, err := engine.Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: body}); err == nil {
			t.Fatal("Transcribe() error = nil, want input-bytes cap error")
		}
	})
	t.Run("duration_cap", func(t *testing.T) {
		t.Parallel()
		engine := Engine{Options: Options{MaxDurationMillis: 100}}
		if _, err := engine.Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: body}); err == nil {
			t.Fatal("Transcribe() error = nil, want duration cap error")
		}
	})
	t.Run("segment_cap", func(t *testing.T) {
		t.Parallel()
		engine := Engine{Options: Options{MaxSegments: 1}}
		isolated := encodeToneWAV(t, []toneSpan{
			{digit: '5', startMillis: 100, endMillis: 300},
			{digit: '9', startMillis: 2500, endMillis: 2700},
		}, 3200)
		if _, err := engine.Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: isolated}); err == nil {
			t.Fatal("Transcribe() error = nil, want segment cap error")
		}
	})
	t.Run("cancelled_context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := (Engine{}).Transcribe(ctx, docmedia.Media{Format: "wav", Body: body}); err == nil {
			t.Fatal("Transcribe() error = nil, want cancellation error")
		}
	})
	t.Run("deadline_context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if _, err := (Engine{}).Transcribe(ctx, docmedia.Media{Format: "wav", Body: body}); err == nil {
			t.Fatal("Transcribe() error = nil, want deadline error")
		}
	})
}

func TestTranscribeRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	t.Run("empty_body", func(t *testing.T) {
		t.Parallel()
		if _, err := (Engine{}).Transcribe(context.Background(), docmedia.Media{Format: "wav"}); err == nil {
			t.Fatal("Transcribe() error = nil, want empty-body error")
		}
	})
	t.Run("non_wav_format", func(t *testing.T) {
		t.Parallel()
		body := encodeToneWAV(t, []toneSpan{{digit: '5', startMillis: 100, endMillis: 300}}, 1000)
		if _, err := (Engine{}).Transcribe(context.Background(), docmedia.Media{Format: "mp3", Body: body}); err == nil {
			t.Fatal("Transcribe() error = nil, want format error")
		}
	})
	t.Run("malformed_wav", func(t *testing.T) {
		t.Parallel()
		if _, err := (Engine{}).Transcribe(context.Background(), docmedia.Media{Format: "wav", Body: []byte("not media")}); err == nil {
			t.Fatal("Transcribe() error = nil, want malformed-media error")
		}
	})
}

func TestExtractWithLocalEngineEmitsDigitFacts(t *testing.T) {
	t.Parallel()

	body := encodeToneWAV(t, []toneSpan{
		{digit: '5', startMillis: 200, endMillis: 500},
	}, 1000)

	result, err := docmedia.Extract(context.Background(), docmedia.Request{
		ScopeID:      "doc-source:git:repo-media",
		GenerationID: "gen-media-qa-1",
		ObservedAt:   time.Date(2026, time.October, 31, 12, 0, 0, 0, time.UTC),
		SourceSystem: "qa",
		SourceURI:    "docs/qa-tones.wav",
		SourceName:   "docs/qa-tones.wav",
		SourceID:     "doc-source:git:repo-media",
		DocumentID:   "doc:git:repo-media:qa-tones",
		ExternalID:   "docs/qa-tones.wav",
		RevisionID:   "rev-qa-1",
		CanonicalURI: "git://repo-media/docs/qa-tones.wav",
		Title:        "QA tone fixture",
		Body:         body,
		Engine:       Engine{},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	sections := make([]map[string]any, 0)
	for _, envelope := range result.Envelopes {
		if envelope.FactKind == "documentation_section" {
			sections = append(sections, envelope.Payload)
		}
	}
	if len(sections) != 1 {
		t.Fatalf("section count = %d, want 1", len(sections))
	}
	if got := sections[0]["content"].(string); got != "5" {
		t.Fatalf("section content = %q, want 5", got)
	}
	metadata, ok := sections[0]["source_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("source_metadata = %#v, want map", sections[0]["source_metadata"])
	}
	if got := metadata["transcript_engine"]; got != EngineName {
		t.Fatalf("transcript_engine = %#v, want %q", got, EngineName)
	}
}

func hasConsecutiveDigits(text string, run int) bool {
	streak := 0
	for _, r := range text {
		if r >= '0' && r <= '9' {
			streak++
			if streak >= run {
				return true
			}
		} else {
			streak = 0
		}
	}
	return false
}

func assertMillisNear(t *testing.T, name string, got, want, tolerance int64) {
	t.Helper()
	delta := got - want
	if delta < 0 {
		delta = -delta
	}
	if delta > tolerance {
		t.Fatalf("%s = %d, want %d ± %d", name, got, want, tolerance)
	}
}

type toneSpan struct {
	digit       byte
	startMillis int
	endMillis   int
}

var dtmfFrequencies = map[byte][2]float64{
	'1': {697, 1209}, '2': {697, 1336}, '3': {697, 1477},
	'4': {770, 1209}, '5': {770, 1336}, '6': {770, 1477},
	'7': {852, 1209}, '8': {852, 1336}, '9': {852, 1477},
	'*': {941, 1209}, '0': {941, 1336}, '#': {941, 1477},
}

func encodeToneWAV(t *testing.T, spans []toneSpan, totalMillis int) []byte {
	t.Helper()

	const sampleRate = 8000
	return encodeWAV(t, toneSamples(t, spans, totalMillis, sampleRate), sampleRate, 16, 1)
}

func toneSamples(t *testing.T, spans []toneSpan, totalMillis, sampleRate int) []int16 {
	t.Helper()

	totalSamples := sampleRate * totalMillis / 1000
	samples := make([]int16, totalSamples)
	for _, span := range spans {
		frequencies, ok := dtmfFrequencies[span.digit]
		if !ok {
			t.Fatalf("unknown test digit %q", span.digit)
		}
		start := sampleRate * span.startMillis / 1000
		end := sampleRate * span.endMillis / 1000
		for i := start; i < end && i < totalSamples; i++ {
			elapsed := float64(i) / float64(sampleRate)
			value := 12000.0*math.Sin(2*math.Pi*frequencies[0]*elapsed) +
				12000.0*math.Sin(2*math.Pi*frequencies[1]*elapsed)
			samples[i] = int16(math.Max(-32768, math.Min(32767, value)))
		}
	}
	return samples
}

func encodeNoiseWAV(t *testing.T, totalMillis int) []byte {
	t.Helper()

	const sampleRate = 8000
	totalSamples := sampleRate * totalMillis / 1000
	rng := rand.New(rand.NewSource(1))
	samples := make([]int16, totalSamples)
	for i := range samples {
		samples[i] = int16((rng.Float64()*2 - 1) * 980)
	}
	return encodePCM16WAV(t, samples, sampleRate)
}

func encodeWAV(t *testing.T, samples []int16, sampleRate, bits, channels int) []byte {
	t.Helper()

	if bits != 8 && bits != 16 {
		t.Fatalf("unsupported test width %d", bits)
	}
	bytesPerSample := bits / 8
	byteRate := sampleRate * channels * bytesPerSample
	dataSize := len(samples) * bytesPerSample * channels
	buffer := make([]byte, 0, 44+dataSize)
	buffer = append(buffer, "RIFF"...)
	buffer = appendUint32(buffer, uint32(36+dataSize))
	buffer = append(buffer, "WAVE"...)
	buffer = append(buffer, "fmt "...)
	buffer = appendUint32(buffer, 16)
	buffer = appendUint16(buffer, 1)
	buffer = appendUint16(buffer, uint16(channels))
	buffer = appendUint32(buffer, uint32(sampleRate))
	buffer = appendUint32(buffer, uint32(byteRate))
	buffer = appendUint16(buffer, uint16(channels*bytesPerSample))
	buffer = appendUint16(buffer, uint16(bits))
	buffer = append(buffer, "data"...)
	buffer = appendUint32(buffer, uint32(dataSize))
	for _, sample := range samples {
		for channel := 0; channel < channels; channel++ {
			if bits == 16 {
				var word [2]byte
				binary.LittleEndian.PutUint16(word[:], uint16(sample))
				buffer = append(buffer, word[:]...)
			} else {
				buffer = append(buffer, byte(int(sample)/256+128))
			}
		}
	}
	return buffer
}

func encodePCM16WAV(t *testing.T, samples []int16, sampleRate int) []byte {
	t.Helper()

	return encodeWAV(t, samples, sampleRate, 16, 1)
}

func appendUint16(buffer []byte, value uint16) []byte {
	var word [2]byte
	binary.LittleEndian.PutUint16(word[:], value)
	return append(buffer, word[:]...)
}

func appendUint32(buffer []byte, value uint32) []byte {
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return append(buffer, word[:]...)
}

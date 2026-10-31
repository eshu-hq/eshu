// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dtmf

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

const (
	// EngineName identifies this backend in transcript section metadata.
	EngineName = "local-dtmf"
	// EngineVersion is the implementation version of this backend.
	EngineVersion = "1.0.0"
)

const (
	defaultMaxInputBytes     = int64(16 << 20)
	defaultMaxDurationMillis = int64(120_000)
	defaultMaxSegments       = 200
	defaultMaxSegmentChars   = 64
	defaultMaxPersistedRun   = 1
	frameMillis              = 25
	minSampleRate            = 4000
	maxSampleRate            = 192000
	maxChannels              = 64
	rmsFloor                 = 0.03
	powerFloor               = 1.0
	marginThreshold          = 6.0
	twistLow                 = 0.2
	twistHigh                = 5.0
	minRunFrames             = 2
	splitGapFrames           = 3
	segmentGapMillis         = int64(1500)
	contextCheckFrames       = 64
)

// Options bounds one DTMF transcription pass.
type Options struct {
	// MaxInputBytes caps the media body accepted for decoding.
	MaxInputBytes int64
	// MaxDurationMillis caps the header-derived audio duration decoded.
	MaxDurationMillis int64
	// MaxSegments caps the emitted segment count; overflow fails loudly.
	MaxSegments int
	// MaxSegmentChars caps persisted text per segment.
	MaxSegmentChars int
	// MaxPersistedDigitRun caps the longest consecutive digit run kept in
	// segment text. Longer runs are withheld as potentially sensitive.
	MaxPersistedDigitRun int
}

// Engine decodes DTMF keypad tones from WAV PCM audio. It implements
// docmedia.Engine with the zero value ready for QA-tier use.
type Engine struct {
	Options Options
}

// Transcribe decodes the DTMF tones in input and returns one segment per
// isolated tone group. It performs no network or filesystem I/O, allocates
// only one frame buffer plus output, and honors context cancellation.
func (e Engine) Transcribe(ctx context.Context, input docmedia.Media) (docmedia.EngineResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return docmedia.EngineResult{}, fmt.Errorf("dtmf transcribe cancelled: %w", err)
	}
	opts := normalizeOptions(e.Options)
	if len(input.Body) == 0 {
		return docmedia.EngineResult{}, fmt.Errorf("dtmf transcribe needs a non-empty media body")
	}
	if int64(len(input.Body)) > opts.MaxInputBytes {
		return docmedia.EngineResult{}, fmt.Errorf("dtmf input %d bytes exceeds %d byte cap", len(input.Body), opts.MaxInputBytes)
	}
	if input.Format != "" && input.Format != "wav" {
		return docmedia.EngineResult{}, fmt.Errorf("dtmf transcribes wav audio only, got %q", input.Format)
	}
	audio, err := decodePCM(input.Body)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	if audio.durationMillis > opts.MaxDurationMillis {
		return docmedia.EngineResult{}, fmt.Errorf("dtmf duration %d ms exceeds %d ms cap", audio.durationMillis, opts.MaxDurationMillis)
	}
	events, err := detectEvents(ctx, audio)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	segments, err := groupSegments(events, audio.sampleRate, opts)
	if err != nil {
		return docmedia.EngineResult{}, err
	}
	return docmedia.EngineResult{
		EngineName:    EngineName,
		EngineVersion: EngineVersion,
		Segments:      segments,
	}, nil
}

func normalizeOptions(opts Options) Options {
	if opts.MaxInputBytes <= 0 {
		opts.MaxInputBytes = defaultMaxInputBytes
	}
	if opts.MaxDurationMillis <= 0 {
		opts.MaxDurationMillis = defaultMaxDurationMillis
	}
	if opts.MaxSegments <= 0 {
		opts.MaxSegments = defaultMaxSegments
	}
	if opts.MaxSegmentChars <= 0 {
		opts.MaxSegmentChars = defaultMaxSegmentChars
	}
	if opts.MaxPersistedDigitRun <= 0 {
		opts.MaxPersistedDigitRun = defaultMaxPersistedRun
	}
	return opts
}

// pcmAudio is bounded decoded WAV PCM metadata plus raw data access.
type pcmAudio struct {
	data           []byte
	dataOffset     int
	dataSize       int
	channels       int
	sampleRate     int
	bitsPerSample  int
	durationMillis int64
}

func decodePCM(body []byte) (pcmAudio, error) {
	if len(body) < 44 || string(body[0:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		return pcmAudio{}, fmt.Errorf("dtmf transcribes wav containers only: malformed header")
	}
	var audio pcmAudio
	var byteRate int64
	formatSeen := false
	offset := 12
	for offset+8 <= len(body) {
		chunkID := string(body[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(body[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 || offset+chunkSize > len(body) {
			return pcmAudio{}, fmt.Errorf("dtmf transcribes wav containers only: chunk overruns body")
		}
		chunk := body[offset : offset+chunkSize]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return pcmAudio{}, fmt.Errorf("dtmf transcribes wav containers only: short format chunk")
			}
			format := binary.LittleEndian.Uint16(chunk[0:2])
			channels := int(binary.LittleEndian.Uint16(chunk[2:4]))
			rate := int(binary.LittleEndian.Uint32(chunk[4:8]))
			byteRate = int64(binary.LittleEndian.Uint32(chunk[8:12]))
			bits := int(binary.LittleEndian.Uint16(chunk[14:16]))
			if format != 1 {
				return pcmAudio{}, fmt.Errorf("dtmf transcribes PCM wav only, got format %d", format)
			}
			if channels < 1 || channels > maxChannels {
				return pcmAudio{}, fmt.Errorf("dtmf rejects channel count %d", channels)
			}
			if rate < minSampleRate || rate > maxSampleRate {
				return pcmAudio{}, fmt.Errorf("dtmf rejects sample rate %d", rate)
			}
			if bits != 8 && bits != 16 {
				return pcmAudio{}, fmt.Errorf("dtmf transcribes 8-bit or 16-bit PCM only, got %d-bit", bits)
			}
			audio.channels = channels
			audio.sampleRate = rate
			audio.bitsPerSample = bits
			formatSeen = true
		case "data":
			audio.data = body
			audio.dataOffset = offset
			audio.dataSize = chunkSize
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if !formatSeen || audio.data == nil || audio.dataSize <= 0 || byteRate <= 0 {
		return pcmAudio{}, fmt.Errorf("dtmf transcribes wav containers only: missing PCM data")
	}
	audio.durationMillis = int64(audio.dataSize) * 1000 / byteRate
	return audio, nil
}

// toneEvent is one debounced keypad press with frame bounds.
type toneEvent struct {
	symbol     byte
	startFrame int
	endFrame   int
	confidence float64
}

var (
	rowFrequencies = []float64{697, 770, 852, 941}
	colFrequencies = []float64{1209, 1336, 1477, 1633}
	keypadSymbols  = [4][4]byte{
		{'1', '2', '3', 'A'},
		{'4', '5', '6', 'B'},
		{'7', '8', '9', 'C'},
		{'*', '0', '#', 'D'},
	}
)

func detectEvents(ctx context.Context, audio pcmAudio) ([]toneEvent, error) {
	frameSize := audio.sampleRate * frameMillis / 1000
	if frameSize < 2 {
		return nil, fmt.Errorf("dtmf rejects sample rate %d: frame too short", audio.sampleRate)
	}
	frameCount := (audio.dataSize * 8 / audio.bitsPerSample / audio.channels) / frameSize
	if frameCount < 1 {
		return nil, nil
	}
	coeffs := goertzelCoeffs(audio.sampleRate)
	frame := make([]float64, frameSize)
	raw := make([]rawFrame, 0, frameCount)
	for index := 0; index < frameCount; index++ {
		if index%contextCheckFrames == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("dtmf transcribe cancelled: %w", err)
			}
		}
		readFrame(audio, index*frameSize, frame)
		symbol, confidence := detectSymbol(frame, coeffs)
		raw = append(raw, rawFrame{symbol: symbol, confidence: confidence})
	}
	return debounce(raw), nil
}

// rawFrame is one frame of tone detection output.
type rawFrame struct {
	symbol     byte
	confidence float64
}

func goertzelCoeffs(sampleRate int) [8]float64 {
	var coeffs [8]float64
	for i, freq := range rowFrequencies {
		coeffs[i] = 2 * math.Cos(2*math.Pi*freq/float64(sampleRate))
	}
	for i, freq := range colFrequencies {
		coeffs[4+i] = 2 * math.Cos(2*math.Pi*freq/float64(sampleRate))
	}
	return coeffs
}

// readFrame decodes one mono frame normalized to [-1, 1].
func readFrame(audio pcmAudio, start int, frame []float64) {
	bytesPerSample := audio.bitsPerSample / 8
	stride := bytesPerSample * audio.channels
	for i := range frame {
		sum := 0.0
		for channel := 0; channel < audio.channels; channel++ {
			at := audio.dataOffset + (start+i)*stride + channel*bytesPerSample
			if at < 0 || at+bytesPerSample > len(audio.data) {
				continue
			}
			if audio.bitsPerSample == 16 {
				raw := int16(binary.LittleEndian.Uint16(audio.data[at : at+2]))
				sum += float64(raw) / 32768
			} else {
				sum += (float64(audio.data[at]) - 128) / 128
			}
		}
		frame[i] = sum / float64(audio.channels)
	}
}

// detectSymbol returns the keypad symbol in frame with its confidence, or
// zero when no tone qualifies. Confidence scales with the detection margin
// above the acceptance threshold.
func detectSymbol(frame []float64, coeffs [8]float64) (byte, float64) {
	energy := 0.0
	for _, value := range frame {
		energy += value * value
	}
	if math.Sqrt(energy/float64(len(frame))) < rmsFloor {
		return 0, 0
	}
	powers := goertzelPowers(frame, coeffs)
	bestRow, secondRow := 0, 1
	for i := 1; i < 4; i++ {
		if powers[i] > powers[bestRow] {
			secondRow, bestRow = bestRow, i
		} else if i != bestRow && powers[i] > powers[secondRow] {
			secondRow = i
		}
	}
	bestCol, secondCol := 4, 5
	for i := 5; i < 8; i++ {
		if powers[i] > powers[bestCol] {
			secondCol, bestCol = bestCol, i
		} else if i != bestCol && powers[i] > powers[secondCol] {
			secondCol = i
		}
	}
	rowPower, colPower := powers[bestRow], powers[bestCol]
	if rowPower < powerFloor || colPower < powerFloor {
		return 0, 0
	}
	rowMargin := rowPower / (powers[secondRow] + 1e-12)
	colMargin := colPower / (powers[secondCol] + 1e-12)
	if rowMargin < marginThreshold || colMargin < marginThreshold {
		return 0, 0
	}
	if twist := rowPower / (colPower + 1e-12); twist < twistLow || twist > twistHigh {
		return 0, 0
	}
	margin := math.Min(rowMargin, colMargin)
	confidence := (margin - 1) / (marginThreshold*marginThreshold - 1)
	return keypadSymbols[bestRow][bestCol-4], math.Min(1, math.Max(0, confidence))
}

func goertzelPowers(frame []float64, coeffs [8]float64) [8]float64 {
	var powers [8]float64
	for k, coeff := range coeffs {
		prev, prev2 := 0.0, 0.0
		for _, sample := range frame {
			current := sample + coeff*prev - prev2
			prev2, prev = prev, current
		}
		powers[k] = prev*prev + prev2*prev2 - coeff*prev*prev2
	}
	return powers
}

// debounce merges raw per-frame symbols into debounced tone events. A symbol
// becomes an event after minRunFrames consecutive frames; the same symbol
// resumes one event unless splitGapFrames silent frames intervene. Event
// confidence is the weakest frame confidence in the run.
func debounce(raw []rawFrame) []toneEvent {
	var events []toneEvent
	index := 0
	for index < len(raw) {
		symbol := raw[index].symbol
		if symbol == 0 {
			index++
			continue
		}
		end := index
		confidence := raw[index].confidence
		for end < len(raw) && raw[end].symbol == symbol {
			confidence = math.Min(confidence, raw[end].confidence)
			end++
		}
		if end-index >= minRunFrames {
			events = append(events, toneEvent{
				symbol:     symbol,
				startFrame: index,
				endFrame:   end - 1,
				confidence: confidence,
			})
		}
		index = end
	}
	return mergeCloseRepeats(events)
}

// mergeCloseRepeats rejoins same-symbol events that debounce split on a
// sub-splitGapFrames dropout, keeping one event per keypad press.
func mergeCloseRepeats(events []toneEvent) []toneEvent {
	if len(events) < 2 {
		return events
	}
	merged := events[:1]
	for _, event := range events[1:] {
		prev := &merged[len(merged)-1]
		if event.symbol == prev.symbol && event.startFrame-prev.endFrame-1 < splitGapFrames {
			prev.endFrame = event.endFrame
			prev.confidence = math.Min(prev.confidence, event.confidence)
			continue
		}
		merged = append(merged, event)
	}
	return merged
}

func groupSegments(events []toneEvent, sampleRate int, opts Options) ([]docmedia.Segment, error) {
	frameSize := sampleRate * frameMillis / 1000
	gapFrames := int(segmentGapMillis) / frameMillis
	var segments []docmedia.Segment
	start := 0
	flush := func(end int) error {
		text := make([]byte, 0, end-start)
		confidence := 1.0
		for _, event := range events[start:end] {
			text = append(text, event.symbol)
			confidence = math.Min(confidence, event.confidence)
		}
		if len(text) > opts.MaxSegmentChars {
			text = text[:opts.MaxSegmentChars]
		}
		if longestDigitRun(text) > opts.MaxPersistedDigitRun {
			return nil
		}
		if len(segments) >= opts.MaxSegments {
			return fmt.Errorf("dtmf decoded more than %d segments", opts.MaxSegments)
		}
		segments = append(segments, docmedia.Segment{
			SegmentID:   fmt.Sprintf("seg-%d", len(segments)+1),
			Text:        string(text),
			StartMillis: int64(events[start].startFrame*frameSize) * 1000 / int64(sampleRate),
			EndMillis:   int64((events[end-1].endFrame+1)*frameSize) * 1000 / int64(sampleRate),
			Confidence:  confidence,
		})
		return nil
	}
	for end := 1; end <= len(events); end++ {
		if end < len(events) && events[end].startFrame-events[end-1].endFrame-1 <= gapFrames {
			continue
		}
		if err := flush(end); err != nil {
			return nil, err
		}
		start = end
	}
	return segments, nil
}

func longestDigitRun(text []byte) int {
	longest, streak := 0, 0
	for _, c := range text {
		if c >= '0' && c <= '9' {
			streak++
			longest = max(longest, streak)
		} else {
			streak = 0
		}
	}
	return longest
}

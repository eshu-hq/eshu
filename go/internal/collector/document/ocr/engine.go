// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"slices"
	"time"
)

const (
	// localEngineName identifies the in-tree OCR backend in region metadata.
	localEngineName = "eshu-local-ocr"
	// localEngineVersion versions the template set and matching rules.
	localEngineVersion = "0.1.0"

	defaultLocalMaxInputBytes = 8 << 20
	defaultLocalMaxPixels     = 8_000_000
	defaultLocalTimeout       = 10 * time.Second
	defaultLocalMaxRegions    = 512
	defaultLocalMinConfidence = 0.60
)

// LocalOptions bounds the work one LocalEngine recognition may perform.
// Non-positive fields take deterministic defaults; callers needing tighter
// QA-tier limits set explicit values.
type LocalOptions struct {
	// MaxInputBytes caps the encoded image body accepted for decoding.
	MaxInputBytes int64
	// MaxPixels caps width*height of the decoded image.
	MaxPixels int64
	// Timeout bounds a single Recognize call. Non-positive takes the default.
	Timeout time.Duration
	// MaxRegions caps emitted regions; recognition fails past the cap
	// instead of silently truncating.
	MaxRegions int
	// MinConfidence is the minimum template match score (0,1] kept as text.
	MinConfidence float64
}

// LocalEngine is a dependency-free OCR backend behind the Engine contract.
//
// It decodes PNG, JPEG, and first-frame GIF with the standard library,
// segments lines and glyphs by pixel projection, and matches glyphs against
// an embedded 5x7 template set. It performs no network access, subprocess
// execution, temp file handling, or logging: recognized text only reaches
// facts through Extract, which redacts sensitive regions before persistence.
//
// Accuracy is QA-tier by design: the template set covers uppercase
// alphanumerics and basic punctuation at any uniform scale, while mixed-case
// prose, proportional fonts, and tight multi-line pitch are known limits
// documented in the package README.
type LocalEngine struct {
	maxInputBytes int64
	maxPixels     int64
	timeout       time.Duration
	maxRegions    int
	minConfidence float64
}

// NewLocalEngine returns a LocalEngine with defaults applied.
func NewLocalEngine(opts LocalOptions) *LocalEngine {
	engine := &LocalEngine{
		maxInputBytes: opts.MaxInputBytes,
		maxPixels:     opts.MaxPixels,
		timeout:       opts.Timeout,
		maxRegions:    opts.MaxRegions,
		minConfidence: opts.MinConfidence,
	}
	if engine.maxInputBytes <= 0 {
		engine.maxInputBytes = defaultLocalMaxInputBytes
	}
	if engine.maxPixels <= 0 {
		engine.maxPixels = defaultLocalMaxPixels
	}
	if engine.timeout <= 0 {
		engine.timeout = defaultLocalTimeout
	}
	if engine.maxRegions <= 0 {
		engine.maxRegions = defaultLocalMaxRegions
	}
	if engine.minConfidence <= 0 {
		engine.minConfidence = defaultLocalMinConfidence
	}
	return engine
}

// Recognize decodes img and returns template-matched text regions in reading
// order. It honors context cancellation and the configured timeout, and
// fails on over-cap input instead of partially recognizing it.
func (e *LocalEngine) Recognize(ctx context.Context, img Image) (EngineResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return EngineResult{}, fmt.Errorf("local OCR context: %w", err)
	}
	if len(img.Body) == 0 {
		return EngineResult{}, fmt.Errorf("local OCR rejects empty image body")
	}
	if int64(len(img.Body)) > e.maxInputBytes {
		return EngineResult{}, fmt.Errorf("local OCR input %d bytes exceeds cap %d", len(img.Body), e.maxInputBytes)
	}
	decoded, _, err := image.Decode(bytes.NewReader(img.Body))
	if err != nil {
		return EngineResult{}, fmt.Errorf("local OCR decode: %w", err)
	}
	bounds := decoded.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return EngineResult{}, fmt.Errorf("local OCR rejects degenerate dimensions %dx%d", width, height)
	}
	if int64(width)*int64(height) > e.maxPixels {
		return EngineResult{}, fmt.Errorf("local OCR pixels %d exceed cap %d", int64(width)*int64(height), e.maxPixels)
	}
	grid, err := binarize(ctx, decoded, bounds)
	if err != nil {
		return EngineResult{}, err
	}
	regions := []Region{}
	regionIndex := 0
	for _, band := range lineBands(grid) {
		if err := ctx.Err(); err != nil {
			return EngineResult{}, fmt.Errorf("local OCR context: %w", err)
		}
		for _, word := range bandWords(grid, band, e.minConfidence) {
			regions = append(regions, Region{
				RegionID:   fmt.Sprintf("local-%04d", regionIndex+1),
				Text:       word.text,
				Bounds:     normalizeBounds(word.box, width, height),
				Confidence: word.confidence,
			})
			regionIndex++
			if len(regions) > e.maxRegions {
				return EngineResult{}, fmt.Errorf("local OCR regions exceed cap %d", e.maxRegions)
			}
		}
	}
	return EngineResult{
		EngineName:    localEngineName,
		EngineVersion: localEngineVersion,
		Language:      "en",
		Regions:       regions,
	}, nil
}

// pixelBox is an inclusive-exclusive pixel rectangle.
type pixelBox struct {
	x0, y0, x1, y1 int
}

// localWord is one matched word with its pixel box and mean glyph score.
type localWord struct {
	text       string
	box        pixelBox
	confidence float64
}

// binarize thresholds luminance at mid-gray. The grid is bounded by the
// pixel cap checked in Recognize, and long decodes stay interruptible
// through periodic context checks.
func binarize(ctx context.Context, img image.Image, bounds image.Rectangle) ([][]bool, error) {
	width, height := bounds.Dx(), bounds.Dy()
	grid := make([][]bool, height)
	for y := 0; y < height; y++ {
		if y%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("local OCR context: %w", err)
			}
		}
		row := make([]bool, width)
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			luma := (299*r + 587*g + 114*b) / 1000
			row[x] = luma < 0x8000
		}
		grid[y] = row
	}
	return grid, nil
}

// lineBands groups rows with ink into bands, merging dotted-glyph gaps
// (colon, period, comma tails) that are small relative to the tallest band.
func lineBands(grid [][]bool) []pixelBox {
	height := len(grid)
	if height == 0 {
		return nil
	}
	width := len(grid[0])
	ink := make([]bool, height)
	for y, row := range grid {
		for _, dark := range row {
			if dark {
				ink[y] = true
				break
			}
		}
	}
	bands := []pixelBox{}
	for y := 0; y < height; y++ {
		if !ink[y] {
			continue
		}
		top := y
		for y+1 < height && ink[y+1] {
			y++
		}
		bands = append(bands, pixelBox{x0: 0, y0: top, x1: width, y1: y + 1})
	}
	if len(bands) < 2 {
		return bands
	}
	tallest := 0
	for _, band := range bands {
		if height := band.y1 - band.y0; height > tallest {
			tallest = height
		}
	}
	merged := []pixelBox{bands[0]}
	for _, band := range bands[1:] {
		last := &merged[len(merged)-1]
		if gap := band.y0 - last.y1; gap <= tallest/3 {
			last.y1 = band.y1
		} else {
			merged = append(merged, band)
		}
	}
	return merged
}

// bandWords segments one line band into glyph boxes, matches them, and
// groups accepted glyphs into words split by wide gaps or unknown glyphs.
func bandWords(grid [][]bool, band pixelBox, minConfidence float64) []localWord {
	lineHeight := band.y1 - band.y0
	cell := lineHeight / 7
	if cell < 1 {
		cell = 1
	}
	glyphs := []matchedGlyph{}
	for _, box := range glyphBoxes(grid, band) {
		char, score := matchGlyph(grid, band, box, cell)
		if score < minConfidence {
			continue
		}
		glyphs = append(glyphs, matchedGlyph{box: box, char: char, score: score})
	}
	if len(glyphs) == 0 {
		return nil
	}
	wordGap := adaptiveWordGap(glyphs)
	words := []localWord{}
	current := localWord{box: glyphs[0].box}
	var text []rune
	var scores []float64
	flush := func() {
		if len(text) == 0 {
			return
		}
		sum := 0.0
		for _, score := range scores {
			sum += score
		}
		current.text = string(text)
		current.confidence = sum / float64(len(scores))
		words = append(words, current)
	}
	for _, item := range glyphs {
		if len(text) > 0 && item.box.x0-current.box.x1 > wordGap {
			flush()
			current = localWord{box: item.box}
			text = nil
			scores = nil
		}
		if len(text) == 0 {
			current.box = item.box
		} else {
			current.box.x1 = item.box.x1
		}
		text = append(text, item.char)
		scores = append(scores, item.score)
	}
	flush()
	return words
}

// matchedGlyph is one accepted template match with its pixel box and score.
type matchedGlyph struct {
	box   pixelBox
	char  rune
	score float64
}

// adaptiveWordGap splits words where a gap dwarfs the line tracking. The
// median inter-glyph gap estimates tracking without font metrics, so narrow
// centered glyphs such as '.' stay joined while true word spaces split.
// A lone glyph pair never splits.
func adaptiveWordGap(glyphs []matchedGlyph) int {
	return wordGapFor(glyphGapList(glyphs))
}

// glyphGapList returns consecutive glyph gaps in band order.
func glyphGapList(glyphs []matchedGlyph) []int {
	gaps := make([]int, 0, len(glyphs))
	for i := 1; i < len(glyphs); i++ {
		if gap := glyphs[i].box.x0 - glyphs[i-1].box.x1; gap > 0 {
			gaps = append(gaps, gap)
		}
	}
	return gaps
}

// wordGapFor returns the split threshold for a gap list: triple the median
// tracking gap, floored so speckle never forces a split. Words split only
// past the threshold, so a gap exactly at the boundary stays joined and
// PII-like tokens survive for redaction.
func wordGapFor(gaps []int) int {
	if len(gaps) == 0 {
		return 2
	}
	ordered := append([]int(nil), gaps...)
	slices.Sort(ordered)
	median := ordered[len(ordered)/2]
	if threshold := 3 * median; threshold > 2 {
		return threshold
	}
	return 2
}

// glyphBoxes finds ink-column runs inside a line band, skipping speckle and
// rule-sized boxes that cannot be template glyphs.
func glyphBoxes(grid [][]bool, band pixelBox) []pixelBox {
	boxes := []pixelBox{}
	for x := band.x0; x < band.x1; x++ {
		dark := false
		for y := band.y0; y < band.y1; y++ {
			if grid[y][x] {
				dark = true
				break
			}
		}
		if !dark {
			continue
		}
		left := x
		for x+1 < band.x1 {
			columnDark := false
			for y := band.y0; y < band.y1; y++ {
				if grid[y][x+1] {
					columnDark = true
					break
				}
			}
			if !columnDark {
				break
			}
			x++
		}
		box := pixelBox{x0: left, y0: band.y0, x1: x + 1, y1: band.y1}
		if box.x1-box.x0 >= 2 && box.y1-box.y0 >= 2 {
			boxes = append(boxes, box)
		}
	}
	return boxes
}

// matchGlyph samples a glyph box onto the 5x7 template grid and returns the
// best template and its match fraction. Templates iterate in fixed charset
// order so ties resolve deterministically.
//
// Sampling keeps the line cell size instead of stretching the box: the
// template frame centers on the box horizontally and anchors on the band
// top vertically. Narrow glyphs such as '.' therefore keep their template
// columns instead of smearing across the full grid.
func matchGlyph(grid [][]bool, band pixelBox, box pixelBox, cell int) (rune, float64) {
	center := (box.x0 + box.x1) / 2
	sampled := [7][5]bool{}
	for cy := 0; cy < 7; cy++ {
		for cx := 0; cx < 5; cx++ {
			x0 := center + (cx-2)*cell - cell/2
			y0 := band.y0 + cy*cell
			dark, total := 0, 0
			for y := y0; y < y0+cell; y++ {
				for x := x0; x < x0+cell; x++ {
					if y < 0 || y >= len(grid) || x < 0 || x >= len(grid[y]) {
						continue
					}
					total++
					if grid[y][x] {
						dark++
					}
				}
			}
			sampled[cy][cx] = total > 0 && dark*2 >= total
		}
	}
	best, bestScore := rune(0), 0.0
	for _, glyph := range localCharset {
		template := localFontCells(glyph)
		hits := 0
		for cy := 0; cy < 7; cy++ {
			for cx := 0; cx < 5; cx++ {
				if sampled[cy][cx] == template[cy][cx] {
					hits++
				}
			}
		}
		if score := float64(hits) / 35; score > bestScore {
			best, bestScore = glyph, score
		}
	}
	return best, bestScore
}

// normalizeBounds converts a pixel box to clamped 0-1 coordinates.
func normalizeBounds(box pixelBox, width, height int) Bounds {
	return Bounds{
		X:      clamp01(float64(box.x0) / float64(width)),
		Y:      clamp01(float64(box.y0) / float64(height)),
		Width:  clamp01(float64(box.x1-box.x0) / float64(width)),
		Height: clamp01(float64(box.y1-box.y0) / float64(height)),
	}
}

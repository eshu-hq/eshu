// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import "math"

const (
	// resampleZeroCrossings is the sinc half-width in zero crossings of the
	// low-pass kernel.
	resampleZeroCrossings = 16
	// resampleRolloff keeps the cutoff below the lower Nyquist rate so the
	// Blackman transition band does not alias.
	resampleRolloff = 0.90
	// resampleFracBits is the fixed-point precision of the integer taps.
	resampleFracBits = 15
	// resampleMaxTable caps precomputed phases. When the exact phase count
	// is larger (a source rate nearly coprime with 16 kHz), the fractional
	// delay is quantized to 1/resampleMaxTable of a source frame, which
	// bounds the table to about 1.7 MiB at 192 kHz.
	resampleMaxTable = 1024
)

// resampler converts mono 16-bit-scale samples from srcRate to 16 kHz with a
// Blackman-windowed sinc low-pass filter. Taps are computed once per phase in
// float64 (pure-Go math, every product rounded by an explicit conversion so
// the compiler cannot fuse it) and quantized to integers; filtering itself
// is integer-only. The output is therefore bit-identical across
// architectures for identical input.
type resampler struct {
	phases   int64 // L: distinct output phases, 16000/gcd(src, 16000)
	step     int64 // M: source frames advanced per L output frames
	halfTaps int64
	taps     [][]int32 // [table phase][2*halfTaps]
}

func newResampler(srcRate int) *resampler {
	src := int64(srcRate)
	g := gcd(src, targetSampleRate)
	r := &resampler{phases: targetSampleRate / g, step: src / g}
	cutoff := resampleRolloff * math.Min(1, float64(targetSampleRate)/float64(src))
	r.halfTaps = int64(math.Ceil(resampleZeroCrossings / cutoff))
	width := float64(r.halfTaps)
	table := min(r.phases, resampleMaxTable)
	r.taps = make([][]int32, table)
	for phase := range table {
		frac := float64(phase) / float64(table)
		taps := make([]int32, 2*r.halfTaps)
		var sum int64
		for j := range 2 * r.halfTaps {
			// Tap j weighs source frame base-halfTaps+1+j; x is its distance
			// from the output position in source frames.
			x := float64(frac + float64(r.halfTaps-1-j))
			weight := float64(cutoff * sinc(float64(cutoff*x)) * blackman(x, width))
			taps[j] = int32(math.Round(float64(weight * (1 << resampleFracBits))))
			sum += int64(taps[j])
		}
		// Force unity DC gain exactly on the tap nearest the output position.
		taps[r.halfTaps-1] += int32((1 << resampleFracBits) - sum)
		r.taps[phase] = taps
	}
	return r
}

// at returns output frame i given a mono sample accessor over srcFrames.
func (r *resampler) at(i, srcFrames int64, sample func(int64) int32) int16 {
	position := i * r.step
	base := position / r.phases
	taps := r.taps[(position%r.phases)*int64(len(r.taps))/r.phases]
	var acc int64
	first := base - r.halfTaps + 1
	for j, tap := range taps {
		k := first + int64(j)
		if k < 0 || k >= srcFrames || tap == 0 {
			continue
		}
		acc += int64(sample(k)) * int64(tap)
	}
	half := int64(1) << (resampleFracBits - 1)
	if acc >= 0 {
		acc = (acc + half) >> resampleFracBits
	} else {
		acc = -((-acc + half) >> resampleFracBits)
	}
	return int16(max(math.MinInt16, min(math.MaxInt16, acc)))
}

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	px := float64(math.Pi * x)
	return math.Sin(px) / px
}

func blackman(x, width float64) float64 {
	if math.Abs(x) >= width {
		return 0
	}
	t := float64(math.Pi * x / width)
	return float64(0.42+float64(0.5*math.Cos(t))) + float64(0.08*math.Cos(float64(2*t)))
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

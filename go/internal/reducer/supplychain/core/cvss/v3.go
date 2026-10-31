// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import "math"

// v3BaseKeys are the eight metrics a CVSS v3.x vector must carry.
var v3BaseKeys = []string{"AV", "AC", "PR", "UI", "S", "C", "I", "A"}

// v3Metrics lists every metric a CVSS v3.0 or v3.1 vector may carry with its
// allowed values: the base group, then the temporal group (E, RL, RC) and the
// environmental group (CR, IR, AR, M*). Only the base group affects BaseScore.
var v3Metrics = metricSet{
	"AV":  {"N", "A", "L", "P"},
	"AC":  {"L", "H"},
	"PR":  {"N", "L", "H"},
	"UI":  {"N", "R"},
	"S":   {"U", "C"},
	"C":   {"H", "L", "N"},
	"I":   {"H", "L", "N"},
	"A":   {"H", "L", "N"},
	"E":   {"X", "U", "P", "F", "H"},
	"RL":  {"X", "O", "T", "W", "U"},
	"RC":  {"X", "U", "R", "C"},
	"CR":  {"X", "L", "M", "H"},
	"IR":  {"X", "L", "M", "H"},
	"AR":  {"X", "L", "M", "H"},
	"MAV": {"X", "N", "A", "L", "P"},
	"MAC": {"X", "L", "H"},
	"MPR": {"X", "N", "L", "H"},
	"MUI": {"X", "N", "R"},
	"MS":  {"X", "U", "C"},
	"MC":  {"X", "N", "L", "H"},
	"MI":  {"X", "N", "L", "H"},
	"MA":  {"X", "N", "L", "H"},
}

// Weights from CVSS v3.1 specification section 7.4.
var (
	v3AttackVector        = map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}
	v3AttackComplexity    = map[string]float64{"L": 0.77, "H": 0.44}
	v3UserInteraction     = map[string]float64{"N": 0.85, "R": 0.62}
	v3PrivilegesUnchanged = map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	v3PrivilegesChanged   = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	v3ImpactWeight        = map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
)

// scoreV3 computes the CVSS v3.0 or v3.1 base score from validated metrics.
// The weights and formulas are the "Base Metrics Equations" of the CVSS v3.1
// specification, https://www.first.org/cvss/v3.1/specification-document
// section 7.1, which keeps the v3.0 formulas. The two versions differ only in
// Roundup: v3.1 defines it on integers to avoid floating-point artifacts,
// v3.0 uses the smallest one-decimal number not below the input.
func scoreV3(metrics map[string]string, version string) float64 {
	scopeChanged := metrics["S"] == "C"

	privileges := v3PrivilegesUnchanged
	if scopeChanged {
		privileges = v3PrivilegesChanged
	}

	iss := 1 - float64((1-v3ImpactWeight[metrics["C"]])*(1-v3ImpactWeight[metrics["I"]])*(1-v3ImpactWeight[metrics["A"]]))
	var impact float64
	if scopeChanged {
		// Explicit conversions stop the compiler from fusing the multiply and
		// subtract, which would round differently from the specification.
		impact = float64(7.52*(iss-0.029)) - float64(3.25*math.Pow(iss-0.02, 15))
	} else {
		impact = 6.42 * iss
	}
	if impact <= 0 {
		return 0
	}
	exploitability := 8.22 * v3AttackVector[metrics["AV"]] * v3AttackComplexity[metrics["AC"]] *
		privileges[metrics["PR"]] * v3UserInteraction[metrics["UI"]]
	round := roundUpV31
	if version == "3.0" {
		round = roundUpV30
	}
	if scopeChanged {
		return round(math.Min(float64(1.08*(impact+exploitability)), 10))
	}
	return round(math.Min(impact+exploitability, 10))
}

// roundUpV31 is the Roundup function of CVSS v3.1 specification Appendix A.
func roundUpV31(input float64) float64 {
	scaled := int64(math.Round(input * 100000))
	if scaled%10000 == 0 {
		return float64(scaled) / 100000.0
	}
	return (math.Floor(float64(scaled)/10000) + 1) / 10.0
}

// roundUpV30 is the Roundup function of the CVSS v3.0 specification: the
// smallest number with one decimal place that is not below the input.
func roundUpV30(input float64) float64 {
	return math.Ceil(input*10) / 10
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"math"
	"strconv"
	"strings"
)

// v4BaseKeys are the eleven metrics a CVSS v4.0 vector must carry.
var v4BaseKeys = []string{"AV", "AC", "AT", "PR", "UI", "VC", "VI", "VA", "SC", "SI", "SA"}

// v4Metrics lists every metric a CVSS v4.0 vector may carry with its allowed
// values: the base group, the threat group (E), the environmental group
// (CR, IR, AR, M*), and the supplemental group (S, AU, R, V, RE, U). Only the
// base group affects BaseScore.
var v4Metrics = metricSet{
	"AV":  {"N", "A", "L", "P"},
	"AC":  {"L", "H"},
	"AT":  {"N", "P"},
	"PR":  {"N", "L", "H"},
	"UI":  {"N", "P", "A"},
	"VC":  {"H", "L", "N"},
	"VI":  {"H", "L", "N"},
	"VA":  {"H", "L", "N"},
	"SC":  {"H", "L", "N"},
	"SI":  {"H", "L", "N"},
	"SA":  {"H", "L", "N"},
	"E":   {"X", "A", "P", "U"},
	"CR":  {"X", "H", "M", "L"},
	"IR":  {"X", "H", "M", "L"},
	"AR":  {"X", "H", "M", "L"},
	"MAV": {"X", "N", "A", "L", "P"},
	"MAC": {"X", "L", "H"},
	"MAT": {"X", "N", "P"},
	"MPR": {"X", "N", "L", "H"},
	"MUI": {"X", "N", "P", "A"},
	"MVC": {"X", "H", "L", "N"},
	"MVI": {"X", "H", "L", "N"},
	"MVA": {"X", "H", "L", "N"},
	"MSC": {"X", "H", "L", "N"},
	"MSI": {"X", "S", "H", "L", "N"},
	"MSA": {"X", "S", "H", "L", "N"},
	"S":   {"X", "N", "P"},
	"AU":  {"X", "N", "Y"},
	"R":   {"X", "A", "U", "I"},
	"V":   {"X", "D", "C"},
	"RE":  {"X", "L", "M", "H"},
	"U":   {"X", "Clear", "Green", "Amber", "Red"},
}

// Metric level tables from the CVSS v4.0 reference calculator
// (cvss_score.js): how far a metric value sits from the most severe value of
// its class. They drive the severity-distance step of the scoring algorithm.
var v4Levels = map[string]map[string]float64{
	"AV": {"N": 0.0, "A": 0.1, "L": 0.2, "P": 0.3},
	"PR": {"N": 0.0, "L": 0.1, "H": 0.2},
	"UI": {"N": 0.0, "P": 0.1, "A": 0.2},
	"AC": {"L": 0.0, "H": 0.1},
	"AT": {"N": 0.0, "P": 0.1},
	"VC": {"H": 0.0, "L": 0.1, "N": 0.2},
	"VI": {"H": 0.0, "L": 0.1, "N": 0.2},
	"VA": {"H": 0.0, "L": 0.1, "N": 0.2},
	"SC": {"H": 0.1, "L": 0.2, "N": 0.3},
	"SI": {"S": 0.0, "H": 0.1, "L": 0.2, "N": 0.3},
	"SA": {"S": 0.0, "H": 0.1, "L": 0.2, "N": 0.3},
	"CR": {"H": 0.0, "M": 0.1, "L": 0.2},
	"IR": {"H": 0.0, "M": 0.1, "L": 0.2},
	"AR": {"H": 0.0, "M": 0.1, "L": 0.2},
}

// v4DistanceKeys fixes the metric order of the severity-distance sums so the
// floating-point additions happen in the same order as the reference.
var (
	v4DistanceEQ1    = []string{"AV", "PR", "UI"}
	v4DistanceEQ2    = []string{"AC", "AT"}
	v4DistanceEQ3EQ6 = []string{"VC", "VI", "VA", "CR", "IR", "AR"}
	v4DistanceEQ4    = []string{"SC", "SI", "SA"}
	v4DistanceAll    = []string{"AV", "PR", "UI", "AC", "AT", "VC", "VI", "VA", "SC", "SI", "SA", "CR", "IR", "AR"}
)

// scoreV4 computes the CVSS v4.0 base score from validated metrics, following
// the scoring algorithm of the CVSS v4.0 specification
// (https://www.first.org/cvss/v4.0/specification-document, section 8) as
// implemented by FIRST's reference calculator
// (https://github.com/FIRSTdotorg/cvss-v4-calculator, BSD-2-Clause, copyright
// FIRST, Red Hat, and contributors).
//
// Only the base group is scored, which is the reference calculator with the
// threat metric at X (read as E:A) and the environmental security
// requirements at X (read as H). A vector with no impact on either system
// scores 0.0.
func scoreV4(metrics map[string]string) float64 {
	m := make(map[string]string, len(v4BaseKeys)+3)
	for _, key := range v4BaseKeys {
		m[key] = metrics[key]
	}
	m["CR"], m["IR"], m["AR"] = "H", "H", "H"

	if m["VC"] == "N" && m["VI"] == "N" && m["VA"] == "N" &&
		m["SC"] == "N" && m["SI"] == "N" && m["SA"] == "N" {
		return 0
	}

	eq1, eq2, eq3, eq4, eq5, eq6 := v4EquivalenceClasses(m)
	macro := strconv.Itoa(eq1) + strconv.Itoa(eq2) + strconv.Itoa(eq3) +
		strconv.Itoa(eq4) + strconv.Itoa(eq5) + strconv.Itoa(eq6)
	value := v4MacroScores[macro]

	lower := func(a, b, c, d, e, f int) (float64, bool) {
		score, ok := v4MacroScores[strconv.Itoa(a)+strconv.Itoa(b)+strconv.Itoa(c)+
			strconv.Itoa(d)+strconv.Itoa(e)+strconv.Itoa(f)]
		return score, ok
	}
	next1, ok1 := lower(eq1+1, eq2, eq3, eq4, eq5, eq6)
	next2, ok2 := lower(eq1, eq2+1, eq3, eq4, eq5, eq6)
	next36, ok36 := v4NextLowerEQ3EQ6(eq1, eq2, eq3, eq4, eq5, eq6)
	next4, ok4 := lower(eq1, eq2, eq3, eq4+1, eq5, eq6)
	next5, ok5 := lower(eq1, eq2, eq3, eq4, eq5+1, eq6)

	distances := v4SeverityDistances(m, eq1, eq2, eq3, eq4, eq5, eq6)
	const step = 0.1
	var sum float64
	existing := 0
	if ok1 {
		existing++
		percent := distances.sum(m, v4DistanceEQ1) / float64(v4MaxSeverityEQ1[eq1]*step)
		sum += float64((value - next1) * percent)
	}
	if ok2 {
		existing++
		percent := distances.sum(m, v4DistanceEQ2) / float64(v4MaxSeverityEQ2[eq2]*step)
		sum += float64((value - next2) * percent)
	}
	if ok36 {
		existing++
		percent := distances.sum(m, v4DistanceEQ3EQ6) / float64(v4MaxSeverityEQ3EQ6[eq3][eq6]*step)
		sum += float64((value - next36) * percent)
	}
	if ok4 {
		existing++
		percent := distances.sum(m, v4DistanceEQ4) / float64(v4MaxSeverityEQ4[eq4]*step)
		sum += float64((value - next4) * percent)
	}
	if ok5 {
		// EQ5 (threat) never moves the score within a MacroVector, so its
		// proportion is 0 but it still counts toward the mean.
		existing++
		sum += float64((value - next5) * 0)
	}
	if existing > 0 {
		value -= sum / float64(existing)
	}
	value = math.Max(0, math.Min(10, value))
	return math.Floor(float64(value*10)+0.5) / 10
}

// v4EquivalenceClasses maps base metrics to the six MacroVector digits.
func v4EquivalenceClasses(m map[string]string) (eq1, eq2, eq3, eq4, eq5, eq6 int) {
	switch {
	case m["AV"] == "N" && m["PR"] == "N" && m["UI"] == "N":
		eq1 = 0
	case (m["AV"] == "N" || m["PR"] == "N" || m["UI"] == "N") && m["AV"] != "P":
		eq1 = 1
	default:
		eq1 = 2
	}
	if m["AC"] == "L" && m["AT"] == "N" {
		eq2 = 0
	} else {
		eq2 = 1
	}
	switch {
	case m["VC"] == "H" && m["VI"] == "H":
		eq3 = 0
	case m["VC"] == "H" || m["VI"] == "H" || m["VA"] == "H":
		eq3 = 1
	default:
		eq3 = 2
	}
	// Base-only scoring has no modified subsequent-system integrity or
	// availability, so EQ4 is never 0.
	if m["SC"] == "H" || m["SI"] == "H" || m["SA"] == "H" {
		eq4 = 1
	} else {
		eq4 = 2
	}
	eq5 = 0 // E:X reads as E:A.
	// With the security requirements at H, EQ6 reduces to "any of VC, VI, VA
	// is High".
	if (m["CR"] == "H" && m["VC"] == "H") || (m["IR"] == "H" && m["VI"] == "H") || (m["AR"] == "H" && m["VA"] == "H") {
		eq6 = 0
	} else {
		eq6 = 1
	}
	return eq1, eq2, eq3, eq4, eq5, eq6
}

// v4NextLowerEQ3EQ6 returns the score of the next lower MacroVector along the
// combined EQ3/EQ6 axis. EQ3 and EQ6 are related, so the next lower step
// depends on both digits, exactly as in the reference calculator. A 00 class
// has two lower neighbours and takes the higher of the two scores.
func v4NextLowerEQ3EQ6(eq1, eq2, eq3, eq4, eq5, eq6 int) (float64, bool) {
	key := func(c3, c6 int) string {
		return strconv.Itoa(eq1) + strconv.Itoa(eq2) + strconv.Itoa(c3) +
			strconv.Itoa(eq4) + strconv.Itoa(eq5) + strconv.Itoa(c6)
	}
	switch {
	case eq3 == 1 && eq6 == 1, eq3 == 0 && eq6 == 1:
		score, ok := v4MacroScores[key(eq3+1, eq6)]
		return score, ok
	case eq3 == 1 && eq6 == 0:
		score, ok := v4MacroScores[key(eq3, eq6+1)]
		return score, ok
	case eq3 == 0 && eq6 == 0:
		left, leftOK := v4MacroScores[key(eq3, eq6+1)]
		right, rightOK := v4MacroScores[key(eq3+1, eq6)]
		if leftOK && rightOK && left > right {
			return left, true
		}
		return right, rightOK
	default:
		// EQ3 = 2 has no lower class.
		return 0, false
	}
}

// v4Distances holds the per-metric distance between the vector being scored
// and the nearest highest-severity vector of its MacroVector.
type v4Distances map[string]float64

// sum adds the distances of keys in the reference order.
func (d v4Distances) sum(_ map[string]string, keys []string) float64 {
	var total float64
	for _, key := range keys {
		total += d[key]
	}
	return total
}

// v4SeverityDistances finds the first highest-severity vector of the
// MacroVector that is at least as severe as m in every metric and returns the
// per-metric distance to it.
func v4SeverityDistances(m map[string]string, eq1, eq2, eq3, eq4, eq5, eq6 int) v4Distances {
	distances := make(v4Distances, len(v4DistanceAll))
	for _, candidate := range v4MaxVectors(eq1, eq2, eq3, eq4, eq5, eq6) {
		negative := false
		for _, key := range v4DistanceAll {
			distances[key] = v4Levels[key][m[key]] - v4Levels[key][candidate[key]]
			if distances[key] < 0 {
				negative = true
			}
		}
		if !negative {
			break
		}
	}
	return distances
}

// v4MaxVectors composes the highest-severity vectors of a MacroVector from
// the per-equivalence-class maxima, in the reference iteration order.
func v4MaxVectors(eq1, eq2, eq3, eq4, eq5, eq6 int) []map[string]string {
	var out []map[string]string
	for _, a := range v4MaxComposedEQ1[eq1] {
		for _, b := range v4MaxComposedEQ2[eq2] {
			for _, c := range v4MaxComposedEQ3EQ6[eq3][eq6] {
				for _, d := range v4MaxComposedEQ4[eq4] {
					for _, e := range v4MaxComposedEQ5[eq5] {
						out = append(out, parseMaxVector(a+b+c+d+e))
					}
				}
			}
		}
	}
	return out
}

// parseMaxVector splits a composed maximum such as "AV:N/PR:N/UI:N/AC:L/..."
// into its metric values.
func parseMaxVector(composed string) map[string]string {
	fields := strings.Split(strings.TrimSuffix(composed, "/"), "/")
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, _ := strings.Cut(field, ":")
		out[key] = value
	}
	return out
}

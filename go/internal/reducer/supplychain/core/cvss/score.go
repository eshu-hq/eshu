// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Result is a CVSS base score computed from a vector string.
type Result struct {
	// Score is the base score on the 0.0 to 10.0 scale, rounded to one
	// decimal place by the rule of the vector's version.
	Score float64
	// Version is the CVSS version named by the vector prefix: "3.0", "3.1",
	// or "4.0".
	Version string
}

var (
	// ErrMalformedVector is returned when a vector string is not a valid
	// CVSS vector of a supported version: a missing or duplicated base
	// metric, an unknown metric or value, or broken separators.
	ErrMalformedVector = errors.New("cvss: malformed vector")
	// ErrUnsupportedVersion is returned when the vector names a CVSS version
	// this package does not score (v2 vectors carry no prefix; v3.2 and v5
	// do not exist in the published specifications).
	ErrUnsupportedVersion = errors.New("cvss: unsupported version")
)

// maxVectorLength bounds the vector BaseScore will parse. The longest real
// vector, CVSS v4.0 with every base, threat, environmental, and supplemental
// metric, is under 200 bytes; the cap leaves headroom and keeps a hostile
// multi-megabyte value from being split into segments before it is refused.
const maxVectorLength = 512

// metricSet lists, for each metric key a version accepts, the values it
// allows. Keys in the base group are required exactly once; every other key
// is optional.
type metricSet map[string][]string

// BaseScore computes the CVSS base score of a v3.0, v3.1, or v4.0 vector.
//
// Threat, environmental, and supplemental metrics in the vector are validated
// and then ignored: the result is always the base score, the value sources
// publish as their CVSS score. BaseScore never panics. On any invalid input
// it returns a zero Result and an error wrapping ErrMalformedVector or
// ErrUnsupportedVersion, so a caller never receives a score it should not
// trust.
func BaseScore(vector string) (Result, error) {
	vector = strings.TrimSpace(vector)
	if len(vector) > maxVectorLength {
		return Result{}, fmt.Errorf("%w: vector is too long (%d bytes)", ErrMalformedVector, len(vector))
	}
	prefix, rest, found := strings.Cut(vector, "/")
	version, ok := strings.CutPrefix(prefix, "CVSS:")
	if !ok {
		if strings.Contains(prefix, ":") {
			return Result{}, fmt.Errorf("%w: no CVSS: prefix (a CVSS v2 vector?)", ErrUnsupportedVersion)
		}
		return Result{}, fmt.Errorf("%w: no CVSS: prefix", ErrMalformedVector)
	}
	if !found || rest == "" {
		return Result{}, fmt.Errorf("%w: no metrics after %q", ErrMalformedVector, prefix)
	}
	switch version {
	case "3.0", "3.1":
		metrics, err := parseMetrics(rest, v3Metrics, v3BaseKeys)
		if err != nil {
			return Result{}, err
		}
		return Result{Score: scoreV3(metrics, version), Version: version}, nil
	case "4.0":
		metrics, err := parseMetrics(rest, v4Metrics, v4BaseKeys)
		if err != nil {
			return Result{}, err
		}
		return Result{Score: scoreV4(metrics), Version: version}, nil
	default:
		return Result{}, fmt.Errorf("%w: CVSS:%s", ErrUnsupportedVersion, version)
	}
}

// parseMetrics splits the metric part of a vector into a key to value map. It
// rejects empty segments, a key repeated, a key or value the metric set does
// not list, and a missing base key.
func parseMetrics(rest string, allowed metricSet, baseKeys []string) (map[string]string, error) {
	segments := strings.Split(rest, "/")
	metrics := make(map[string]string, len(segments))
	for _, segment := range segments {
		key, value, ok := strings.Cut(segment, ":")
		if !ok || key == "" || value == "" || strings.Contains(value, ":") {
			return nil, fmt.Errorf("%w: segment %q is not metric:value", ErrMalformedVector, truncate(segment))
		}
		values, known := allowed[key]
		if !known {
			return nil, fmt.Errorf("%w: unknown metric %q", ErrMalformedVector, truncate(key))
		}
		if !slices.Contains(values, value) {
			return nil, fmt.Errorf("%w: invalid value %q for metric %s", ErrMalformedVector, truncate(value), key)
		}
		if _, dup := metrics[key]; dup {
			return nil, fmt.Errorf("%w: duplicate metric %s", ErrMalformedVector, key)
		}
		metrics[key] = value
	}
	for _, key := range baseKeys {
		if _, present := metrics[key]; !present {
			return nil, fmt.Errorf("%w: missing base metric %s", ErrMalformedVector, key)
		}
	}
	return metrics, nil
}

// truncate bounds how much attacker-controlled text an error message carries.
func truncate(text string) string {
	const limit = 32
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

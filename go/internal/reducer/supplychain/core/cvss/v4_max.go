// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

// Highest-severity vectors per equivalence class and the maximum severity
// distance per class, from FIRST's CVSS v4.0 reference calculator
// (max_composed.js and max_severity.js, BSD-2-Clause; Copyright FIRST, Red Hat,
// and contributors) and section 8 of the CVSS v4.0 specification. Class digits
// index the maps; the inner EQ3/EQ6 map is keyed by the EQ6 digit.
var (
	v4MaxComposedEQ1 = map[int][]string{
		0: {"AV:N/PR:N/UI:N/"},
		1: {"AV:A/PR:N/UI:N/", "AV:N/PR:L/UI:N/", "AV:N/PR:N/UI:P/"},
		2: {"AV:P/PR:N/UI:N/", "AV:A/PR:L/UI:P/"},
	}
	v4MaxComposedEQ2 = map[int][]string{
		0: {"AC:L/AT:N/"},
		1: {"AC:H/AT:N/", "AC:L/AT:P/"},
	}
	v4MaxComposedEQ3EQ6 = map[int]map[int][]string{
		0: {
			0: {"VC:H/VI:H/VA:H/CR:H/IR:H/AR:H/"},
			1: {"VC:H/VI:H/VA:L/CR:M/IR:M/AR:H/", "VC:H/VI:H/VA:H/CR:M/IR:M/AR:M/"},
		},
		1: {
			0: {"VC:L/VI:H/VA:H/CR:H/IR:H/AR:H/", "VC:H/VI:L/VA:H/CR:H/IR:H/AR:H/"},
			1: {
				"VC:L/VI:H/VA:L/CR:H/IR:M/AR:H/", "VC:L/VI:H/VA:H/CR:H/IR:M/AR:M/",
				"VC:H/VI:L/VA:H/CR:M/IR:H/AR:M/", "VC:H/VI:L/VA:L/CR:M/IR:H/AR:H/",
				"VC:L/VI:L/VA:H/CR:H/IR:H/AR:M/",
			},
		},
		2: {1: {"VC:L/VI:L/VA:L/CR:H/IR:H/AR:H/"}},
	}
	v4MaxComposedEQ4 = map[int][]string{
		0: {"SC:H/SI:S/SA:S/"},
		1: {"SC:H/SI:H/SA:H/"},
		2: {"SC:L/SI:L/SA:L/"},
	}
	v4MaxComposedEQ5 = map[int][]string{
		0: {"E:A/"},
		1: {"E:P/"},
		2: {"E:U/"},
	}

	v4MaxSeverityEQ1    = map[int]float64{0: 1, 1: 4, 2: 5}
	v4MaxSeverityEQ2    = map[int]float64{0: 1, 1: 2}
	v4MaxSeverityEQ3EQ6 = map[int]map[int]float64{0: {0: 7, 1: 6}, 1: {0: 8, 1: 8}, 2: {1: 10}}
	v4MaxSeverityEQ4    = map[int]float64{0: 6, 1: 5, 2: 4}
)

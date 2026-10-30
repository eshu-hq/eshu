// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

// perlCpanfileDefinition binds only the exact cpanfile name to guarded,
// metadata-only structured parsing. The snapshot sibling cpanfile.snapshot
// is not listed and stays unclaimed, and the perl definition keeps only its
// source extensions, so this definition is the single admission path for
// Perl dependency manifests (#9308).
func perlCpanfileDefinition() Definition {
	return Definition{
		ParserKey:    "perl_cpanfile",
		Language:     "perl_cpanfile",
		Ecosystem:    "perl",
		Admission:    AdmissionStructuredFact,
		ContentStore: ContentStore{Policy: ContentStoreMetadata},
		ExactNames:   []string{"cpanfile"},
	}
}

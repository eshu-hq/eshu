// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

// pythonRuntimeTXTDefinition admits Heroku's exact runtime pin name under a
// metadata-only content policy. The declaration reader sees source bytes
// before the policy clears the persisted body.
func pythonRuntimeTXTDefinition() Definition {
	return Definition{
		ParserKey:    "runtime_txt",
		Language:     "raw_text",
		Ecosystem:    "python",
		Admission:    AdmissionStructuredFact,
		ContentStore: ContentStore{Policy: ContentStoreMetadata},
		ExactNames:   []string{"runtime.txt"},
	}
}

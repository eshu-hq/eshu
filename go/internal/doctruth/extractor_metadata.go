// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth

func sectionEvidenceMetadata(section SectionInput, capacity int) map[string]string {
	metadata := make(map[string]string, capacity)
	for key, value := range section.SourceMetadata {
		if _, reserved := reservedSourceMetadataKeys[key]; reserved {
			metadata["section."+key] = value
			continue
		}
		metadata[key] = value
	}
	metadata["source_start_ref"] = section.SourceStartRef
	metadata["source_end_ref"] = section.SourceEndRef
	return metadata
}

func claimSourceMetadata(section SectionInput, hint ClaimHint) map[string]string {
	metadata := sectionEvidenceMetadata(section, len(section.SourceMetadata)+len(hint.SourceMetadata)+2)
	for key, value := range hint.SourceMetadata {
		if _, reserved := reservedSourceMetadataKeys[key]; reserved {
			metadata["hint."+key] = value
			continue
		}
		if _, exists := metadata[key]; exists {
			metadata["hint."+key] = value
			continue
		}
		metadata[key] = value
	}
	return metadata
}

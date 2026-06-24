// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

func entityLabelForBucket(label string, item Entity) string {
	if label != "Module" {
		return label
	}
	if moduleKind, _ := item.Metadata["module_kind"].(string); moduleKind == "protocol_implementation" {
		return "ProtocolImplementation"
	}
	return label
}

type indexedEntity struct {
	label string
	item  Entity
}

func (e indexedEntity) lineNumber() int {
	if e.item.LineNumber >= 1 {
		return e.item.LineNumber
	}
	return 1
}

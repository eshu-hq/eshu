// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

// ResolveContainingEntityID returns the narrowest function or type span
// that contains a parser reference line, so callback references attach to the
// executable body that owns the reference.
func ResolveContainingEntityID(
	index EntityIndex,
	rawPath string,
	relativePath string,
	line int,
) string {
	var (
		bestEntityID string
		bestWidth    int
	)
	for _, pathKey := range PathKeys(rawPath, relativePath) {
		for _, span := range index.containersByPath[pathKey] {
			if line < span.StartLine || line > span.EndLine {
				continue
			}
			width := span.EndLine - span.StartLine
			if bestEntityID == "" || width < bestWidth {
				bestEntityID = span.EntityID
				bestWidth = width
			}
		}
		if bestEntityID != "" {
			return bestEntityID
		}
	}
	return ""
}

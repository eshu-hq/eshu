// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

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
			if line < span.startLine || line > span.endLine {
				continue
			}
			width := span.endLine - span.startLine
			if bestEntityID == "" || width < bestWidth {
				bestEntityID = span.entityID
				bestWidth = width
			}
		}
		if bestEntityID != "" {
			return bestEntityID
		}
	}
	return ""
}

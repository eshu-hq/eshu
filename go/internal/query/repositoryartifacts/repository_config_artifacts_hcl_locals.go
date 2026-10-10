// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"fmt"
	"path"
	"strings"
)

func extractLocalConfigAssignments(relativePath, content string) map[string]string {
	assignments := make(map[string]string)
	lines := strings.Split(content, "\n")
	inLocals := false
	depth := 0
	currentName := ""
	currentExpression := strings.Builder{}
	currentParenDepth := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inLocals {
			if !strings.HasPrefix(trimmed, localsBlockKeyword) || !strings.Contains(trimmed, "{") {
				continue
			}
			inLocals = true
			depth = countBracesOutsideStrings(line)
			if depth <= 0 {
				inLocals = false
				depth = 0
			}
			continue
		}
		if currentName == "" {
			if assignment := localStringAssignmentPattern.FindStringSubmatch(line); len(assignment) >= 3 {
				name := strings.TrimSpace(assignment[1])
				value := strings.TrimSpace(assignment[2])
				if name != "" && value != "" {
					assignments[name] = value
				}
			} else if assignment := localAssignmentStartPattern.FindStringSubmatch(line); len(assignment) >= 3 {
				currentName = strings.TrimSpace(assignment[1])
				currentExpression.Reset()
				currentExpression.WriteString(strings.TrimSpace(assignment[2]))
				currentParenDepth = countParensOutsideStrings(assignment[2])
				if currentParenDepth <= 0 {
					storeLocalConfigAssignment(assignments, currentName, currentExpression.String(), relativePath)
					currentName = ""
				}
			}
		} else {
			if trimmed != "" {
				currentExpression.WriteString("\n")
				currentExpression.WriteString(trimmed)
			}
			currentParenDepth += countParensOutsideStrings(line)
			if currentParenDepth <= 0 {
				storeLocalConfigAssignment(assignments, currentName, currentExpression.String(), relativePath)
				currentName = ""
			}
		}
		depth += countBracesOutsideStrings(line)
		if depth <= 0 {
			if currentName != "" {
				storeLocalConfigAssignment(assignments, currentName, currentExpression.String(), relativePath)
				currentName = ""
			}
			inLocals = false
			depth = 0
		}
	}
	return assignments
}

func storeLocalConfigAssignment(assignments map[string]string, name, expression, relativePath string) {
	if name == "" {
		return
	}
	if indexedAssignments := extractPathRelativeToIncludeIndexedAssignments(name, expression, relativePath); len(indexedAssignments) > 0 {
		for key, value := range indexedAssignments {
			assignments[key] = value
		}
		return
	}
	if value := extractConfigAssetPathFromExpression(expression, assignments); value != "" {
		assignments[name] = value
	}
}

func extractPathRelativeToIncludeIndexedAssignments(name, expression, relativePath string) map[string]string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" || !pathRelativeToIncludeSplitPattern.MatchString(stripHCLInlineComments(expression)) {
		return nil
	}

	directory := CleanRepositoryRelativePath(path.Dir(strings.TrimSpace(relativePath)))
	if directory == "" || directory == "." {
		return nil
	}

	parts := strings.Split(directory, "/")
	assignments := make(map[string]string, len(parts))
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		assignments[fmt.Sprintf("%s[%d]", trimmedName, index)] = part
	}
	return assignments
}

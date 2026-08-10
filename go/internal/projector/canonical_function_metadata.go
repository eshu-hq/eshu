// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strconv"
	"strings"
)

func appendEntityClassMembers(mat *CanonicalMaterialization) {
	seen := make(map[string]struct{}, len(mat.ClassMembers)+len(mat.Entities))
	for _, member := range mat.ClassMembers {
		seen[classMemberKey(member)] = struct{}{}
	}
	classes := make(map[string]struct{}, len(mat.Entities))
	for _, entity := range mat.Entities {
		if entity.Label == "Class" {
			classes[classEntityKey(entity.EntityName, entity.FilePath)] = struct{}{}
		}
	}
	for _, entity := range mat.Entities {
		if entity.Label != "Function" {
			continue
		}
		className, _ := entity.Metadata["class_context"].(string)
		className = strings.TrimSpace(className)
		if className == "" || strings.TrimSpace(entity.EntityName) == "" {
			continue
		}
		if _, exists := classes[classEntityKey(className, entity.FilePath)]; !exists {
			continue
		}
		member := ClassMemberRow{
			ClassName:    className,
			FunctionName: entity.EntityName,
			FilePath:     entity.FilePath,
			FunctionLine: entity.StartLine,
		}
		key := classMemberKey(member)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		mat.ClassMembers = append(mat.ClassMembers, member)
	}
}

func classEntityKey(name, filePath string) string {
	return strings.TrimSpace(name) + "\x00" + filePath
}

func classMemberKey(member ClassMemberRow) string {
	return strings.Join([]string{member.ClassName, member.FunctionName, member.FilePath, strconv.Itoa(member.FunctionLine)}, "\x00")
}

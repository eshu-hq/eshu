// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget

import (
	"bytes"
	"encoding/xml"
	"io"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// projectRuntimeEvidence reads only direct, unconditional PropertyGroup values
// from the bytes the NuGet parser already read. It never evaluates MSBuild.
func projectRuntimeEvidence(source, original []byte) shared.DotNetRuntimeEvidence {
	evidence := shared.NewDotNetRuntimeEvidence(original)
	if evidence.State == "over_cap" {
		return evidence
	}
	decoder := xml.NewDecoder(bytes.NewReader(source))
	lineOffsets := make([]int64, 0)
	for i, b := range source {
		if b == '\n' {
			lineOffsets = append(lineOffsets, int64(i))
		}
	}
	var stack []string
	var groupCondition bool
	var candidate string
	var candidateLine int
	for {
		startOffset := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			evidence.State = "unavailable"
			evidence.Values = nil
			return evidence
		}
		switch value := token.(type) {
		case xml.StartElement:
			stack = append(stack, value.Name.Local)
			if len(stack) == 2 && stack[0] == "Project" && stack[1] == "PropertyGroup" {
				groupCondition = false
				for _, attr := range value.Attr {
					if attr.Name.Local == "Condition" && strings.TrimSpace(attr.Value) != "" {
						groupCondition = true
					}
				}
			}
			if len(stack) == 3 && stack[0] == "Project" && stack[1] == "PropertyGroup" && !groupCondition && (stack[2] == "TargetFramework" || stack[2] == "TargetFrameworks") {
				candidate = ""
				candidateLine = 1 + sort.Search(len(lineOffsets), func(i int) bool { return lineOffsets[i] >= startOffset })
				for _, attr := range value.Attr {
					if attr.Name.Local == "Condition" && strings.TrimSpace(attr.Value) != "" {
						candidateLine = 0
					}
				}
			}
			if len(stack) > 3 && candidateLine > 0 {
				candidateLine = 0
			}
		case xml.CharData:
			if len(stack) == 3 && candidateLine > 0 {
				candidate += string(value)
			}
		case xml.EndElement:
			if len(stack) == 3 && candidateLine > 0 {
				kind := "dotnet_csproj_target_framework"
				if stack[2] == "TargetFrameworks" {
					kind = "dotnet_csproj_target_frameworks"
				}
				parts := []string{candidate}
				if kind == "dotnet_csproj_target_frameworks" {
					parts = strings.Split(candidate, ";")
				}
				for _, part := range parts {
					evidence.Add(kind, part, candidateLine)
				}
				candidateLine = 0
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return evidence
}

// ExtractDotNetProjectEvidence parses a supplied project body for the content-only path.
func ExtractDotNetProjectEvidence(source []byte) shared.DotNetRuntimeEvidence {
	evidence := shared.NewDotNetRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	normalized := shared.NormalizeLineEndings(source)
	var project nugetProjectFile
	if err := xml.NewDecoder(bytes.NewReader(normalized)).Decode(&project); err != nil {
		evidence.State = "unavailable"
		return evidence
	}
	return projectRuntimeEvidence(normalized, source)
}

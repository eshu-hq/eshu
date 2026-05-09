package perl

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	perlPackagePattern  = regexp.MustCompile(`^\s*package\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)\s*;`)
	perlUsePattern      = regexp.MustCompile(`^\s*use\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	perlSubPattern      = regexp.MustCompile(`^\s*sub\s+([A-Za-z_]\w*)`)
	perlVariablePattern = regexp.MustCompile(`\b(?:my|our)\s+[@$%]?([A-Za-z_]\w*)`)
	perlCallPattern     = regexp.MustCompile(`([A-Za-z_:]+::[A-Za-z_]\w*|[A-Za-z_]\w*)\s*\(`)
)

// Parse reads path and returns the legacy Perl parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "perl", isDependency)
	lines := strings.Split(string(source), "\n")
	seenVariables := make(map[string]struct{})
	seenCalls := make(map[string]struct{})

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if matches := perlPackagePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "classes", map[string]any{
				"name":        shared.LastPathSegment(matches[1], "::"),
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
			})
		}
		if matches := perlUsePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"lang":        "perl",
			})
		}
		if matches := perlSubPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			item := map[string]any{
				"name":        matches[1],
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
				"decorators":  []string{},
			}
			if options.IndexSource {
				item["source"] = rawLine
			}
			shared.AppendBucket(payload, "functions", item)
		}
		for _, match := range perlVariablePattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			name := match[1]
			if _, ok := seenVariables[name]; ok {
				continue
			}
			seenVariables[name] = struct{}{}
			shared.AppendBucket(payload, "variables", map[string]any{
				"name":        name,
				"line_number": lineNumber,
				"end_line":    lineNumber,
				"lang":        "perl",
			})
		}
		for _, match := range perlCallPattern.FindAllStringSubmatch(trimmed, -1) {
			if len(match) != 2 {
				continue
			}
			appendUniqueRegexCall(payload, seenCalls, match[1], lineNumber, "perl")
		}
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

// PreScan returns Perl subroutine and package names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes")
	slices.Sort(names)
	return names, nil
}

func appendUniqueRegexCall(
	payload map[string]any,
	seen map[string]struct{},
	fullName string,
	lineNumber int,
	lang string,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}
	shared.AppendBucket(payload, "function_calls", map[string]any{
		"name":        fullName,
		"full_name":   fullName,
		"line_number": lineNumber,
		"lang":        lang,
	})
}

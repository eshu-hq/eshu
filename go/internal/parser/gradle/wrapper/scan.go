// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

import "bytes"

// Scan reads source as a Java properties file and returns the Gradle
// distribution row when distributionUrl matches the documented grammar. The
// work is linear in len(source). No byte of any property value other than the
// version leaves this function.
func Scan(source []byte) Result {
	if len(source) > MaxSourceBytes {
		return Result{State: StateOversize, ParseError: "file exceeds the wrapper size bound"}
	}
	result := Result{State: StateParsed}
	var row map[string]any
	declarations, line := 0, 1
	for pos := 0; pos < len(source); {
		start := firstNonBlank(source, pos)
		eol := lineEnd(source, pos)
		if start >= eol || source[start] == '#' || source[start] == '!' {
			pos, line = nextLine(source, eol), line+1
			continue
		}
		keyEnd := start
		for keyEnd < eol && !isKeyEnd(source[keyEnd]) {
			keyEnd++
		}
		logical := logicalLine(source, pos)
		key := source[start:keyEnd]
		switch {
		case bytes.IndexByte(key, '\\') >= 0:
			// A key written with an escape may still name distributionUrl to a
			// Java reader. Do not guess: count it.
			result.Counts.Unsupported++
		case string(key) == KeyDistributionURL:
			declarations++
			if declarations == 1 && !logical.dangling {
				row = distributionRow(source, keyEnd, logical.end, line)
			}
		}
		pos, line = logical.next, line+logical.physical
	}
	switch {
	case declarations == 1 && row != nil:
		result.Rows = []map[string]any{row}
	case declarations > 0:
		// Repeated, dangling, or outside the grammar: no row.
		result.Counts.Unsupported++
	}
	return result
}

// distributionRow builds the row from the value that follows the key, or
// returns nil when the value is outside the grammar.
func distributionRow(source []byte, keyEnd, end, line int) map[string]any {
	value, ok := logicalValue(source, keyEnd, end)
	if !ok {
		return nil
	}
	version, kind, ok := archiveVersion(value)
	if !ok {
		return nil
	}
	return map[string]any{
		"name":              "gradle",
		"value":             version,
		"line_number":       line,
		"section":           "distribution",
		"config_kind":       "gradle_distribution",
		"lang":              LanguageName,
		"distribution_type": kind,
	}
}

// archiveVersion takes the version out of the archive name at the end of the
// URL path: gradle-<version>-bin.zip or gradle-<version>-all.zip. The query
// and fragment are cut first. Nothing else about the URL is read.
func archiveVersion(value []byte) (version, kind string, ok bool) {
	if cut := bytes.IndexAny(value, "?#"); cut >= 0 {
		value = value[:cut]
	}
	name := value[bytes.LastIndexByte(value, '/')+1:]
	const prefix = "gradle-"
	if !bytes.HasPrefix(name, []byte(prefix)) {
		return "", "", false
	}
	var suffix string
	switch {
	case bytes.HasSuffix(name, []byte("-bin.zip")):
		kind, suffix = "bin", "-bin.zip"
	case bytes.HasSuffix(name, []byte("-all.zip")):
		kind, suffix = "all", "-all.zip"
	default:
		return "", "", false
	}
	// The prefix and suffix can overlap in a name such as gradle-bin.zip, which
	// has no version between them.
	if len(name) <= len(prefix)+len(suffix) {
		return "", "", false
	}
	token := name[len(prefix) : len(name)-len(suffix)]
	if !validVersion(token) {
		return "", "", false
	}
	return string(token), kind, true
}

// validVersion accepts a release number or a pre-release or snapshot suffix:
// a leading digit, then letters, digits, dot, plus, underscore and hyphen.
func validVersion(token []byte) bool {
	if len(token) == 0 || len(token) > MaxVersionBytes || token[0] < '0' || token[0] > '9' {
		return false
	}
	for _, b := range token {
		switch {
		case b >= '0' && b <= '9', b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b == '.', b == '+', b == '_', b == '-':
		default:
			return false
		}
	}
	return true
}

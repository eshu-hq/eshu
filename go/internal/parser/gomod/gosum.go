// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomod

import (
	"bufio"
	"bytes"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func parseGoSum(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	payload := basePayload(path, isDependency)

	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if options.IndexSource {
		payload["source"] = string(source)
	}

	rows, scanErr := goSumChecksumRows(source)
	sortGoSumRows(rows)
	payload["variables"] = rows

	state := map[string]any{
		"state":           "parsed",
		"checksum_count":  len(rows),
		"ambiguous_entry": "go.sum records every version any tool has verified, not the currently selected version",
	}
	if scanErr != nil {
		// A scanner error (for example, a single go.sum line that exceeds
		// the buffer) silently truncates the file unless we surface it. The
		// rows already collected stay on the payload for forensic value but
		// the state envelope flips to malformed so the readiness layer
		// treats this as missing/ambiguous evidence, never as a complete
		// parse.
		state["state"] = "malformed"
		state["parse_error"] = scanErr.Error()
	}
	payload["gomod_state"] = state
	return payload, nil
}

func goSumChecksumRows(source []byte) ([]map[string]any, error) {
	if len(source) == 0 {
		return []map[string]any{}, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	rows := make([]map[string]any, 0, 16)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		row, ok := goSumLineRow(scanner.Text(), lineNumber)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

func goSumLineRow(line string, lineNumber int) (map[string]any, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return nil, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 3 {
		return nil, false
	}
	modulePath := strings.TrimSpace(fields[0])
	versionField := strings.TrimSpace(fields[1])
	checksum := strings.TrimSpace(fields[2])
	if modulePath == "" || versionField == "" || checksum == "" {
		return nil, false
	}
	version := versionField
	checksumKind := "module"
	if strings.HasSuffix(versionField, "/go.mod") {
		version = strings.TrimSuffix(versionField, "/go.mod")
		checksumKind = "gomod"
	}
	return map[string]any{
		"name":             modulePath,
		"line_number":      lineNumber,
		"value":            version,
		"section":          "go-sum",
		"config_kind":      "dependency_checksum",
		"package_manager":  PackageManager,
		"lang":             LanguageName,
		"lockfile":         true,
		"ambiguous":        true,
		"checksum":         checksum,
		"checksum_kind":    checksumKind,
		"dependency_path":  []string{modulePath},
		"dependency_depth": 1,
	}, true
}

func sortGoSumRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		li, _ := rows[i]["line_number"].(int)
		lj, _ := rows[j]["line_number"].(int)
		if li != lj {
			return li < lj
		}
		ni, _ := rows[i]["name"].(string)
		nj, _ := rows[j]["name"].(string)
		if ni != nj {
			return ni < nj
		}
		ki, _ := rows[i]["checksum_kind"].(string)
		kj, _ := rows[j]["checksum_kind"].(string)
		return ki < kj
	})
}

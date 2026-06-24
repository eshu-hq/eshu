// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nodelockfile

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Parse decodes one Node/TypeScript lockfile (yarn classic, yarn berry, or
// pnpm) and returns the parser payload expected by the parent engine. The
// caller selects this parser by filename through the registry; Parse decides
// the lockfile flavor from the on-disk contents so misnamed files do not
// silently produce wrong evidence.
func Parse(
	path string,
	isDependency bool,
	options shared.Options,
) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	filename := strings.ToLower(filepath.Base(path))
	payload := basePayload(path, isDependency)

	flavor := detectFlavor(filename, source)
	switch flavor {
	case flavorPnpm:
		payload["variables"] = parsePnpmLockfile(source, payload)
	case flavorYarnBerry:
		payload["variables"] = parseYarnBerryLockfile(source, payload)
	case flavorYarnClassic:
		payload["variables"] = parseYarnClassicLockfile(source, payload)
	default:
		payload["variables"] = []map[string]any{}
		payload["lockfile_parse_state"] = "malformed"
	}

	if options.IndexSource {
		payload["source"] = string(source)
	}
	return payload, nil
}

func basePayload(path string, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, "node_lockfile", isDependency)
	payload["variables"] = []map[string]any{}
	return payload
}

// flavor enumerates the supported lockfile shapes.
type flavor int

const (
	flavorUnknown flavor = iota
	flavorYarnClassic
	flavorYarnBerry
	flavorPnpm
)

// detectFlavor decides which lockfile shape to apply. Filename matters for
// pnpm because pnpm uses .yaml; yarn always uses yarn.lock. Within yarn we
// distinguish classic (banner "# yarn lockfile v1" and quoted-string values)
// from berry (YAML-compatible with "__metadata:" and resolution: strings).
func detectFlavor(filename string, source []byte) flavor {
	base := strings.ToLower(strings.TrimSpace(filename))
	switch base {
	case "pnpm-lock.yaml", "pnpm-lock.yml":
		return flavorPnpm
	case "yarn.lock":
		// fall through to content sniffing below
	default:
		return flavorUnknown
	}

	text := string(source)
	if strings.Contains(text, "__metadata:") || strings.Contains(text, "resolution:") {
		return flavorYarnBerry
	}
	if strings.Contains(text, "yarn lockfile v1") {
		return flavorYarnClassic
	}
	// Default to yarn classic for yarn.lock without a banner; classic-style
	// entries use `version "x"` (quoted string), berry uses `version: x`.
	if strings.Contains(text, "version \"") {
		return flavorYarnClassic
	}
	if strings.Contains(text, "version: ") {
		return flavorYarnBerry
	}
	return flavorUnknown
}

// dependencyRow constructs a stable row shape shared by all flavors so the
// reducer sees identical envelope fields regardless of lockfile origin. The
// `package_manager` field is intentionally the canonical npm ecosystem so
// downstream SQL filters (storage/postgres owned-package-targets) and the
// consumption reducer match yarn and pnpm evidence under the same npm
// identity. The lockfile flavor (`npm`, `yarn`, `pnpm`) is preserved as a
// separate `package_manager_flavor` field for operators and readiness.
func dependencyRow(
	name string,
	version string,
	section string,
	flavorName string,
	lockfileFormat string,
	lineNumber int,
	chain []string,
) map[string]any {
	row := map[string]any{
		"name":                   strings.TrimSpace(name),
		"line_number":            lineNumber,
		"value":                  strings.TrimSpace(version),
		"section":                section,
		"config_kind":            "dependency",
		"package_manager":        "npm",
		"package_manager_flavor": flavorName,
		"lockfile":               true,
		"lockfile_format":        lockfileFormat,
		"lang":                   "node_lockfile",
	}
	if len(chain) > 0 {
		row["dependency_path"] = chain
		row["dependency_depth"] = len(chain)
		row["direct_dependency"] = len(chain) == 1
	}
	return row
}

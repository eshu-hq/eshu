// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// GoModuleName returns the module path of a go.mod file the way the Go parser
// reads it (goModulePath in go/internal/parser/go_package_module_import_path.go):
// after shared.NormalizeLineEndings, the first line of exactly two fields whose
// first is "module" names the module, and the second field is taken verbatim.
// The normalization is the parser's own, which leaves a file that has any '\n'
// untouched, so a stray '\r' beside LF is read the same way here. A module line
// with a trailing comment, or a quoted path, therefore yields what the parser
// yields for it, and the anchor cannot disagree with the import paths the parser
// stamped on definitions.
func GoModuleName(content string) string {
	for _, line := range strings.Split(string(shared.NormalizeLineEndings([]byte(content))), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

// PackageManifestName returns the "name" of a package.json manifest. Invalid
// JSON, a non-object document, or a missing, blank, or non-string name yields ""
// so one bad manifest never fails the load.
func PackageManifestName(content string) string {
	var manifest struct {
		Name any `json:"name"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return ""
	}
	name, _ := manifest.Name.(string)
	return strings.TrimSpace(name)
}

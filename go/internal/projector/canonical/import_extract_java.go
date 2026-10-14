// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"strings"
)

// javaSourcePackage places one file in its Java package for package-level
// import cycles (#7349). Only Java files are placed: every other language
// keeps an empty package; each lift owns its own mapping (Go: #7347,
// TS/JS: #7348).
//
// The file-level package_name the Java parser writes wins: it is the
// package_declaration of exactly this file. Java function items carry no
// package, so there is no bucket fallback: anything else -- no key, a
// non-string value, a file in the default package -- is unknown, never a
// guess: an invented package would collapse the wrong files together.
func javaSourcePackage(file parsedFileRef) string {
	if strings.ToLower(strings.TrimSpace(file.Language)) != "java" {
		return ""
	}
	if file.ParsedFileData == nil {
		return ""
	}
	pkg, _ := file.ParsedFileData["package_name"].(string)
	return strings.TrimSpace(pkg)
}

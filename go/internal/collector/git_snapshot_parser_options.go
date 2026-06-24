// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func snapshotParserOptions(
	filePath string,
	goPackageTargets parser.GoPackageSemanticRoots,
	emitDataflow bool,
	repositoryID string,
) parser.Options {
	goOptions := goPackageTargets[filepath.Dir(filePath)]
	return parser.Options{
		IndexSource:                     true,
		VariableScope:                   snapshotParserVariableScope(filePath),
		GoImportedInterfaceParamMethods: goOptions.ImportedInterfaceParamMethods,
		GoDirectMethodCallRoots:         goOptions.DirectMethodCallRoots,
		GoPackageImportPath:             goOptions.ImportPath,
		RepositoryID:                    repositoryID,
		EmitDataflow:                    emitDataflow,
	}
}

func snapshotParserVariableScope(filePath string) string {
	switch filepath.Ext(filePath) {
	case ".java":
		return "module"
	default:
		return "all"
	}
}

// LoadEmitDataflowGate reports whether the value-flow emission gate is enabled,
// reading the ESHU_EMIT_DATAFLOW environment contract. It is off by default so
// the snapshot payload stays byte-identical to before the value-flow feature;
// only an explicit affirmative value enables it.
func LoadEmitDataflowGate(getenv func(string) string) bool {
	return emitDataflowFromEnv(getenv("ESHU_EMIT_DATAFLOW"))
}

// emitDataflowFromEnv parses the affirmative-only ESHU_EMIT_DATAFLOW contract.
func emitDataflowFromEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

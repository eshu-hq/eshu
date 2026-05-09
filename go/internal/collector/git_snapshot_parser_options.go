package collector

import (
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func snapshotParserOptions(
	filePath string,
	goPackageTargets parser.GoPackageImportedInterfaceParamMethods,
) parser.Options {
	return parser.Options{
		IndexSource:                     true,
		VariableScope:                   snapshotParserVariableScope(filePath),
		GoImportedInterfaceParamMethods: goPackageTargets[filepath.Dir(filePath)],
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

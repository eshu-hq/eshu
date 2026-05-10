package collector

import (
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func snapshotParserOptions(
	filePath string,
	goPackageTargets parser.GoPackageSemanticRoots,
) parser.Options {
	goOptions := goPackageTargets[filepath.Dir(filePath)]
	return parser.Options{
		IndexSource:                     true,
		VariableScope:                   snapshotParserVariableScope(filePath),
		GoImportedInterfaceParamMethods: goOptions.ImportedInterfaceParamMethods,
		GoDirectMethodCallRoots:         goOptions.DirectMethodCallRoots,
		GoPackageImportPath:             goOptions.ImportPath,
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

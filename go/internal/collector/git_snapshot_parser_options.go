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
		VariableScope:                   "all",
		GoImportedInterfaceParamMethods: goPackageTargets[filepath.Dir(filePath)],
	}
}

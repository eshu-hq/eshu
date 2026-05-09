package parser

// GoImportedInterfaceParamMethods maps lower-case function names to the
// imported interface methods required by each parameter index.
type GoImportedInterfaceParamMethods map[string]map[int][]string

// GoPackageImportedInterfaceParamMethods maps absolute Go package directories
// to same-package imported interface parameter contracts.
type GoPackageImportedInterfaceParamMethods map[string]GoImportedInterfaceParamMethods

// Options configures one parser execution.
type Options struct {
	IndexSource                     bool
	VariableScope                   string
	GoImportedInterfaceParamMethods GoImportedInterfaceParamMethods
}

package parser

import "strings"

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

func (o Options) normalizedVariableScope() string {
	scope := strings.TrimSpace(strings.ToLower(o.VariableScope))
	if scope == "" {
		return "module"
	}
	if scope == "all" {
		return "all"
	}
	return "module"
}

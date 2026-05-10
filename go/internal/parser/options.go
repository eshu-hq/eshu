package parser

// GoImportedInterfaceParamMethods maps lower-case function names to the
// imported interface methods required by each parameter index.
type GoImportedInterfaceParamMethods map[string]map[int][]string

// GoDirectMethodCallRoots maps qualified lower-case Go method declarations to
// root kinds observed from imported-package selector calls.
type GoDirectMethodCallRoots map[string][]string

// GoPackageSemanticRoots maps absolute Go package directories to package-level
// semantic contracts discovered before per-file parsing.
type GoPackageSemanticRoots map[string]GoPackageSemanticRootOptions

// GoPackageSemanticRootOptions carries Go reachability facts that require a
// repository or package pre-scan before parsing individual files.
type GoPackageSemanticRootOptions struct {
	ImportedInterfaceParamMethods GoImportedInterfaceParamMethods
	DirectMethodCallRoots         GoDirectMethodCallRoots
	ImportPath                    string
}

// Options configures one parser execution.
type Options struct {
	IndexSource                     bool
	VariableScope                   string
	GoImportedInterfaceParamMethods GoImportedInterfaceParamMethods
	GoDirectMethodCallRoots         GoDirectMethodCallRoots
	GoPackageImportPath             string
}

package golang

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// Options configures one Go parser execution.
type Options = shared.Options

// GoImportedInterfaceParamMethods maps lower-case Go function names to the
// imported interface methods required by each parameter index.
type GoImportedInterfaceParamMethods = shared.GoImportedInterfaceParamMethods

// GoDirectMethodCallRoots maps qualified lower-case Go method declarations to
// root kinds observed from imported-package selector calls.
type GoDirectMethodCallRoots = shared.GoDirectMethodCallRoots

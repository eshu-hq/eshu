// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"fmt"
	"strings"
)

func goSCIPSymbol(packageImportPath string, classContext string, name string) string {
	packageImportPath = strings.TrimSpace(packageImportPath)
	classContext = strings.TrimSpace(classContext)
	name = strings.TrimSpace(name)
	if packageImportPath == "" || name == "" {
		return ""
	}
	if classContext != "" {
		return fmt.Sprintf("scip-go gomod %s %s#%s().", packageImportPath, classContext, name)
	}
	return fmt.Sprintf("scip-go gomod %s %s().", packageImportPath, name)
}

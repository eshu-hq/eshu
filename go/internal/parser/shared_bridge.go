// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

func sharedOptions(options Options) shared.Options {
	return shared.Options{
		IndexSource:                     options.IndexSource,
		VariableScope:                   options.VariableScope,
		GoImportedInterfaceParamMethods: shared.GoImportedInterfaceParamMethods(options.GoImportedInterfaceParamMethods),
		GoDirectMethodCallRoots:         shared.GoDirectMethodCallRoots(options.GoDirectMethodCallRoots),
		GoPackageImportPath:             options.GoPackageImportPath,
		RepositoryID:                    options.RepositoryID,
		EmitDataflow:                    options.EmitDataflow,
	}
}

func basePayload(path string, lang string, isDependency bool) map[string]any {
	return shared.BasePayload(path, lang, isDependency)
}

func appendBucket(payload map[string]any, key string, item map[string]any) {
	shared.AppendBucket(payload, key, item)
}

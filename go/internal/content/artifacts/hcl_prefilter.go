// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content/artifacts/prefilter"
)

// localsBlockKeyword is the line prefix extractLocalConfigAssignments looks
// for to open a locals block. Both the walk and the prefilter read it, so the
// gate cannot drift from the code it guards.
const localsBlockKeyword = "locals"

// Positions of each scan in hclScanPrefilters, in constructor order.
const (
	hclScanDependencyConfigPath = iota
	hclScanReadConfig
	hclScanIncludePath
	hclScanFindInParentFolders
	hclScanConfigFunction
	hclScanModuleSource
)

// hclScanPrefilters derives, from the extractor's own regular expressions,
// what text a file must hold before a scan can match (#7126). Each scan costs
// a pass over the whole file even when it finds nothing, and most hydrated
// HCL files (provider lock files, plain resource files) hold none of the
// required literals. The derivation is sound for every input, so skipping a
// scan changes no row; see the prefilter package for the contract.
var hclScanPrefilters = prefilter.NewSet(
	terragruntDependencyConfigPathPattern,
	terragruntReadConfigPattern,
	terragruntIncludePathPattern,
	terragruntFindInParentFoldersPattern,
	localConfigFunctionStartPattern,
	localTerraformModuleSourcePattern,
)

// hclScanPlan names the scans of extractHCLConfigAssetRows that may produce
// a row for one file. A false field is a proof that the scan finds nothing.
type hclScanPlan struct {
	dependencyConfigPath bool
	readConfig           bool
	includePath          bool
	findInParentFolders  bool
	configFunction       bool
	moduleSource         bool
	// locals reports whether the content holds the locals keyword at all;
	// extractLocalConfigAssignments finds no block without it.
	locals bool
}

// planHCLScans decides which scans a file's content can satisfy.
func planHCLScans(prefilters *prefilter.Set, content string) hclScanPlan {
	may := prefilters.Evaluate(content)
	return hclScanPlan{
		dependencyConfigPath: may[hclScanDependencyConfigPath],
		readConfig:           may[hclScanReadConfig],
		includePath:          may[hclScanIncludePath],
		findInParentFolders:  may[hclScanFindInParentFolders],
		configFunction:       may[hclScanConfigFunction],
		moduleSource:         may[hclScanModuleSource],
		locals:               strings.Contains(content, localsBlockKeyword),
	}
}

// needsLocalAssignments reports whether any scan that reads the locals map
// can run. Only the dependency config path, the config function and the
// module source scans resolve expressions against it; the terragrunt
// parent-folder scans normalize with a nil map.
func (p hclScanPlan) needsLocalAssignments() bool {
	return p.locals && (p.dependencyConfigPath || p.configFunction || p.moduleSource)
}

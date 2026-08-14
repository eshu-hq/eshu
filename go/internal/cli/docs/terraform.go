// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

const (
	// terraformTruthMaxFiles bounds how many .tf files one scan parses.
	terraformTruthMaxFiles = 2000
	// terraformTruthMaxFileBytes bounds how much of one .tf file is read.
	terraformTruthMaxFileBytes = 512 * 1024
)

// errTerraformTruthLimitReached stops the Terraform walk at
// terraformTruthMaxFiles, marking the scan incomplete rather than failing.
var errTerraformTruthLimitReached = errors.New("terraform truth file limit reached")

// TerraformAddressResolver builds the resolver that checks a documented
// Terraform address against the workspace's own .tf files. It returns nil when
// no workspace root resolves.
//
// The scan is lazy and runs at most once per resolver. When it is incomplete --
// the file limit was hit, a file was oversized, or HCL failed to parse -- an
// unmatched address reports unsupported rather than contradicted, so invalid
// HCL never turns a correctly documented address into a contradiction.
func TerraformAddressResolver(verifyPath string) doctruth.TerraformAddressResolver {
	root, ok := TruthRoot(verifyPath)
	if !ok {
		return nil
	}
	var once sync.Once
	var addresses map[string]struct{}
	var complete bool
	return func(_ doctruth.DocumentInput, address string) doctruth.TerraformAddressResolution {
		normalized := doctruth.NormalizeTerraformAddressClaim(address)
		if normalized == "" {
			return doctruth.TerraformAddressResolution{}
		}
		once.Do(func() {
			addresses, complete = terraformAddressTruth(root)
		})
		if _, ok := addresses[normalized]; ok {
			return doctruth.TerraformAddressResolution{Supported: true, Exists: true}
		}
		if !complete {
			return doctruth.TerraformAddressResolution{}
		}
		return doctruth.TerraformAddressResolution{Supported: true, Exists: false}
	}
}

// terraformAddressTruth walks root parsing every .tf and .tf.json file into the
// set of resource, data, and module addresses they declare. The second return
// reports whether the scan was complete.
func terraformAddressTruth(root string) (map[string]struct{}, bool) {
	addresses := map[string]struct{}{}
	files := 0
	complete := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			complete = false
			return nil
		}
		if entry.IsDir() {
			if shouldSkipTerraformTruthDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTerraformTruthFile(path) {
			return nil
		}
		files++
		if files > terraformTruthMaxFiles {
			return errTerraformTruthLimitReached
		}
		fileAddresses, ok := terraformAddressesFromFile(path)
		if !ok {
			complete = false
		}
		for _, address := range fileAddresses {
			addresses[address] = struct{}{}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errTerraformTruthLimitReached) {
		complete = false
	}
	if errors.Is(err, errTerraformTruthLimitReached) {
		complete = false
	}
	return addresses, complete
}

// shouldSkipTerraformTruthDir reports the directories the Terraform scan does
// not descend into. .terraform is excluded on top of the manifest scan's list:
// it holds downloaded modules whose addresses are not this workspace's truth.
func shouldSkipTerraformTruthDir(name string) bool {
	switch name {
	case ".git", ".terraform", ".worktrees", "node_modules", "vendor", "dist", "build", "site":
		return true
	default:
		return false
	}
}

// isTerraformTruthFile reports whether path is a Terraform configuration file
// in either the native or JSON syntax.
func isTerraformTruthFile(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(lower, ".tf") || strings.HasSuffix(lower, ".tf.json")
}

// terraformAddressesFromFile parses one Terraform file into the addresses it
// declares: `<type>.<name>` for a resource, `data.<type>.<name>` for a data
// source, and `module.<name>` for a module. The second return is false when the
// file could not be read, exceeds terraformTruthMaxFileBytes, or has HCL
// errors -- a partially parsed file is not treated as complete truth.
func terraformAddressesFromFile(path string) ([]string, bool) {
	file, err := os.Open(path) // #nosec G304 -- path is a local Terraform file discovered by the program from the scan target directory, not an HTTP request param
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, terraformTruthMaxFileBytes+1))
	if err != nil || len(content) > terraformTruthMaxFileBytes {
		return nil, false
	}

	parser := hclparse.NewParser()
	var parsed *hcl.File
	var diags hcl.Diagnostics
	if strings.HasSuffix(strings.ToLower(path), ".tf.json") {
		parsed, diags = parser.ParseJSON(content, path)
	} else {
		parsed, diags = parser.ParseHCL(content, path)
	}
	if diags.HasErrors() || parsed == nil {
		return nil, false
	}
	schema := &hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{
		{Type: "resource", LabelNames: []string{"type", "name"}},
		{Type: "data", LabelNames: []string{"type", "name"}},
		{Type: "module", LabelNames: []string{"name"}},
	}}
	body, _, diags := parsed.Body.PartialContent(schema)
	if diags.HasErrors() || body == nil {
		return nil, false
	}
	addresses := make([]string, 0, len(body.Blocks))
	for _, block := range body.Blocks {
		switch block.Type {
		case "resource":
			if len(block.Labels) == 2 {
				addresses = append(addresses, block.Labels[0]+"."+block.Labels[1])
			}
		case "data":
			if len(block.Labels) == 2 {
				addresses = append(addresses, "data."+block.Labels[0]+"."+block.Labels[1])
			}
		case "module":
			if len(block.Labels) == 1 {
				addresses = append(addresses, "module."+block.Labels[0])
			}
		}
	}
	return addresses, true
}

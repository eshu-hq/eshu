// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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
	docsVerifyTerraformTruthMaxFiles     = 2000
	docsVerifyTerraformTruthMaxFileBytes = 512 * 1024
)

var errDocsVerifyTerraformTruthLimitReached = errors.New("terraform truth file limit reached")

func docsVerifyTerraformAddressResolver(verifyPath string) doctruth.TerraformAddressResolver {
	root, ok := docsVerifyTruthRoot(verifyPath)
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
			addresses, complete = docsVerifyTerraformAddressTruth(root)
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

func docsVerifyTerraformAddressTruth(root string) (map[string]struct{}, bool) {
	addresses := map[string]struct{}{}
	files := 0
	complete := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			complete = false
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDocsVerifyTerraformTruthDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDocsVerifyTerraformTruthFile(path) {
			return nil
		}
		files++
		if files > docsVerifyTerraformTruthMaxFiles {
			return errDocsVerifyTerraformTruthLimitReached
		}
		fileAddresses, ok := docsVerifyTerraformAddressesFromFile(path)
		if !ok {
			complete = false
		}
		for _, address := range fileAddresses {
			addresses[address] = struct{}{}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDocsVerifyTerraformTruthLimitReached) {
		complete = false
	}
	if errors.Is(err, errDocsVerifyTerraformTruthLimitReached) {
		complete = false
	}
	return addresses, complete
}

func shouldSkipDocsVerifyTerraformTruthDir(name string) bool {
	switch name {
	case ".git", ".terraform", ".worktrees", "node_modules", "vendor", "dist", "build", "site":
		return true
	default:
		return false
	}
}

func isDocsVerifyTerraformTruthFile(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(lower, ".tf") || strings.HasSuffix(lower, ".tf.json")
}

func docsVerifyTerraformAddressesFromFile(path string) ([]string, bool) {
	file, err := os.Open(path) // #nosec G304 -- path is a local Terraform file discovered by the program from the scan target directory, not an HTTP request param
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, docsVerifyTerraformTruthMaxFileBytes+1))
	if err != nil || len(content) > docsVerifyTerraformTruthMaxFileBytes {
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

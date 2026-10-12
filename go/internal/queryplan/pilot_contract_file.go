// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// loadPilotContractFile keeps references portable and reads through directory
// handles so neither a contract symlink nor a contracts-directory symlink can
// escape its permitted root. Contract version 1 rejects unrecognized fields.
func loadPilotContractFile(manifestPath, ref string) (result *PilotContract, err error) {
	if path.Clean(ref) != ref || path.Ext(ref) != ".yaml" ||
		!strings.HasPrefix(ref, "contracts/") || strings.Contains(ref, "\\") {
		return nil, fmt.Errorf("contract_file must be a clean relative YAML path under contracts/")
	}
	name, err := filepath.Localize(strings.TrimPrefix(ref, "contracts/"))
	if err != nil {
		return nil, fmt.Errorf("invalid contract_file: %w", err)
	}
	manifestRoot, err := os.OpenRoot(filepath.Dir(manifestPath))
	if err != nil {
		return nil, fmt.Errorf("open manifest directory: %w", err)
	}
	defer func() { err = errors.Join(err, manifestRoot.Close()) }()
	contractsRoot, err := manifestRoot.OpenRoot("contracts")
	if err != nil {
		return nil, fmt.Errorf("open contracts directory: %w", err)
	}
	defer func() { err = errors.Join(err, contractsRoot.Close()) }()
	data, err := contractsRoot.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read contract_file: %w", err)
	}
	var contract PilotContract
	if err := decodeStrictYAML(data, &contract); err != nil {
		return nil, fmt.Errorf("parse contract_file: %w", err)
	}
	return &contract, nil
}

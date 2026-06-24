// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

func resetTerraformSchemaRegistryForTest() {
	terraformSchemaRegistryMu.Lock()
	defer terraformSchemaRegistryMu.Unlock()

	terraformResourceExtractors = map[string][]terraformResourceExtractor{}
	terraformSchemaBootstrap = false
}

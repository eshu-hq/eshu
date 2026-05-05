package relationships

func resetTerraformSchemaRegistryForTest() {
	terraformSchemaRegistryMu.Lock()
	defer terraformSchemaRegistryMu.Unlock()

	terraformResourceExtractors = map[string][]terraformResourceExtractor{}
	terraformSchemaBootstrap = false
}

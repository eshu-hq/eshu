package collector

func hasValueFlowMetadata(collected CollectedGeneration) bool {
	return len(collected.ValueFlowSummaries) > 0 || len(collected.ValueFlowSources) > 0
}

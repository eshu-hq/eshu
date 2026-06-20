package runtime

import (
	"strings"
	"testing"
)

func TestDockerComposeDocsDescribeSemanticProviderModes(t *testing.T) {
	t.Parallel()

	content := readRepositoryFile(t, "../../..", "docs/public/run-locally/docker-compose.md")
	for _, want := range []string{
		"Semantic Provider Modes",
		"`ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`",
		"`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`",
		"`ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID`",
		"no-provider",
		"local hash",
		"${ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER:-hash}",
		"`/api/v0/search/semantic`",
		"`retrieval_state`",
		"`semantic_active`",
		"`index_unready`",
		"Ollama",
		"`local_dev_profile`",
		"secret-backed",
		"`search_documents`",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Docker Compose docs missing %q", want)
		}
	}
}

package query

import (
	"context"
	"testing"
)

func TestBuildContentRelationshipSetDockerComposePromotesBuildContextImageAndDependsOn(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "compose-1",
		RepoID:       "repo-runtime",
		RelativePath: "docker-compose.yaml",
		EntityType:   "File",
		Language:     "yaml",
		SourceCache: `services:
  api:
    build:
      context: ../api
    image: ghcr.io/acme/api:1.2.3
    depends_on:
      - database
  database:
    image: postgres:16
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}
	if len(relationships.outgoing) != 4 {
		t.Fatalf("len(relationships.outgoing) = %d, want 4", len(relationships.outgoing))
	}

	checks := map[string]map[string]string{
		"DEPLOYS_FROM|../api|docker_compose_build_context": {
			"type":   "DEPLOYS_FROM",
			"target": "../api",
			"reason": "docker_compose_build_context",
		},
		"DEPLOYS_FROM|ghcr.io/acme/api:1.2.3|docker_compose_image": {
			"type":   "DEPLOYS_FROM",
			"target": "ghcr.io/acme/api:1.2.3",
			"reason": "docker_compose_image",
		},
		"DEPLOYS_FROM|postgres:16|docker_compose_image": {
			"type":   "DEPLOYS_FROM",
			"target": "postgres:16",
			"reason": "docker_compose_image",
		},
		"DEPENDS_ON|database|docker_compose_depends_on": {
			"type":   "DEPENDS_ON",
			"target": "database",
			"reason": "docker_compose_depends_on",
		},
	}

	for _, relationship := range relationships.outgoing {
		key := relationship["type"].(string) + "|" + relationship["target_name"].(string) + "|" + relationship["reason"].(string)
		expected, ok := checks[key]
		if !ok {
			t.Fatalf("unexpected relationship: %#v", relationship)
		}
		if got, want := relationship["type"], expected["type"]; got != want {
			t.Fatalf("relationship[type] = %#v, want %#v", got, want)
		}
		if got, want := relationship["target_name"], expected["target"]; got != want {
			t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
		}
		if got, want := relationship["reason"], expected["reason"]; got != want {
			t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
		}
		delete(checks, key)
	}

	if len(checks) != 0 {
		t.Fatalf("missing relationships: %#v", checks)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package targeted

import "fmt"

// Source repositories for the file-row routes. Each file row reaches an owed
// repository through one parsed_file_data bucket, and each has its own repository
// so a route that stops producing its edge shows up by name.
const (
	parityRepoRouteTerraform  = "repository:r_a0000001"
	parityRepoRouteTerragrunt = "repository:r_a0000002"
	parityRepoRouteHelmChart  = "repository:r_a0000003"
	parityRepoRouteHelmValues = "repository:r_a0000004"
	parityRepoRouteKustomize  = "repository:r_a0000005"
	parityRepoRouteDockerfile = "repository:r_a0000006"
	parityRepoRouteJenkins    = "repository:r_a0000007"
	parityRepoRouteArgoCDApp  = "repository:r_a0000008"
	parityRepoRouteFluxAlias  = "repository:r_a0000009"
	parityRepoRouteRawKind    = "repository:r_a000000a"
	parityRepoMarkerSource    = "repository:r_a000000b"
	parityRepoFileConfig      = "repository:r_a000000c"
	parityRepoEncodedSource   = "repository:r_a000000d"
	parityRepoEncodedDash     = "repository:r_a000000e"
	parityRepoEncodedAccent   = "repository:r_a000000f"
)

func parityFile(factID, repoID, path string, parsed map[string]any) parityFact {
	return parityFact{
		factID: factID, scopeID: "git:" + repoID, kind: "file",
		payload: map[string]any{"repo_id": repoID, "relative_path": path, "parsed_file_data": parsed},
	}
}

// parityFileRouteRows holds one file row per structured route, with the empty
// ArgoCD keys every parsed file carries. The key-name markers used to select all
// of them, which hid any route the alias anchors missed. Now the alias in each
// value is what selects the row.
func parityFileRouteRows() []parityFact {
	const payments = "https://github.com/acme/payments-api"
	withEmptyArgoCDKeys := func(bucket string, value any) map[string]any {
		return map[string]any{bucket: value, "argocd_applications": []any{}, "argocd_applicationsets": nil}
	}
	row := func(values map[string]any) []any { return []any{values} }
	return []parityFact{
		parityFile("route-terraform", parityRepoRouteTerraform, "modules.tf", withEmptyArgoCDKeys("terraform_modules",
			row(map[string]any{"name": "svc", "source": "git::" + payments + ".git//modules/svc"}))),
		parityFile("route-terragrunt", parityRepoRouteTerragrunt, "terragrunt.hcl", withEmptyArgoCDKeys("terragrunt_dependencies",
			row(map[string]any{"name": "svc", "config_path": "../payments-api"}))),
		parityFile("route-helm-chart", parityRepoRouteHelmChart, "Chart.yaml", withEmptyArgoCDKeys("helm_charts",
			row(map[string]any{"name": "svc", "dependencies": "payments-api"}))),
		parityFile("route-helm-values", parityRepoRouteHelmValues, "values.yaml", withEmptyArgoCDKeys("helm_values",
			row(map[string]any{"name": "svc", "image_repositories": "ghcr.io/acme/payments-api"}))),
		parityFile("route-kustomize", parityRepoRouteKustomize, "kustomization.yaml", withEmptyArgoCDKeys("kustomize_overlays",
			row(map[string]any{"resource_refs": []any{"github.com/acme/payments-api//deploy"}}))),
		parityFile("route-dockerfile", parityRepoRouteDockerfile, "Dockerfile", withEmptyArgoCDKeys("dockerfile_labels",
			row(map[string]any{"name": "org.opencontainers.image.source", "value": payments}))),
		parityFile("route-jenkins", parityRepoRouteJenkins, "Jenkinsfile", withEmptyArgoCDKeys("shared_libraries",
			[]any{"payments-api@main"})),
		parityFile("route-flux-alias", parityRepoRouteFluxAlias, "clusters/prod/payments.yaml", withEmptyArgoCDKeys("flux_git_repositories",
			row(map[string]any{"name": "svc", "url": payments + ".git"}))),
		// Percent-encoded spec.url values. NormalizeRemoteURL decodes them, so each
		// resolves to its target, but the row holds neither the remote's token nor
		// any alias. No URL anchor can reach these rows; only their content can.
		parityFile("flux-encoded-dash", parityRepoEncodedSource, "clusters/prod/dash.yaml", withEmptyArgoCDKeys("flux_git_repositories",
			row(map[string]any{"name": "dash", "url": "https://github.com/acme/my%2Dapp"}))),
		parityFile("flux-encoded-accent", parityRepoEncodedSource, "clusters/prod/accent.yaml", withEmptyArgoCDKeys("flux_git_repositories",
			row(map[string]any{"name": "accent", "url": "https://github.com/acme/caf%C3%A9"}))),
		// Non-empty ArgoCD bucket with the alias in the value.
		parityFile("route-argocd-app", parityRepoRouteArgoCDApp, "apps/payments.yaml", map[string]any{
			"argocd_applications": row(map[string]any{"name": "svc", "source_repo": payments}),
		}),
		// Raw kind route, with no artifact_type, so isArgoCDArtifact takes it from
		// the content alone.
		parityContent("route-raw-kind", parityRepoRouteRawKind, "apps/raw.yaml", "",
			"kind: Application\nspec:\n  source:\n    repoURL: "+payments+"\n"),
	}
}

// parityMarkerRows reach the load through their ArgoCD content alone: none of
// them holds a token of an owed repository. The ApplicationSet's generator reads
// file-config, so config-file-only arriving in the load proves the second phase
// still starts from a file row.
func parityMarkerRows() []parityFact {
	return []parityFact{
		parityFile("marker-application-file", parityRepoMarkerSource, "apps/one.yaml", map[string]any{
			"argocd_applications":    []any{map[string]any{"name": "one", "source_repo": "https://github.com/acme/gitops-config"}},
			"argocd_applicationsets": []any{},
		}),
		parityFile("marker-applicationset-file", parityRepoMarkerSource, "apps/two.yaml", map[string]any{
			"argocd_applications": nil,
			"argocd_applicationsets": []any{map[string]any{
				"name":                   "two",
				"generator_source_repos": "https://github.com/acme/file-config",
				"generator_source_paths": "apps/*/config.yaml",
			}},
		}),
		parityContent("config-file-only", parityRepoFileConfig, "apps/two/config.yaml", "",
			"team: two\nservice: api\n"),
		// Names nothing in the catalog, so on a full-catalog pass only a marker can
		// select it.
		parityContent("marker-unresolved", parityRepoMarkerSource, "apps/three.yaml", "argocd",
			"kind: Application\nspec:\n  source:\n    repoURL: https://example.com/elsewhere/absent\n"),
	}
}

// parityNegativeRows must never load. Each carries an ArgoCD key or its text
// without a value a reader can use, and none names a repository.
func parityNegativeRows() []parityFact {
	var rows []parityFact
	for index, value := range []any{nil, []any{}, map[string]any{}, "", "   "} {
		rows = append(rows, parityFile(
			fmt.Sprintf("negative-empty-%d", index), parityRepoQuiet, fmt.Sprintf("neg/empty-%d.yaml", index),
			map[string]any{"argocd_applications": value, "argocd_applicationsets": value, "flux_git_repositories": value},
		))
	}
	return append(rows,
		parityContent("negative-prose", parityRepoQuiet, "neg/notes.md", "",
			"the argocd_applications, argocd_applicationsets and flux_git_repositories buckets are empty here"),
		parityContent("negative-quoted-json", parityRepoQuiet, "neg/sample.json", "",
			`{"argocd_applications": [{"name": "sample"}], "argocd_applicationsets": [{"name": "sample"}]}`),
		parityFile("negative-source-text", parityRepoQuiet, "neg/source.yaml", map[string]any{
			"source":              `"argocd_applications": [{"name": "sample"}]`,
			"argocd_applications": nil,
		}),
		parityFile("negative-path-argocd_applications", parityRepoQuiet, "neg/argocd_applications.yaml",
			map[string]any{"argocd_applicationsets": []any{}}),
		// The deferred family predicate admits this path, so only the anchors can
		// keep it out of that load.
		parityFile("negative-admitted-path", parityRepoQuiet, "neg/main.tf",
			map[string]any{"argocd_applications": nil, "argocd_applicationsets": nil}),
		// The owner of a slug in the catalog, where most file rows hold it: the clone
		// path. These rows name no repository, so an anchor on the owner is what
		// would load them. The path is one the deferred family predicate admits.
		parityFile("negative-owner-path", parityRepoQuiet, "neg/owner.tf", map[string]any{
			"path":      "/srv/clones/acme-corporation/quiet/neg/owner.tf",
			"repo_path": "/srv/clones/acme-corporation/quiet",
		}),
		// The last word of an alias with no slash. Only the longest-token rule keeps
		// a common trailing word like this one out of the terms.
		parityContent("negative-last-word", parityRepoQuiet, "neg/last-word.tf", "terraform",
			`layer = "core"`),
	)
}

// parityWhitespaceRows are payloads as a client might send them. Postgres
// normalizes jsonb on input, so the spacing here must not reach the predicate.
// They exist only as raw rows, which is why only the live proof sees them.
func parityWhitespaceRows() []ParityRow {
	row := func(factID, parsed string) ParityRow {
		scopeID := "git:" + parityRepoQuiet
		return ParityRow{
			FactID: factID, StableFactKey: "stable-" + factID, ScopeID: scopeID,
			GenerationID: parityGeneration(scopeID), Kind: "file", SchemaVersion: "1.0.0",
			Payload: []byte(`{"repo_id": "` + parityRepoQuiet + `", "relative_path": "neg/` + factID +
				`.yaml", "parsed_file_data": ` + parsed + `}`),
		}
	}
	return []ParityRow{
		row("negative-spaced-empty", `{ "argocd_applications"  :   [   ] , "argocd_applicationsets" :  {  } }`),
		row("marker-spaced-full", `{ "argocd_applications"  :   [  { "name" : "spaced" }  ] }`),
	}
}

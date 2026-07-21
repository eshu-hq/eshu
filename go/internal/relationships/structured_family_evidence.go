// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// discoverStructuredHelmEvidence and discoverStructuredArgoCDEvidence below
// read the parsed_file_data helm_charts, helm_values, argocd_applications,
// and argocd_applicationsets inner keys through the typed
// factschema.DecodeParsedFileData* accessors (issue #5445 slice 1) rather
// than a raw map lookup. Each accessor skips a malformed row rather than
// failing the whole bucket, so a decode error here is always nil in
// practice; the error return is ignored deliberately, matching the
// pre-typing raw-map read's silent tolerance of an absent/wrong-shape
// bucket.

func discoverStructuredHelmEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	charts, _ := factschema.DecodeParsedFileDataHelmCharts(parsedFileData)
	for _, chart := range charts {
		chartName := strings.TrimSpace(chart.Name)
		for _, candidate := range csvValues(chart.Dependencies) {
			details := withFirstPartyRefDetails(
				map[string]any{"helm_chart_name": chartName},
				"helm_dependency_name", chartName, "", "", "", candidate,
			)
			evidence = append(evidence, matchCatalog(
				sourceRepoID, candidate, filePath,
				EvidenceKindHelmChart, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindHelmChart),
				"Helm chart metadata references the target repository",
				"helm", matcher, seen, details,
			)...)
		}
		for _, candidate := range csvValues(chart.DependencyRepositories) {
			normalized := normalizeHelmRefValue(candidate)
			details := withFirstPartyRefDetails(
				map[string]any{
					"helm_chart_name":       chartName,
					"dependency_repository": candidate,
				},
				"helm_dependency_repository", chartName, "", "", "", normalized,
			)
			evidence = append(evidence, matchCatalog(
				sourceRepoID, normalized, filePath,
				EvidenceKindHelmChart, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindHelmChart),
				"Helm chart metadata references the target repository",
				"helm", matcher, seen, details,
			)...)
		}
	}

	valuesRows, _ := factschema.DecodeParsedFileDataHelmValues(parsedFileData)
	for _, valuesRow := range valuesRows {
		valuesName := strings.TrimSpace(valuesRow.Name)
		for _, candidate := range csvValues(valuesRow.ImageRepositories) {
			normalized := normalizeHelmRefValue(candidate)
			details := withFirstPartyRefDetails(
				map[string]any{
					"helm_values_name": valuesName,
					"image_repository": candidate,
				},
				"helm_image_repository", valuesName, "", "", "", normalized,
			)
			evidence = append(evidence, matchCatalog(
				sourceRepoID, normalized, filePath,
				EvidenceKindHelmValues, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindHelmValues),
				"Helm values reference the target repository",
				"helm", matcher, seen, details,
			)...)
		}
	}

	return evidence
}

func discoverStructuredArgoCDEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	var evidence []EvidenceFact

	applications, _ := factschema.DecodeParsedFileDataArgoCDApplications(parsedFileData)
	for _, application := range applications {
		appName := strings.TrimSpace(application.Name)
		for _, source := range argoApplicationSourceRefs(application) {
			details := withFirstPartyRefDetails(
				map[string]any{
					"argocd_application_name": appName,
					"source_revision":         source.revision,
				},
				"argocd_application_source",
				appName,
				source.path,
				source.root,
				source.revision,
				source.repoURL,
			)
			evidence = append(evidence, matchCatalog(
				sourceRepoID, source.repoURL, filePath,
				EvidenceKindArgoCDAppSource, RelDeploysFrom, DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindArgoCDAppSource),
				"ArgoCD Application source references the target repository",
				"argocd", matcher, seen, details,
			)...)

			for _, deployedRepo := range matchingCatalogEntries(source.repoURL, matcher) {
				evidence = append(evidence, appendDestinationPlatformEvidence(
					deployedRepo.RepoID,
					filePath,
					argocdDestination{
						name:      application.DestName,
						namespace: application.DestNamespace,
						server:    application.DestServer,
					},
					seen,
				)...)
			}
		}
	}

	appSets, _ := factschema.DecodeParsedFileDataArgoCDApplicationSets(parsedFileData)
	for _, appSet := range appSets {
		appSetName := strings.TrimSpace(appSet.Name)
		discoveryRepos := csvValues(appSet.GeneratorSourceRepos)
		discoveryPaths := csvValues(appSet.GeneratorSourcePaths)
		discoveryRoots := csvValues(appSet.GeneratorSourceRoots)
		if len(discoveryRoots) == 0 {
			discoveryRoots = csvValues(appSet.SourceRoots)
		}
		templateRepos := csvValues(appSet.TemplateSourceRepos)
		templatePaths := csvValues(appSet.TemplateSourcePaths)
		templateRoots := csvValues(appSet.TemplateSourceRoots)
		if len(templateRoots) == 0 {
			templateRoots = csvValues(appSet.SourceRoots)
		}

		for _, repoURL := range discoveryRepos {
			root := firstCSV(discoveryRoots)
			path := firstCSV(discoveryPaths)
			for _, configRepo := range matchingCatalogEntries(repoURL, matcher) {
				if configRepo.RepoID == sourceRepoID {
					continue
				}
				evidence = append(evidence, appendDiscoveryEvidence(
					sourceRepoID, configRepo, filePath, path, seen,
				)...)
				applyStructuredRefDetails(evidence, EvidenceKindArgoCDApplicationSetDiscovery, configRepo.RepoID, func(details map[string]any) map[string]any {
					return withFirstPartyRefDetails(
						mergeDetails(details, map[string]any{"argocd_applicationset_name": appSetName}),
						"argocd_applicationset_discovery", appSetName, path, root, "", repoURL,
					)
				})

				for _, templateRepoURL := range templateRepos {
					templatePath := firstCSV(templatePaths)
					templateRoot := firstCSV(templateRoots)
					for _, deployedRepo := range matchingCatalogEntries(templateRepoURL, matcher) {
						if deployedRepo.RepoID == configRepo.RepoID || deployedRepo.RepoID == sourceRepoID {
							continue
						}
						evidence = append(evidence, appendDeploySourceEvidence(
							sourceRepoID, deployedRepo, configRepo, filePath, path, templateRepoURL, seen,
						)...)
						applyStructuredRefDetails(evidence, EvidenceKindArgoCDApplicationSetDeploySource, configRepo.RepoID, func(details map[string]any) map[string]any {
							return withFirstPartyRefDetails(
								mergeDetails(details, map[string]any{"argocd_applicationset_name": appSetName}),
								"argocd_applicationset_template_source", appSetName, templatePath, templateRoot, "", templateRepoURL,
							)
						})
						evidence = append(evidence, appendDestinationPlatformEvidence(
							deployedRepo.RepoID,
							filePath,
							argocdDestination{
								name:      appSet.DestName,
								namespace: appSet.DestNamespace,
								server:    appSet.DestServer,
							},
							seen,
						)...)
					}
				}
			}
		}
	}

	return evidence
}

type argoApplicationSourceRef struct {
	repoURL  string
	path     string
	root     string
	revision string
}

// argoApplicationSourceRefs reads the typed, already-decoded
// codegraphv1.ArgoCDApplication's *_repos/*_paths/*_roots/*_revisions
// string fields, not a raw map. This is a deliberately stricter contract
// than the pre-issue-#5445 raw-map read, and the tradeoff is intentional
// (Finding 4, issue #5445 review): the pre-typing read passed the RAW
// parsed_file_data value straight to tupleCSVValues, which tolerated a
// string, a []string, or a []any per field. Since the #5445 slice 1
// migration, factschema.DecodeParsedFileDataArgoCDApplications enforces
// SourceRepos (and every other CSV-ish field here) as a Go string at decode
// time (sdk/go/factschema/decode_map.go's assignField, reflect.String case).
// A producer that ever emitted one of these fields as an array instead of a
// comma-joined string would make decodeMapInto error, and
// decodeParsedFileDataTolerantSlice
// (sdk/go/factschema/decode_parsed_file_data_tolerant.go) skips the WHOLE
// malformed Application/ApplicationSet row -- not just the one bad field --
// silently dropping every other well-formed field on that same row (Finding
// 2's operator-signal debug log is the only surfaced trace of that drop).
//
// This is not special-cased back to per-field tolerance here, for two
// reasons. First, the sole real producer
// (go/internal/parser/yaml/argocd.go's joinArgoSourceTupleValues) always
// returns a Go string and only a Go string for source_repos/source_paths/
// source_roots/source_revisions/generator_source_repos/... -- there is no
// live path that emits an array today, and codegraphv1.ArgoCDApplication's
// own doc comment (parsed_file_data_gitops.go) already states these fields
// are "always a comma-joined CSV string on the wire, not a JSON array."
// Second, this row-atomic-on-type-mismatch behavior is not unique to the
// CSV-ish fields: EVERY named field on EVERY one of the 8 issue-#5445-slice-1
// accessors (Name, DestName, DestNamespace, URL, ...) has the identical
// whole-row-drop-on-mismatch contract, because they all route through the
// same decodeParsedFileDataTolerantSlice. Restoring tolerance only for
// SourceRepos would patch one field while leaving the identical landmine on
// every other typed field this package reads, which is a general
// row-vs-field-tolerance design question for decodeParsedFileDataTolerantSlice
// itself, not something to special-case per call site.
//
// tupleCSVValues' []string and []any type-switch cases below are therefore
// unreachable dead code: this function is tupleCSVValues' only caller in the
// repository (rg confirms four call sites, all four here, all four passing
// an already-decode-time-enforced string field), so the dynamic type is
// always string. Removing the now-dead branches is a real cleanup but is out
// of scope for this change; flagged separately rather than expanding this
// PR's surface.
func argoApplicationSourceRefs(application codegraphv1.ArgoCDApplication) []argoApplicationSourceRef {
	repos := tupleCSVValues(application.SourceRepos)
	if len(repos) == 0 {
		repos = csvValues(application.SourceRepo)
	}
	paths := tupleCSVValues(application.SourcePaths)
	roots := tupleCSVValues(application.SourceRoots)
	revisions := tupleCSVValues(application.SourceRevisions)
	if len(repos) == 1 {
		paths = fallbackCSV(paths, application.SourcePath)
		roots = fallbackCSV(roots, application.SourceRoot)
		revisions = fallbackCSV(revisions, application.SourceRevision)
	}

	refs := make([]argoApplicationSourceRef, 0, len(repos))
	for index, repoURL := range repos {
		repoURL = strings.TrimSpace(repoURL)
		if repoURL == "" {
			continue
		}
		refs = append(refs, argoApplicationSourceRef{
			repoURL:  repoURL,
			path:     indexedCSV(paths, index),
			root:     indexedCSV(roots, index),
			revision: indexedCSV(revisions, index),
		})
	}
	return refs
}

func tupleCSVValues(value any) []string {
	var values []string
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.Split(typed, ",")
		values = make([]string, len(parts))
		for index, part := range parts {
			values[index] = strings.TrimSpace(part)
		}
	// DEAD as of #5445 slice 1: every caller now passes a typed string field
	// (ArgoCDApplication.SourceRepos etc.), decode-enforced by assignField's
	// reflect.String case, so only the string case above executes. These two
	// branches are retained deliberately, not as a live safety net -- do not
	// rely on them handling a []string or []any value, because a value of that
	// shape can no longer reach this function. Removal is tracked as follow-up;
	// see the doc comment on argoApplicationSourceRefs.
	case []string:
		values = make([]string, len(typed))
		for index, part := range typed {
			values[index] = strings.TrimSpace(part)
		}
	case []any:
		values = make([]string, len(typed))
		for index, part := range typed {
			if value, ok := part.(string); ok {
				values[index] = strings.TrimSpace(value)
			}
		}
	default:
		return nil
	}
	if !hasNonEmptyCSVValue(values) {
		return nil
	}
	return values
}

func hasNonEmptyCSVValue(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}

func fallbackCSV(values []string, fallback any) []string {
	if len(values) > 0 {
		return values
	}
	return csvValues(fallback)
}

func indexedCSV(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func applyStructuredRefDetails(
	evidence []EvidenceFact,
	kind EvidenceKind,
	targetRepoID string,
	update func(map[string]any) map[string]any,
) {
	for index := range evidence {
		if evidence[index].EvidenceKind != kind {
			continue
		}
		if targetRepoID != "" && evidence[index].TargetRepoID != targetRepoID {
			continue
		}
		evidence[index].Details = update(evidence[index].Details)
	}
}

func firstCSV(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

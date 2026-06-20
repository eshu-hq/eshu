package componentindex

import "testing"

func TestValidateAcceptsReviewedEntry(t *testing.T) {
	report := Validate(validIndex())

	if !report.Valid {
		t.Fatalf("Validate() valid = false, issues = %+v", report.Issues)
	}
}

func TestValidateRejectsRequiredIssueFixtures(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Index)
		code IssueCode
	}{
		{
			name: "missing metadata",
			mut: func(index *Index) {
				index.Entries[0].ComponentID = ""
			},
			code: IssueMissingMetadata,
		},
		{
			name: "duplicate component id",
			mut: func(index *Index) {
				index.Entries = append(index.Entries, validEntry())
			},
			code: IssueDuplicateComponentID,
		},
		{
			name: "duplicate fact kind",
			mut: func(index *Index) {
				second := validEntry()
				second.ComponentID = "dev.eshu.collector.second"
				index.Entries = append(index.Entries, second)
			},
			code: IssueDuplicateFactKind,
		},
		{
			name: "malformed manifest digest",
			mut: func(index *Index) {
				index.Entries[0].ManifestDigest = "sha256:not-hex"
			},
			code: IssueMalformedDigest,
		},
		{
			name: "mutable artifact tag",
			mut: func(index *Index) {
				index.Entries[0].Artifacts[0].Image = "ghcr.io/eshu/example:latest"
			},
			code: IssueMutableArtifactTag,
		},
		{
			name: "unsupported lifecycle channel",
			mut: func(index *Index) {
				index.Entries[0].LifecycleChannel = "trusted"
			},
			code: IssueUnsupportedChannel,
		},
		{
			name: "missing review link",
			mut: func(index *Index) {
				index.Entries[0].Review.PR = ""
			},
			code: IssueMissingReviewLink,
		},
		{
			name: "revoked installable entry",
			mut: func(index *Index) {
				index.Entries[0].Revocation.Revoked = true
				index.Entries[0].Installable = true
			},
			code: IssueRevokedInstallable,
		},
		{
			name: "core-owned fact kind",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].Kind = "gcp_cloud_resource"
			},
			code: IssueUnsupportedFactKind,
		},
		{
			name: "non-namespaced fact kind",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].Kind = "custom_observation"
			},
			code: IssueUnsupportedFactKind,
		},
		{
			name: "invalid schema version",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].SchemaVersions = []string{"one"}
			},
			code: IssueUnsupportedSchemaVersion,
		},
		{
			name: "unsupported source confidence",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].SourceConfidence = []string{"guessed"}
			},
			code: IssueUnsupportedSourceConfidence,
		},
		{
			name: "unknown source confidence",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].SourceConfidence = []string{"unknown"}
			},
			code: IssueUnsupportedSourceConfidence,
		},
		{
			name: "missing reducer consumer contract",
			mut: func(index *Index) {
				index.Entries[0].ConsumerContracts.Reducer.Phases = nil
			},
			code: IssueMissingConsumerContract,
		},
		{
			name: "missing provenance signature",
			mut: func(index *Index) {
				index.Entries[0].Provenance.Signature = ""
			},
			code: IssueMissingProvenanceSignature,
		},
		{
			name: "missing conformance proof",
			mut: func(index *Index) {
				index.Entries[0].Conformance.ProofURI = ""
			},
			code: IssueMissingConformanceProof,
		},
		{
			name: "failed conformance proof",
			mut: func(index *Index) {
				index.Entries[0].Conformance.Status = "failed"
			},
			code: IssueFailedConformanceProof,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := validIndex()
			tt.mut(&index)

			report := Validate(index)

			if report.Valid {
				t.Fatalf("Validate() valid = true, want issue code %q", tt.code)
			}
			if !hasIssue(report.Issues, tt.code) {
				t.Fatalf("Validate() issues = %+v, want code %q", report.Issues, tt.code)
			}
		})
	}
}

func TestValidateRejectsMissingRequiredEntryShape(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Index)
	}{
		{
			name: "compatible core",
			mut: func(index *Index) {
				index.Entries[0].CompatibleCore = ""
			},
		},
		{
			name: "component type",
			mut: func(index *Index) {
				index.Entries[0].ComponentType = ""
			},
		},
		{
			name: "collector kinds",
			mut: func(index *Index) {
				index.Entries[0].CollectorKinds = nil
			},
		},
		{
			name: "fact schema versions",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].SchemaVersions = nil
			},
		},
		{
			name: "fact source confidence",
			mut: func(index *Index) {
				index.Entries[0].EmittedFacts[0].SourceConfidence = nil
			},
		},
		{
			name: "telemetry prefix",
			mut: func(index *Index) {
				index.Entries[0].Telemetry.MetricsPrefix = ""
			},
		},
		{
			name: "source repository",
			mut: func(index *Index) {
				index.Entries[0].Source.Repository = ""
			},
		},
		{
			name: "provenance mode",
			mut: func(index *Index) {
				index.Entries[0].Provenance.Mode = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := validIndex()
			tt.mut(&index)

			report := Validate(index)

			if report.Valid {
				t.Fatalf("Validate() valid = true, want missing metadata issue")
			}
			if !hasIssue(report.Issues, IssueMissingMetadata) {
				t.Fatalf("Validate() issues = %+v, want missing metadata issue", report.Issues)
			}
		})
	}
}

func validIndex() Index {
	return Index{
		APIVersion: "eshu.dev/community-extension-index/v1alpha1",
		Kind:       "CommunityExtensionIndex",
		Entries:    []Entry{validEntry()},
	}
}

func validEntry() Entry {
	return Entry{
		ComponentID:      "dev.eshu.collector.example",
		Publisher:        "eshu-hq",
		Version:          "0.1.0",
		LifecycleChannel: ChannelCommunityMaintained,
		Installable:      true,
		ManifestDigest:   "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Artifacts: []ArtifactRef{{
			Image: "ghcr.io/eshu/example@sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		}},
		CompatibleCore: ">=0.1.0",
		ComponentType:  "collector",
		CollectorKinds: []string{"example"},
		EmittedFacts: []FactClaim{{
			Kind:             "community.example.resource",
			SchemaVersions:   []string{"1.0.0"},
			SourceConfidence: []string{"observed"},
		}},
		ConsumerContracts: ConsumerContracts{
			Reducer: ReducerContract{Phases: []string{"community_extension"}},
		},
		Telemetry: Telemetry{MetricsPrefix: "eshu_extension_example"},
		Source: SourceRef{
			Repository: "https://github.com/eshu-hq/example-extension",
		},
		Review: ReviewRef{
			PR: "https://github.com/eshu-hq/eshu/pull/1904",
		},
		Provenance: Provenance{
			Required:  true,
			Mode:      "sigstore",
			Signature: "sigstore:sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		Conformance: ConformanceProof{
			SchemaVersion: "eshu.extension.conformance.v1",
			Status:        "passed",
			ProofURI:      "https://github.com/eshu-hq/eshu/actions/runs/1234567890",
		},
	}
}

func hasIssue(issues []Issue, code IssueCode) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

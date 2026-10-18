// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const supportedSpecVersion = "1.1.0"

// payloadSchemaDir is the repo-relative directory that houses checked-in
// JSON Schema artifacts. A family/kind's payload_schema value must resolve
// to a file under this directory or generation fails closed.
const payloadSchemaDir = "sdk/go/factschema/schema"

type options struct {
	repoRoot string
	specPath string
	goOut    string
	docOut   string
	check    bool
}

type specFile struct {
	Version  string                `yaml:"version"`
	Families map[string]familySpec `yaml:"families"`
}

type familySpec struct {
	LifecycleOwner        string            `yaml:"lifecycle_owner"`
	SchemaVersion         string            `yaml:"schema_version"`
	SchemaVersionOverride map[string]string `yaml:"schema_version_overrides"`
	ReducerDomain         string            `yaml:"reducer_domain"`
	// ReducerDir is the legacy registry reducer_dir hint. It is carried
	// through generation for YAML compatibility and, as before, is not
	// consumed when building registry entries; verified reducer
	// ownership lives in contract.Binding declarations.
	ReducerDir             string            `yaml:"reducer_dir"`
	ProjectionHook         string            `yaml:"projection_hook"`
	AdmissionHook          string            `yaml:"admission_hook"`
	ReadSurface            string            `yaml:"read_surface"`
	ReadSurfaceOverrides   map[string]string `yaml:"read_surface_overrides"`
	TruthProfile           string            `yaml:"truth_profile"`
	PolicyGate             string            `yaml:"policy_gate"`
	ProviderKeyIndependent bool              `yaml:"provider_key_independent"`
	// PayloadSchema is the family-level default repo-relative path to a
	// checked-in JSON Schema artifact under sdk/go/factschema/schema/.
	// Optional; most families leave this and the per-kind override unset
	// until their fact kinds gain a typed sdk/go/factschema struct.
	PayloadSchema string `yaml:"payload_schema"`
	// PayloadSchemaOverrides sets payload_schema per kind, following the
	// same per-kind override pattern as schema_version_overrides and
	// read_surface_overrides.
	PayloadSchemaOverrides map[string]string `yaml:"payload_schema_overrides"`
	// DeprecatedIn is the family-level default deprecation marker (semver).
	// Optional.
	DeprecatedIn string `yaml:"deprecated_in"`
	// DeprecatedInOverrides sets deprecated_in per kind.
	DeprecatedInOverrides map[string]string `yaml:"deprecated_in_overrides"`
	// RemovedIn is the family-level default removal marker (semver).
	// Optional.
	RemovedIn string `yaml:"removed_in"`
	// RemovedInOverrides sets removed_in per kind.
	RemovedInOverrides map[string]string `yaml:"removed_in_overrides"`
	// AdmissionExempt registers a legacy family for its contract metadata
	// (notably payload_schema) without enrolling it in schema-version
	// admission. An exempt family has no live (XFactKinds, XSchemaVersion) Go
	// pair, must leave schema_version blank, and its kinds classify as
	// CompatibilityUnknownKind at runtime — identical to an unregistered kind.
	// This decouples registry membership from mandatory version admission for
	// the legacy git code-graph kinds (file, repository); see issue #4752.
	AdmissionExempt bool     `yaml:"admission_exempt"`
	Kinds           []string `yaml:"kinds"`
}

type liveFamily struct {
	name    string
	kinds   func() []string
	version func(string) (string, bool)
}

type registryEntry struct {
	facts.FactKindRegistryEntry
	family string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	outputs, err := buildOutputs(opts)
	if err != nil {
		return err
	}
	if err := applyOutputs(opts.repoRoot, outputs, opts.check); err != nil {
		return err
	}
	if opts.check {
		_, _ = fmt.Fprintln(stdout, "fact-kind-registry: generated artifacts are current")
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "fact-kind-registry: wrote %d generated files\n", len(outputs))
	return nil
}

// buildOutputs discovers and validates the typed contract declarations
// under the repo's Go tree, derives the legacy registry spec from
// them, and renders the full deterministic output set: the compact
// registry YAML, the generated Go registry and Markdown tables at
// their existing paths, and the contracts.d shards and catalog pages.
func buildOutputs(opts options) ([]outputFile, error) {
	families, bindings, diags := discoverDeclarations(opts.repoRoot, filepath.Join(opts.repoRoot, "go"))
	diags = append(diags, validateContracts(opts.repoRoot, families, bindings)...)
	if len(families) == 0 && len(diags) == 0 {
		diags.addf(sourcePos{file: "go", line: 1}, "no contract.Declare family declarations found; ACTION: declare each family beside its fact definitions")
	}
	if err := diags.err(); err != nil {
		return nil, err
	}
	spec := deriveSpec(families)
	entries, err := buildRegistry(opts.repoRoot, spec)
	if err != nil {
		return nil, err
	}
	goBytes, err := renderGo(entries)
	if err != nil {
		return nil, err
	}
	yamlBytes, err := renderRegistryYAML(spec)
	if err != nil {
		return nil, err
	}
	shards, err := renderShards(families, bindings, entries)
	if err != nil {
		return nil, err
	}
	outputs := make([]outputFile, 0, len(shards)+3)
	for _, named := range []struct {
		abs  string
		data []byte
	}{
		{opts.specPath, yamlBytes},
		{opts.goOut, goBytes},
		{opts.docOut, renderMarkdown(entries)},
	} {
		rel, err := filepath.Rel(opts.repoRoot, named.abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("generated output %s must live under -repo-root %s", named.abs, opts.repoRoot)
		}
		outputs = append(outputs, outputFile{rel: filepath.ToSlash(rel), data: named.data})
	}
	outputs = append(outputs, shards...)
	if err := validateOutputSet(outputs); err != nil {
		return nil, err
	}
	return outputs, nil
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("fact-kind-registry", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := options{}
	flags.StringVar(&opts.repoRoot, "repo-root", "..", "repository root")
	flags.StringVar(&opts.specPath, "spec", "", "generated fact-kind registry YAML output path")
	flags.StringVar(&opts.goOut, "go-out", "", "generated Go output path")
	flags.StringVar(&opts.docOut, "doc-out", "", "generated Markdown output path")
	flags.BoolVar(&opts.check, "check", false, "verify generated files without writing")
	if err := flags.Parse(args); err != nil {
		return options{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	if opts.repoRoot == "" {
		return options{}, fmt.Errorf("-repo-root is required")
	}
	if strings.TrimSpace(opts.specPath) == "" {
		opts.specPath = filepath.Join(opts.repoRoot, "specs", "fact-kind-registry.v1.yaml")
	}
	if strings.TrimSpace(opts.goOut) == "" {
		opts.goOut = filepath.Join(opts.repoRoot, "go", "internal", "facts", "fact_kind_registry.generated.go")
	}
	if strings.TrimSpace(opts.docOut) == "" {
		opts.docOut = filepath.Join(opts.repoRoot, "go", "internal", "facts", "FACT_KIND_REGISTRIES.md")
	}
	return opts, nil
}

func buildRegistry(repoRoot string, spec specFile) ([]registryEntry, error) {
	liveByName := map[string]liveFamily{}
	for _, family := range liveFamilies() {
		liveByName[family.name] = family
		if _, ok := spec.Families[family.name]; !ok {
			return nil, fmt.Errorf("spec missing live fact family %q", family.name)
		}
	}
	var entries []registryEntry
	seen := map[string]string{}
	for name, familySpec := range spec.Families {
		live, ok := liveByName[name]
		if !ok && !familySpec.AdmissionExempt {
			// A non-exempt family must have a live (XFactKinds,
			// XSchemaVersion) Go pair backing it. An admission-exempt family
			// has none by design and is allowed through with a zero live.
			return nil, fmt.Errorf("spec references unknown fact family %q", name)
		}
		familyEntries, err := buildFamilyEntries(repoRoot, name, live, familySpec)
		if err != nil {
			return nil, err
		}
		for _, entry := range familyEntries {
			if previous, dup := seen[entry.Kind]; dup {
				return nil, fmt.Errorf("fact kind %q appears in families %q and %q", entry.Kind, previous, name)
			}
			seen[entry.Kind] = name
			entries = append(entries, registryEntry{FactKindRegistryEntry: entry, family: name})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Kind < entries[j].Kind })
	return entries, nil
}

func buildFamilyEntries(repoRoot, name string, live liveFamily, spec familySpec) ([]facts.FactKindRegistryEntry, error) {
	if err := validateFamilyMetadata(name, spec); err != nil {
		return nil, err
	}
	specKinds := sortedUnique(spec.Kinds)
	// A non-exempt family's kinds must match its live Go helper exactly. An
	// admission-exempt family has no live helper (live is zero), so it skips
	// the drift check and derives everything from the spec alone.
	if !spec.AdmissionExempt {
		liveKinds := sortedUnique(live.kinds())
		if !stringSlicesEqual(liveKinds, specKinds) {
			return nil, fmt.Errorf("family %q kinds drifted: spec=%v live=%v", name, specKinds, liveKinds)
		}
	}
	if err := validateKindOverrides(name, "read_surface_overrides", spec.ReadSurfaceOverrides, specKinds); err != nil {
		return nil, err
	}
	if err := validateKindOverrides(name, "payload_schema_overrides", spec.PayloadSchemaOverrides, specKinds); err != nil {
		return nil, err
	}
	if err := validateKindOverrides(name, "deprecated_in_overrides", spec.DeprecatedInOverrides, specKinds); err != nil {
		return nil, err
	}
	if err := validateKindOverrides(name, "removed_in_overrides", spec.RemovedInOverrides, specKinds); err != nil {
		return nil, err
	}
	entries := make([]facts.FactKindRegistryEntry, 0, len(specKinds))
	for _, kind := range specKinds {
		// An admission-exempt kind carries no schema version and is not backed
		// by a live version helper; every other kind must match its live
		// helper exactly. validateFamilyMetadata already rejects a non-blank
		// schema_version on an exempt family.
		var specVersion string
		if !spec.AdmissionExempt {
			wantVersion, ok := live.version(kind)
			if !ok {
				return nil, fmt.Errorf("family %q kind %q has no live schema version", name, kind)
			}
			specVersion = spec.SchemaVersion
			if override := strings.TrimSpace(spec.SchemaVersionOverride[kind]); override != "" {
				specVersion = override
			}
			if specVersion != wantVersion {
				return nil, fmt.Errorf("family %q kind %q schema_version = %q, live helper returns %q", name, kind, specVersion, wantVersion)
			}
		}
		readSurface := spec.ReadSurface
		if override := strings.TrimSpace(spec.ReadSurfaceOverrides[kind]); override != "" {
			readSurface = override
		}
		payloadSchema := spec.PayloadSchema
		if override := strings.TrimSpace(spec.PayloadSchemaOverrides[kind]); override != "" {
			payloadSchema = override
		}
		if err := validatePayloadSchemaReference(repoRoot, name, kind, payloadSchema); err != nil {
			return nil, err
		}
		deprecatedIn := spec.DeprecatedIn
		if override := strings.TrimSpace(spec.DeprecatedInOverrides[kind]); override != "" {
			deprecatedIn = override
		}
		removedIn := spec.RemovedIn
		if override := strings.TrimSpace(spec.RemovedInOverrides[kind]); override != "" {
			removedIn = override
		}
		if err := validateLifecycleMarker(name, kind, "deprecated_in", deprecatedIn); err != nil {
			return nil, err
		}
		if err := validateLifecycleMarker(name, kind, "removed_in", removedIn); err != nil {
			return nil, err
		}
		if strings.TrimSpace(removedIn) != "" && strings.TrimSpace(deprecatedIn) == "" {
			return nil, fmt.Errorf("family %q kind %q has removed_in set without deprecated_in", name, kind)
		}
		entries = append(entries, facts.FactKindRegistryEntry{
			Kind:                   kind,
			SchemaVersion:          specVersion,
			LifecycleOwner:         spec.LifecycleOwner,
			ReducerDomain:          spec.ReducerDomain,
			ProjectionHook:         spec.ProjectionHook,
			AdmissionHook:          spec.AdmissionHook,
			ReadSurface:            readSurface,
			TruthProfile:           facts.FactKindTruthProfile(spec.TruthProfile),
			PolicyGate:             spec.PolicyGate,
			ProviderKeyIndependent: spec.ProviderKeyIndependent,
			PayloadSchema:          strings.TrimSpace(payloadSchema),
			DeprecatedIn:           strings.TrimSpace(deprecatedIn),
			RemovedIn:              strings.TrimSpace(removedIn),
			AdmissionExempt:        spec.AdmissionExempt,
		})
	}
	return entries, nil
}

func renderGo(entries []registryEntry) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// SPDX-License-Identifier: MIT\n")
	buf.WriteString("// Copyright (c) 2025-2026 eshu-hq\n")
	buf.WriteString("//\n")
	buf.WriteString("// Code generated by go/cmd/fact-kind-registry; DO NOT EDIT.\n")
	buf.WriteString("// Source-of-truth: typed fact-family contract declarations in Go source.\n\n")
	buf.WriteString("package facts\n\n")
	buf.WriteString("var factKindRegistryEntries = []FactKindRegistryEntry{\n")
	for _, entry := range entries {
		fmt.Fprintf(&buf, "\t{Kind: %q, SchemaVersion: %q, LifecycleOwner: %q, ReducerDomain: %q, ProjectionHook: %q, AdmissionHook: %q, ReadSurface: %q, TruthProfile: %q, PolicyGate: %q, ProviderKeyIndependent: %t, PayloadSchema: %q, DeprecatedIn: %q, RemovedIn: %q, AdmissionExempt: %t},\n",
			entry.Kind, entry.SchemaVersion, entry.LifecycleOwner, entry.ReducerDomain, entry.ProjectionHook,
			entry.AdmissionHook, entry.ReadSurface, entry.TruthProfile, entry.PolicyGate, entry.ProviderKeyIndependent,
			entry.PayloadSchema, entry.DeprecatedIn, entry.RemovedIn, entry.AdmissionExempt)
	}
	buf.WriteString("}\n\n")
	buf.WriteString("var factKindRegistryByKind = buildFactKindRegistryByKind(factKindRegistryEntries)\n")
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated Go: %w", err)
	}
	return formatted, nil
}

func renderMarkdown(entries []registryEntry) []byte {
	var buf bytes.Buffer
	buf.WriteString("# Fact Kind Registries\n\n")
	buf.WriteString("Generated from typed fact-family contract declarations in Go source. Do not edit this file by hand; run `scripts/generate-fact-kind-registry.sh`.\n\n")
	buf.WriteString("| Fact kind | Schema | Owner | Reducer domain | Projection | Admission | Read surface | Truth profile | Policy gate | No-provider | Payload schema | Deprecated in | Removed in | Admission exempt |\n")
	buf.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range entries {
		fmt.Fprintf(&buf, "| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%t` | `%s` | `%s` | `%s` | `%t` |\n",
			entry.Kind, entry.SchemaVersion, entry.LifecycleOwner, entry.ReducerDomain, entry.ProjectionHook,
			entry.AdmissionHook, entry.ReadSurface, entry.TruthProfile, entry.PolicyGate, entry.ProviderKeyIndependent,
			entry.PayloadSchema, entry.DeprecatedIn, entry.RemovedIn, entry.AdmissionExempt)
	}
	return buf.Bytes()
}

func writeFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { // #nosec G301 -- repo-local generated artifact directory.
		return fmt.Errorf("create generated file directory %s: %w", filepath.Dir(path), err)
	}
	current, err := os.ReadFile(path) // #nosec G304 -- path is an internally configured generated artifact.
	if err == nil && bytes.Equal(current, contents) {
		return nil
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil { // #nosec G306 -- generated source/doc artifact.
		return fmt.Errorf("write generated file %s: %w", path, err)
	}
	return nil
}

func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return compactStrings(out)
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var previous string
	for i, value := range values {
		if value == "" || (i > 0 && value == previous) {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func liveFamilies() []liveFamily {
	return []liveFamily{
		{"aws", cloud.AWSFactKinds, cloud.AWSSchemaVersion},
		{"azure", cloud.AzureFactKinds, cloud.AzureSchemaVersion},
		{"ci_cd_run", facts.CICDRunFactKinds, facts.CICDRunSchemaVersion},
		{"codeowners", facts.CodeownersFactKinds, facts.CodeownersSchemaVersion},
		{"documentation", docs.FactKinds, docs.SchemaVersion},
		{"ec2_instance_posture", cloud.EC2InstancePostureFactKinds, cloud.EC2InstancePostureSchemaVersion},
		{"gcp", cloud.GCPFactKinds, cloud.GCPSchemaVersion},
		{"incident_context", facts.IncidentContextFactKinds, facts.IncidentContextSchemaVersion},
		{"incident_routing", facts.IncidentRoutingFactKinds, facts.IncidentRoutingSchemaVersion},
		{"kubernetes_live", cloud.KubernetesLiveFactKinds, cloud.KubernetesLiveSchemaVersion},
		{"observability", facts.ObservabilityFactKinds, facts.ObservabilitySchemaVersion},
		{"oci_registry", chain.OCIRegistryFactKinds, chain.OCIRegistrySchemaVersion},
		{"package_registry", chain.PackageRegistryFactKinds, chain.PackageRegistrySchemaVersion},
		{"rds_posture", cloud.RDSPostureFactKinds, cloud.RDSPostureSchemaVersion},
		{"reducer_derived", facts.ReducerDerivedFactKinds, facts.ReducerDerivedSchemaVersion},
		{"ref_overlay", facts.RefOverlayFactKinds, facts.RefOverlaySchemaVersion},
		{"s3_bucket_posture", cloud.S3BucketPostureFactKinds, cloud.S3BucketPostureSchemaVersion},
		{"s3_external_principal_grant", cloud.S3ExternalPrincipalGrantFactKinds, cloud.S3ExternalPrincipalGrantSchemaVersion},
		{"sbom_attestation", chain.SBOMAttestationFactKinds, chain.SBOMAttestationSchemaVersion},
		{"scanner_worker", facts.ScannerWorkerFactKinds, facts.ScannerWorkerSchemaVersion},
		{"secrets_iam", facts.SecretsIAMFactKinds, facts.SecretsIAMSchemaVersion},
		{"security_alert", facts.SecurityAlertFactKinds, facts.SecurityAlertSchemaVersion},
		{"semantic", facts.SemanticFactKinds, facts.SemanticSchemaVersion},
		{"service_catalog", facts.ServiceCatalogFactKinds, facts.ServiceCatalogSchemaVersion},
		{"submodule", facts.SubmoduleFactKinds, facts.SubmoduleSchemaVersion},
		{"terraform_state", cloud.TerraformStateFactKinds, cloud.TerraformStateSchemaVersion},
		{"vulnerability_intelligence", chain.VulnerabilityIntelligenceFactKinds, chain.VulnerabilityIntelligenceSchemaVersion},
		{"vulnerability_suppression", chain.VulnerabilitySuppressionFactKinds, chain.VulnerabilitySuppressionSchemaVersion},
		{"work_item", facts.WorkItemFactKinds, facts.WorkItemSchemaVersion},
	}
}

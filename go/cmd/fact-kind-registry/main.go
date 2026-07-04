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
	"gopkg.in/yaml.v3"
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
	LifecycleOwner         string            `yaml:"lifecycle_owner"`
	SchemaVersion          string            `yaml:"schema_version"`
	SchemaVersionOverride  map[string]string `yaml:"schema_version_overrides"`
	ReducerDomain          string            `yaml:"reducer_domain"`
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
	Kinds              []string          `yaml:"kinds"`
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
	spec, err := loadSpec(opts.specPath)
	if err != nil {
		return err
	}
	entries, err := buildRegistry(opts.repoRoot, spec)
	if err != nil {
		return err
	}
	goBytes, err := renderGo(entries)
	if err != nil {
		return err
	}
	docBytes := renderMarkdown(entries)
	if opts.check {
		if err := verifyFile(opts.goOut, goBytes, "scripts/generate-fact-kind-registry.sh"); err != nil {
			return err
		}
		if err := verifyFile(opts.docOut, docBytes, "scripts/generate-fact-kind-registry.sh"); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "fact-kind-registry: generated artifacts are current")
		return nil
	}
	if err := writeFile(opts.goOut, goBytes); err != nil {
		return err
	}
	if err := writeFile(opts.docOut, docBytes); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "fact-kind-registry: wrote %s and %s\n", opts.goOut, opts.docOut)
	return nil
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("fact-kind-registry", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := options{}
	flags.StringVar(&opts.repoRoot, "repo-root", "..", "repository root")
	flags.StringVar(&opts.specPath, "spec", "", "fact-kind registry YAML path")
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

func loadSpec(path string) (specFile, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a repo-local generator input.
	if err != nil {
		return specFile{}, fmt.Errorf("read fact-kind registry spec: %w", err)
	}
	var spec specFile
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return specFile{}, fmt.Errorf("decode fact-kind registry spec: %w", err)
	}
	if spec.Version != supportedSpecVersion {
		return specFile{}, fmt.Errorf("fact-kind registry spec version %q not supported (expected %q)", spec.Version, supportedSpecVersion)
	}
	if len(spec.Families) == 0 {
		return specFile{}, fmt.Errorf("fact-kind registry spec must declare families")
	}
	return spec, nil
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
		if !ok {
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
	liveKinds := sortedUnique(live.kinds())
	specKinds := sortedUnique(spec.Kinds)
	if !stringSlicesEqual(liveKinds, specKinds) {
		return nil, fmt.Errorf("family %q kinds drifted: spec=%v live=%v", name, specKinds, liveKinds)
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
		wantVersion, ok := live.version(kind)
		if !ok {
			return nil, fmt.Errorf("family %q kind %q has no live schema version", name, kind)
		}
		specVersion := spec.SchemaVersion
		if override := strings.TrimSpace(spec.SchemaVersionOverride[kind]); override != "" {
			specVersion = override
		}
		if specVersion != wantVersion {
			return nil, fmt.Errorf("family %q kind %q schema_version = %q, live helper returns %q", name, kind, specVersion, wantVersion)
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
		})
	}
	return entries, nil
}

// validatePayloadSchemaReference fails closed when a non-blank payload_schema
// value does not resolve to a real file under sdk/go/factschema/schema/. A
// dangling reference — a typo, a moved file, or a schema that was never
// generated — must never be silently accepted as a valid contract pointer.
func validatePayloadSchemaReference(repoRoot, family, kind, payloadSchema string) error {
	ref := strings.TrimSpace(payloadSchema)
	if ref == "" {
		return nil
	}
	wantPrefix := payloadSchemaDir + "/"
	if !strings.HasPrefix(ref, wantPrefix) {
		return fmt.Errorf("family %q kind %q payload_schema %q must be under %s", family, kind, ref, payloadSchemaDir)
	}
	abs := filepath.Join(repoRoot, filepath.FromSlash(ref))
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("family %q kind %q payload_schema %q does not exist: %w", family, kind, ref, err)
	}
	if info.IsDir() {
		return fmt.Errorf("family %q kind %q payload_schema %q is a directory, want a file", family, kind, ref)
	}
	return nil
}

func validateFamilyMetadata(name string, spec familySpec) error {
	for field, value := range map[string]string{
		"lifecycle_owner": spec.LifecycleOwner,
		"schema_version":  spec.SchemaVersion,
		"reducer_domain":  spec.ReducerDomain,
		"projection_hook": spec.ProjectionHook,
		"admission_hook":  spec.AdmissionHook,
		"read_surface":    spec.ReadSurface,
		"truth_profile":   spec.TruthProfile,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("family %q missing %s", name, field)
		}
	}
	switch facts.FactKindTruthProfile(spec.TruthProfile) {
	case facts.FactKindTruthDeterministic:
		if !spec.ProviderKeyIndependent {
			return fmt.Errorf("family %q deterministic truth requires provider_key_independent", name)
		}
	case facts.FactKindTruthProviderGated, facts.FactKindTruthFixtureGated:
	case facts.FactKindTruthOptionalSemantic:
		if strings.TrimSpace(spec.PolicyGate) == "" {
			return fmt.Errorf("family %q optional_semantic truth requires policy_gate", name)
		}
	default:
		return fmt.Errorf("family %q unsupported truth_profile %q", name, spec.TruthProfile)
	}
	return nil
}

func validateKindOverrides(name, field string, overrides map[string]string, specKinds []string) error {
	if len(overrides) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(specKinds))
	for _, kind := range specKinds {
		known[kind] = struct{}{}
	}
	for kind, value := range overrides {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("family %q %s for kind %q is blank", name, field, kind)
		}
		if _, ok := known[kind]; !ok {
			return fmt.Errorf("family %q %s references unknown kind %q", name, field, kind)
		}
	}
	return nil
}

func renderGo(entries []registryEntry) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// SPDX-License-Identifier: MIT\n")
	buf.WriteString("// Copyright (c) 2025-2026 eshu-hq\n")
	buf.WriteString("//\n")
	buf.WriteString("// Code generated by go/cmd/fact-kind-registry; DO NOT EDIT.\n")
	buf.WriteString("// Source-of-truth: specs/fact-kind-registry.v1.yaml\n\n")
	buf.WriteString("package facts\n\n")
	buf.WriteString("var factKindRegistryEntries = []FactKindRegistryEntry{\n")
	for _, entry := range entries {
		fmt.Fprintf(&buf, "\t{Kind: %q, SchemaVersion: %q, LifecycleOwner: %q, ReducerDomain: %q, ProjectionHook: %q, AdmissionHook: %q, ReadSurface: %q, TruthProfile: %q, PolicyGate: %q, ProviderKeyIndependent: %t, PayloadSchema: %q, DeprecatedIn: %q, RemovedIn: %q},\n",
			entry.Kind, entry.SchemaVersion, entry.LifecycleOwner, entry.ReducerDomain, entry.ProjectionHook,
			entry.AdmissionHook, entry.ReadSurface, entry.TruthProfile, entry.PolicyGate, entry.ProviderKeyIndependent,
			entry.PayloadSchema, entry.DeprecatedIn, entry.RemovedIn)
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
	buf.WriteString("Generated from `specs/fact-kind-registry.v1.yaml`. Do not edit this file by hand; run `scripts/generate-fact-kind-registry.sh`.\n\n")
	buf.WriteString("| Fact kind | Schema | Owner | Reducer domain | Projection | Admission | Read surface | Truth profile | Policy gate | No-provider | Payload schema | Deprecated in | Removed in |\n")
	buf.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, entry := range entries {
		fmt.Fprintf(&buf, "| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%t` | `%s` | `%s` | `%s` |\n",
			entry.Kind, entry.SchemaVersion, entry.LifecycleOwner, entry.ReducerDomain, entry.ProjectionHook,
			entry.AdmissionHook, entry.ReadSurface, entry.TruthProfile, entry.PolicyGate, entry.ProviderKeyIndependent,
			entry.PayloadSchema, entry.DeprecatedIn, entry.RemovedIn)
	}
	return buf.Bytes()
}

func verifyFile(path string, want []byte, generateCommand string) error {
	got, err := os.ReadFile(path) // #nosec G304 -- path is an internally configured generated artifact.
	if err != nil {
		return fmt.Errorf("read generated file %s: %w", path, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("generated file %s is stale; run %s", path, generateCommand)
	}
	return nil
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
		{"aws", facts.AWSFactKinds, facts.AWSSchemaVersion},
		{"azure", facts.AzureFactKinds, facts.AzureSchemaVersion},
		{"ci_cd_run", facts.CICDRunFactKinds, facts.CICDRunSchemaVersion},
		{"documentation", facts.DocumentationFactKinds, facts.DocumentationSchemaVersion},
		{"ec2_instance_posture", facts.EC2InstancePostureFactKinds, facts.EC2InstancePostureSchemaVersion},
		{"gcp", facts.GCPFactKinds, facts.GCPSchemaVersion},
		{"incident_context", facts.IncidentContextFactKinds, facts.IncidentContextSchemaVersion},
		{"incident_routing", facts.IncidentRoutingFactKinds, facts.IncidentRoutingSchemaVersion},
		{"kubernetes_live", facts.KubernetesLiveFactKinds, facts.KubernetesLiveSchemaVersion},
		{"observability", facts.ObservabilityFactKinds, facts.ObservabilitySchemaVersion},
		{"oci_registry", facts.OCIRegistryFactKinds, facts.OCIRegistrySchemaVersion},
		{"package_registry", facts.PackageRegistryFactKinds, facts.PackageRegistrySchemaVersion},
		{"rds_posture", facts.RDSPostureFactKinds, facts.RDSPostureSchemaVersion},
		{"s3_bucket_posture", facts.S3BucketPostureFactKinds, facts.S3BucketPostureSchemaVersion},
		{"s3_external_principal_grant", facts.S3ExternalPrincipalGrantFactKinds, facts.S3ExternalPrincipalGrantSchemaVersion},
		{"sbom_attestation", facts.SBOMAttestationFactKinds, facts.SBOMAttestationSchemaVersion},
		{"scanner_worker", facts.ScannerWorkerFactKinds, facts.ScannerWorkerSchemaVersion},
		{"secrets_iam", facts.SecretsIAMFactKinds, facts.SecretsIAMSchemaVersion},
		{"security_alert", facts.SecurityAlertFactKinds, facts.SecurityAlertSchemaVersion},
		{"semantic", facts.SemanticFactKinds, facts.SemanticSchemaVersion},
		{"service_catalog", facts.ServiceCatalogFactKinds, facts.ServiceCatalogSchemaVersion},
		{"terraform_state", facts.TerraformStateFactKinds, facts.TerraformStateSchemaVersion},
		{"vulnerability_intelligence", facts.VulnerabilityIntelligenceFactKinds, facts.VulnerabilityIntelligenceSchemaVersion},
		{"vulnerability_suppression", facts.VulnerabilitySuppressionFactKinds, facts.VulnerabilitySuppressionSchemaVersion},
		{"work_item", facts.WorkItemFactKinds, facts.WorkItemSchemaVersion},
	}
}

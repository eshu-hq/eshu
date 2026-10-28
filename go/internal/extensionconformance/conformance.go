// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	conformance "github.com/eshu-hq/eshu-sdk/collector/conformance"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The host wrapper re-exports the public collector conformance report contract
// so existing CLI and runtime callers keep a single import path while the
// verdict logic lives once in the public SDK module.
type (
	// Mode selects the conformance proof mode.
	Mode = conformance.Mode
	// Status is the overall conformance result.
	Status = conformance.Status
	// FindingCode identifies one stable conformance failure class.
	FindingCode = conformance.FindingCode
	// Report is the stable conformance result returned to CLIs and automation.
	Report = conformance.Report
	// Finding describes one conformance failure or blocker.
	Finding = conformance.Finding
	// Summary aggregates accepted fixture evidence.
	Summary = conformance.Summary
)

const (
	// SchemaVersion is the stable JSON report schema emitted by the harness.
	SchemaVersion = conformance.SchemaVersion
	// ModeFixture validates local SDK result fixtures only.
	ModeFixture = conformance.ModeFixture
	// ModeCompose reserves a Compose-backed proof mode.
	ModeCompose = conformance.ModeCompose
	// StatusPassed reports that no blocking conformance findings were emitted.
	StatusPassed = conformance.StatusPassed
	// StatusFailed reports a blocking conformance finding or an unevaluable run.
	StatusFailed = conformance.StatusFailed

	// FindingManifestInvalid means the manifest could not be loaded or violated
	// the proof-metadata contract.
	FindingManifestInvalid = conformance.FindingManifestInvalid
	// FindingFixtureRequired means the request did not include result fixtures.
	FindingFixtureRequired = conformance.FindingFixtureRequired
	// FindingFixtureContractFailed means a fixture violates the contract.
	FindingFixtureContractFailed = conformance.FindingFixtureContractFailed
	// FindingMissingReducerConsumer means a declared reducer phase is unavailable.
	FindingMissingReducerConsumer = conformance.FindingMissingReducerConsumer
	// FindingPayloadSchemaInvalid means a fixture payload violated its schema.
	FindingPayloadSchemaInvalid = conformance.FindingPayloadSchemaInvalid
	// FindingUnsupportedMode means the request named an unsupported mode.
	FindingUnsupportedMode = conformance.FindingUnsupportedMode

	// FindingFixtureReadFailed means a fixture file could not be read or decoded
	// as a collector SDK result. It is host-only because the public harness
	// operates on already-decoded fixtures.
	FindingFixtureReadFailed FindingCode = "fixture_read_failed"
)

// Request describes one host-side conformance run that loads a manifest and
// fixture files from disk before delegating to the public SDK harness.
type Request struct {
	ManifestPath string
	FixturePaths []string
	Mode         Mode
	// ComponentHome is the component registry home whose durable producer
	// grants admit core-owned fact kinds the manifest declares. Empty means no
	// grants, so a core-owned declaration fails closed.
	ComponentHome string
}

// Run loads the manifest and fixtures from disk and delegates the verdict to
// the public collector conformance harness. It is read-only: it does not
// install components, claim workflow work, write graph truth, or run Compose
// services. The manifest loads against the producer grants in
// Request.ComponentHome, so a first-party collector that declares granted
// core-owned fact kinds is judged the way install and activation judge it.
func Run(ctx context.Context, req Request) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC()
	manifest, grants, err := component.NewRegistry(req.ComponentHome).LoadGrantedManifestAt(req.ManifestPath, now)
	if err == nil {
		err = validateDeclaredCoreSchemaVersions(manifest)
	}
	if err != nil {
		report := Report{
			SchemaVersion: conformance.SchemaVersion,
			Mode:          normalizeReportMode(req.Mode),
			Status:        conformance.StatusFailed,
			Findings: []Finding{{
				Code:                   FindingManifestInvalid,
				Message:                err.Error(),
				BlocksPublication:      true,
				BlocksHostedActivation: true,
			}},
		}
		return report, conformanceError(report)
	}

	fixtures := make([]sdkcollector.Result, 0, len(req.FixturePaths))
	var readFindings []Finding
	for _, fixturePath := range req.FixturePaths {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		result, readErr := readFixture(fixturePath)
		if readErr != nil {
			readFindings = append(readFindings, Finding{
				Code:                   FindingFixtureReadFailed,
				Message:                readErr.Error(),
				BlocksPublication:      true,
				BlocksHostedActivation: true,
			})
			continue
		}
		fixtures = append(fixtures, result)
	}

	coreFacts := trustedCoreFacts(manifest, grants, now)
	report := conformance.Run(conformance.Request{
		Manifest:          manifestToPublic(manifest),
		Fixtures:          fixtures,
		Mode:              req.Mode,
		CoreFacts:         coreFacts,
		ReservedFactKinds: facts.CoreFactKinds(),
		PayloadSchemas:    payloadSchemasForManifest(manifest, coreFacts),
	})
	if len(readFindings) > 0 {
		report.Findings = append(readFindings, report.Findings...)
		report.Status = conformance.StatusFailed
	}

	if report.Status != conformance.StatusPassed {
		return report, conformanceError(report)
	}
	return report, nil
}

// validateDeclaredCoreSchemaVersions prevents a producer grant from making a
// version pass conformance that core's fact admission would later reject.
func validateDeclaredCoreSchemaVersions(manifest component.Manifest) error {
	for _, fact := range manifest.Spec.EmittedFacts {
		if !facts.IsCoreFactKind(fact.Kind) {
			continue
		}
		for _, version := range fact.SchemaVersions {
			if err := facts.ValidateSchemaVersion(fact.Kind, version); err != nil {
				return err
			}
		}
	}
	return nil
}

// trustedCoreFacts builds exact collector/schema tuples from the same grant
// snapshot and evaluation time used to admit the manifest. A declaration alone
// never creates authorization; each tuple must pass the runtime grant policy.
func trustedCoreFacts(manifest component.Manifest, grants []component.ProducerGrant, now time.Time) *conformance.CoreFactContext {
	var authorizations []conformance.CoreFactAuthorization
	for _, fact := range manifest.Spec.EmittedFacts {
		if !facts.IsCoreFactKind(fact.Kind) {
			continue
		}
		entry, ok := facts.FactKindRegistryEntryFor(fact.Kind)
		if !ok {
			// The SDK rejects missing coverage for a reserved declaration.
			continue
		}
		auth := conformance.CoreFactAuthorization{Kind: fact.Kind, ReducerPhases: []string{entry.ReducerDomain}}
		if entry.PayloadSchema != "" {
			auth.PayloadSchemaRef = entry.Kind
		}
		for _, collectorKind := range manifest.Spec.CollectorKinds {
			emission := conformance.CoreFactEmission{CollectorKind: collectorKind}
			for _, version := range fact.SchemaVersions {
				if component.AuthorizesEmission(grants, manifest.Metadata.ID, manifest.Metadata.Version,
					fact.Kind, version, []string{collectorKind}, now) {
					emission.SchemaVersions = append(emission.SchemaVersions, version)
				}
			}
			if len(emission.SchemaVersions) > 0 {
				auth.Emissions = append(auth.Emissions, emission)
			}
		}
		authorizations = append(authorizations, auth)
	}
	if len(authorizations) == 0 {
		return nil
	}
	return &conformance.CoreFactContext{
		ComponentID:      manifest.Metadata.ID,
		ComponentVersion: manifest.Metadata.Version,
		Facts:            authorizations,
	}
}

func normalizeReportMode(mode Mode) Mode {
	if strings.TrimSpace(string(mode)) == "" {
		return conformance.ModeFixture
	}
	return mode
}

func readFixture(fixturePath string) (sdkcollector.Result, error) {
	raw, err := os.ReadFile(fixturePath) // #nosec G304 -- reads a checked-in conformance fixture file at a path constructed by the program from known fixture directories
	if err != nil {
		return sdkcollector.Result{}, err
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return sdkcollector.Result{}, err
	}
	return result, nil
}

// manifestToPublic maps the internal component manifest onto the portable
// conformance manifest the public harness validates.
func manifestToPublic(manifest component.Manifest) conformance.Manifest {
	facts := make([]conformance.FactFamily, 0, len(manifest.Spec.EmittedFacts))
	for _, fact := range manifest.Spec.EmittedFacts {
		facts = append(facts, conformance.FactFamily{
			Kind:             fact.Kind,
			PayloadSchemaRef: fact.PayloadSchemaRef,
			SchemaVersions:   append([]string(nil), fact.SchemaVersions...),
			SourceConfidence: append([]string(nil), fact.SourceConfidence...),
		})
	}
	artifacts := make([]conformance.Artifact, 0, len(manifest.Spec.Artifacts))
	for _, artifact := range manifest.Spec.Artifacts {
		artifacts = append(artifacts, conformance.Artifact{
			Platform: artifact.Platform,
			Image:    artifact.Image,
		})
	}
	return conformance.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Metadata: conformance.Metadata{
			ID:        manifest.Metadata.ID,
			Name:      manifest.Metadata.Name,
			Publisher: manifest.Metadata.Publisher,
			Version:   manifest.Metadata.Version,
		},
		Spec: conformance.Spec{
			CompatibleCore: manifest.Spec.CompatibleCore,
			ComponentType:  manifest.Spec.ComponentType,
			CollectorKinds: append([]string(nil), manifest.Spec.CollectorKinds...),
			Runtime: conformance.RuntimeContract{
				SDKProtocol: manifest.Spec.Runtime.SDKProtocol,
				Adapter:     manifest.Spec.Runtime.Adapter,
			},
			Artifacts:    artifacts,
			EmittedFacts: facts,
			ConsumerContracts: conformance.ConsumerContracts{
				Reducer: conformance.ReducerContract{
					Phases: append([]string(nil), manifest.Spec.ConsumerContracts.Reducer.Phases...),
				},
			},
			Telemetry: conformance.Telemetry{MetricsPrefix: manifest.Spec.Telemetry.MetricsPrefix},
		},
	}
}

func payloadSchemasForManifest(manifest component.Manifest, coreFacts *conformance.CoreFactContext) map[string]json.RawMessage {
	var schemas map[string]json.RawMessage
	trustedRefs := make(map[string]string)
	if coreFacts != nil {
		for _, auth := range coreFacts.Facts {
			trustedRefs[auth.Kind] = auth.PayloadSchemaRef
		}
	}
	for _, fact := range manifest.Spec.EmittedFacts {
		ref := fact.PayloadSchemaRef
		if trustedRefs[fact.Kind] != "" {
			ref = trustedRefs[fact.Kind]
		}
		if strings.TrimSpace(ref) == "" {
			continue
		}
		raw, ok := fixturepack.SchemaFor(ref)
		if !ok {
			continue
		}
		if schemas == nil {
			schemas = make(map[string]json.RawMessage)
		}
		schemas[fact.Kind] = append(json.RawMessage(nil), raw...)
	}
	return schemas
}

func conformanceError(report Report) error {
	if len(report.Findings) == 0 {
		return fmt.Errorf("fixture conformance failed")
	}
	return fmt.Errorf("fixture conformance failed: %s", report.Findings[0].Code)
}

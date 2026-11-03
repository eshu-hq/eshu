// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package satellite

import (
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

const (
	// ComponentID is the satellite's component identity. It matches the
	// producer ID the fact registry authorizes for the AWS core-owned
	// kinds, so producer grants naming this ID admit the manifest.
	ComponentID = "collector-aws-cloud"
	// Version is the first satellite component manifest version.
	Version = "0.1.0"

	publisher      = "eshu-hq"
	compatibleCore = ">=0.0.5 <0.2.0"
	artifactImage  = "linux/amd64"
	metricsPrefix  = "eshu_collector_aws_cloud_"
)

// emittedKinds lists every fact kind the satellite can emit, in registry
// emission order: the nine aws-family kinds first, then the posture and
// inventory families. The host rejects undeclared kinds at admission, so
// this list must cover the whole satellite surface, not only the kinds the
// pinned SDK fixture pack ships schemas for.
func emittedKinds() []string {
	kinds := append([]string{}, cloud.AWSFactKinds()...)
	kinds = append(kinds, cloud.EC2InstancePostureFactKinds()...)
	kinds = append(kinds, cloud.S3BucketPostureFactKinds()...)
	kinds = append(kinds, cloud.RDSPostureFactKinds()...)
	kinds = append(kinds, cloud.S3ExternalPrincipalGrantFactKinds()...)
	kinds = append(kinds, cloud.SSMHostFactKinds()...)
	return kinds
}

// schemaVersion resolves the registry schema version for one emitted kind.
// An unresolvable kind fails closed: the manifest must never declare a
// version the fact admission path would reject.
func schemaVersion(kind string) (string, error) {
	resolvers := []func(string) (string, bool){
		cloud.AWSSchemaVersion,
		cloud.EC2InstancePostureSchemaVersion,
		cloud.S3BucketPostureSchemaVersion,
		cloud.RDSPostureSchemaVersion,
		cloud.S3ExternalPrincipalGrantSchemaVersion,
		cloud.SSMHostSchemaVersion,
	}
	for _, resolve := range resolvers {
		if version, ok := resolve(kind); ok {
			return version, nil
		}
	}
	return "", fmt.Errorf("aws satellite has no schema version for fact kind %q", kind)
}

// payloadSchemaRef returns the kind-keyed payload schema ref when the
// shipped fixture pack carries a schema for the kind, and empty otherwise.
// The component contract rejects a ref the pack does not ship, so kinds
// without pack coverage (aws_tag_observation, the aws_ssm_host pair) ride
// without one until the pack gains them.
func payloadSchemaRef(kind string) string {
	if _, ok := fixturepack.SchemaFor(kind); ok {
		return kind
	}
	return ""
}

// reducerPhases derives the manifest's reducer-phase contract from the
// fact registry: the deduplicated, sorted reducer domains of the emitted
// kinds. The conformance harness compares this set against the trusted
// grant-derived phases, so both sides stay equal by construction.
func reducerPhases(kinds []string) ([]string, error) {
	seen := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		entry, ok := facts.FactKindRegistryEntryFor(kind)
		if !ok {
			return nil, fmt.Errorf("aws satellite fact kind %q has no registry entry", kind)
		}
		seen[entry.ReducerDomain] = struct{}{}
	}
	phases := make([]string, 0, len(seen))
	for phase := range seen {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	return phases, nil
}

// ManifestFor builds the typed component manifest for the AWS satellite.
// The image ref is publish-owned (the satellite repository publishes the
// image; this port never mints one) and must be digest-pinned: an
// unpinned ref fails closed before it can become admission truth.
//
// The manifest declares the full emitted-kind inventory including the
// aws_ssm_host pair, even though the pinned SDK fixture pack ships no
// payload schemas for those kinds yet. Fixture conformance therefore
// reports exactly that pack gap and nothing else; install, verify, and
// activation admission cover all fifteen kinds through the required
// producer grants.
func ManifestFor(imageRef string) (component.Manifest, error) {
	artifact := component.Artifact{Platform: artifactImage, Image: imageRef}
	if err := artifact.Validate(); err != nil {
		return component.Manifest{}, fmt.Errorf("aws satellite artifact: %w", err)
	}
	kinds := emittedKinds()
	families := make([]component.FactFamily, 0, len(kinds))
	for _, kind := range kinds {
		version, err := schemaVersion(kind)
		if err != nil {
			return component.Manifest{}, err
		}
		families = append(families, component.FactFamily{
			Kind:             kind,
			PayloadSchemaRef: payloadSchemaRef(kind),
			SchemaVersions:   []string{version},
			SourceConfidence: []string{facts.SourceConfidenceReported},
		})
	}
	phases, err := reducerPhases(kinds)
	if err != nil {
		return component.Manifest{}, err
	}
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        ComponentID,
			Name:      "AWS cloud collector",
			Publisher: publisher,
			Version:   Version,
		},
		Spec: component.Spec{
			CompatibleCore: compatibleCore,
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{awscloud.CollectorKind},
			Runtime: component.RuntimeContract{
				SDKProtocol: component.CollectorSDKProtocolV1Alpha1,
				Adapter:     component.RuntimeAdapterOCI,
			},
			Artifacts:    []component.Artifact{artifact},
			EmittedFacts: families,
			ConsumerContracts: component.ConsumerContracts{
				Reducer: component.ReducerContract{Phases: phases},
			},
			Telemetry: component.Telemetry{MetricsPrefix: metricsPrefix},
		},
	}, nil
}

// RequiredGrants returns the live producer grants one manifest needs: one
// grant per declared fact family, pinned to the manifest id and version
// and scoped to the satellite collector kind. Production activations
// issue these through `eshu component grant`, which assigns the bounded
// lifetime activation policy chooses (there are no non-expiring grants,
// and expired grants deny admission fail-closed); the constructor defines
// the required SET the host admission enforces, with a far-future expiry
// so fixtures stay live. Never mint production grants from this
// constructor output: set lifetimes and renew through the activation
// grant flow.
func RequiredGrants(manifest component.Manifest) []component.ProducerGrant {
	grants := make([]component.ProducerGrant, 0, len(manifest.Spec.EmittedFacts))
	for _, family := range manifest.Spec.EmittedFacts {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     manifest.Metadata.ID,
			Version:        manifest.Metadata.Version,
			Kind:           family.Kind,
			SchemaVersions: append([]string(nil), family.SchemaVersions...),
			Scope:          awscloud.CollectorKind,
			ExpiresAt:      time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	return grants
}

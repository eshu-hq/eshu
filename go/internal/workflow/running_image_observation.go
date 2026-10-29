// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"strings"
	"unicode"
)

// RunningImageProducerClass identifies the typed provider observation source.
// The database authenticates the producer principal separately from this value.
type RunningImageProducerClass string

const (
	// RunningImageProducerKubernetesPod describes one observed Pod container.
	RunningImageProducerKubernetesPod RunningImageProducerClass = "kubernetes_pod"
	// RunningImageProducerECSTask describes one observed ECS task container.
	RunningImageProducerECSTask RunningImageProducerClass = "ecs_task"
	// RunningImageProducerLambdaFunction describes one observed Lambda function image.
	RunningImageProducerLambdaFunction RunningImageProducerClass = "lambda_function"
)

// RunningImageObservationClass distinguishes a running container from a
// deployed package. Server policy authorizes these classes independently.
type RunningImageObservationClass string

const (
	// RunningImageClassRunningContainer reports a provider's container state.
	RunningImageClassRunningContainer RunningImageObservationClass = "running_container"
	// RunningImageClassActiveDeployment reports an active deployed image package.
	RunningImageClassActiveDeployment RunningImageObservationClass = "active_deployment"
)

// RunningImageDigestKind says what the provider's reported digest identifies.
// Unknown is retained at intake, but it cannot become a scanner target until
// an immutable manifest relation is independently verified.
type RunningImageDigestKind string

const (
	// RunningImageDigestUnknown means the provider did not prove digest kind.
	RunningImageDigestUnknown RunningImageDigestKind = "unknown"
	// RunningImageDigestManifest identifies an OCI image manifest digest.
	RunningImageDigestManifest RunningImageDigestKind = "manifest"
	// RunningImageDigestConfig identifies an OCI image config digest.
	RunningImageDigestConfig RunningImageDigestKind = "config"
)

// RunningImageObservation is the nonsecret typed intake shape for one object
// slot. Its fields are provider claims, never authorization witnesses: only a
// DB-authenticated producer, collection fence and published source generation
// can make an observation eligible for scanner admission.
type RunningImageObservation struct {
	ProducerClass         RunningImageProducerClass
	ObservationClass      RunningImageObservationClass
	ProducerInstanceID    string
	SourceScopeID         string
	SourceGenerationID    string
	ObjectID              string
	SlotID                string
	ObservedRevision      string
	Registry              string
	Repository            string
	RuntimeImageReference string
	RuntimeDigest         string
	RuntimeDigestKind     RunningImageDigestKind
	// LastUpdateStatus retains Lambda update uncertainty for protected audit.
	// It never turns a deployed package into an observed execution.
	LastUpdateStatus string
}

// Validate checks only the intake shape. It does not validate provider state,
// producer identity, source scope, collection completeness or current grants.
func (observation RunningImageObservation) Validate() error {
	for name, value := range map[string]string{
		"producer_instance_id": observation.ProducerInstanceID,
		"source_scope_id":      observation.SourceScopeID,
		"source_generation_id": observation.SourceGenerationID,
		"object_id":            observation.ObjectID,
		"slot_id":              observation.SlotID,
		"registry":             observation.Registry,
		"repository":           observation.Repository,
		"runtime_image_ref":    observation.RuntimeImageReference,
	} {
		if value == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("running image observation %s must be a nonempty canonical value", name)
		}
		if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return fmt.Errorf("running image observation %s contains whitespace or control characters", name)
		}
		if len(value) > 4096 {
			return fmt.Errorf("running image observation %s exceeds 4096 bytes", name)
		}
	}
	if len(observation.ObservedRevision) > 4096 || len(observation.LastUpdateStatus) > 64 {
		return fmt.Errorf("running image observation context exceeds its byte limit")
	}
	if strings.ContainsAny(observation.Registry+observation.Repository, "@?#") ||
		strings.Contains(observation.Registry, "/") ||
		strings.HasPrefix(observation.Repository, "/") ||
		strings.HasSuffix(observation.Repository, "/") ||
		strings.Contains(observation.Repository, "//") ||
		strings.ContainsAny(observation.RuntimeImageReference, "?#") {
		return fmt.Errorf("running image observation has a noncanonical locator")
	}
	fullRepository := observation.Registry + "/" + observation.Repository
	if !strings.HasPrefix(observation.RuntimeImageReference, fullRepository+":") &&
		!strings.HasPrefix(observation.RuntimeImageReference, fullRepository+"@") {
		return fmt.Errorf("running image observation reference differs from registry and repository")
	}
	if err := validateScannerSHA256(observation.RuntimeDigest); err != nil {
		return fmt.Errorf("running image observation digest: %w", err)
	}
	switch observation.RuntimeDigestKind {
	case RunningImageDigestUnknown, RunningImageDigestManifest, RunningImageDigestConfig:
	default:
		return fmt.Errorf("running image observation digest kind %q is unsupported", observation.RuntimeDigestKind)
	}
	switch observation.ProducerClass {
	case RunningImageProducerKubernetesPod, RunningImageProducerECSTask:
		if observation.ObservationClass != RunningImageClassRunningContainer {
			return fmt.Errorf("running image producer %q requires running_container class", observation.ProducerClass)
		}
	case RunningImageProducerLambdaFunction:
		if observation.ObservationClass != RunningImageClassActiveDeployment ||
			observation.ObservedRevision == "" ||
			observation.ObservedRevision != strings.TrimSpace(observation.ObservedRevision) {
			return fmt.Errorf("lambda image observation requires active_deployment class and observed revision")
		}
	default:
		return fmt.Errorf("running image producer class %q is unsupported", observation.ProducerClass)
	}
	return nil
}

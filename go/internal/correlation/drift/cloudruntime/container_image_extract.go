// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import (
	"encoding/json"
	"strings"
)

// MaxContainerImagesPerResource bounds how many distinct container image
// references ExtractDeclaredContainerImages and ExtractObservedContainerImages
// return for one ECS task-definition resource. This is defense in depth on
// top of realistic AWS ECS task-definition container counts: even a
// malformed or adversarial container_definitions document can never grow the
// declared or observed image set past this bound, so a single ECS resource
// can never inflate the value-drift evidence surface unboundedly (#5453).
const MaxContainerImagesPerResource = 8

// ContainerImageExtractionResult is the bounded output of a container-image
// extraction. Images is the deduplicated, source-ordered, capped list of
// image references. Truncated reports whether the source carried more
// distinct images than MaxContainerImagesPerResource; callers should surface
// this as an operator-facing warning rather than silently dropping the
// excess containers.
//
// This struct intentionally carries no field beyond Images and the two
// bounded booleans -- see TestExtractDeclaredContainerImagesNeverLeaksNonImageFields,
// which fails the build's own review contract if a future edit adds a field
// capable of holding source content. Every other property of a container
// definition (environment, secrets, logConfiguration, command, entryPoint,
// portMappings, ...) must never reach this type.
type ContainerImageExtractionResult struct {
	// Images is the bounded, deduplicated list of container image
	// references, in first-seen source order.
	Images []string
	// Truncated is true when the source container list carried more
	// distinct images than MaxContainerImagesPerResource.
	Truncated bool
	// Degraded is true when the source carried a container-definitions value
	// that could NOT be read: a non-string container_definitions (a redaction
	// marker object, for instance), a non-slice observed containers value, or
	// a container_definitions string that failed to parse as JSON.
	//
	// It is not the same as absent. An absent value (nil, or a blank string)
	// leaves Degraded false, because there is genuinely nothing to compare.
	// A degraded value means evidence EXISTS and we could not use it, which is
	// what an operator has to fix. Both yield an empty Images, so without this
	// flag the two are indistinguishable downstream (#5837).
	Degraded bool
}

// declaredContainerDefinition is the ONLY shape ExtractDeclaredContainerImages
// ever decodes a container_definitions element into. json.Unmarshal silently
// discards every JSON object key that has no matching struct field, so
// "environment", "secrets", "logConfiguration", "command", "entryPoint", and
// any other container_definitions field never survive the decode -- they are
// never allocated into a Go value this package can accidentally propagate.
// This is the security bound for the Terraform-declared side (#5453): the
// raw container_definitions blob itself is never retained or returned.
type declaredContainerDefinition struct {
	Image string `json:"image"`
}

// ExtractDeclaredContainerImages parses a Terraform aws_ecs_task_definition
// container_definitions attribute value -- a JSON-encoded STRING holding an
// array of container definition objects -- and extracts ONLY the "image"
// field of each container, bounded at MaxContainerImagesPerResource.
//
// containerDefinitions must be the raw attribute value as decoded from the
// terraform_state_resource payload's attributes map (a string; the collector
// never emits container_definitions pre-parsed). Malformed JSON, a non-array
// top-level value, a non-string Go type, or a nil input all yield an empty
// result rather than an error -- the caller treats missing declared evidence
// as "no signal", never as a value mismatch.
//
// The empty results are not all the same, though, and the Degraded flag is
// the difference. A nil or blank input is genuinely ABSENT. A non-string value
// -- which is exactly what the terraform-state collector's fail-closed
// redaction produces when its provider-schema resolver is nil or a schema
// bundle failed to parse, replacing the scalar with a marker OBJECT -- is
// DEGRADED, as is a string that will not parse. Reporting those as absent is
// what let a value comparison collapse into a false convergence (#5837).
func ExtractDeclaredContainerImages(containerDefinitions any) ContainerImageExtractionResult {
	raw, ok := containerDefinitions.(string)
	if !ok {
		return ContainerImageExtractionResult{Degraded: containerDefinitions != nil}
	}
	if strings.TrimSpace(raw) == "" {
		return ContainerImageExtractionResult{}
	}
	var parsed []declaredContainerDefinition
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ContainerImageExtractionResult{Degraded: true}
	}
	images := make([]string, 0, len(parsed))
	for _, container := range parsed {
		images = append(images, container.Image)
	}
	return boundedContainerImages(images)
}

// ExtractObservedContainerImages parses the AWS-observed "containers"
// attribute from an aws_resource ecs.task or ecs.task_definition payload --
// a decoded []any of per-container maps -- and extracts ONLY the "image"
// field of each container, bounded at MaxContainerImagesPerResource.
//
// The AWS collector's ecs.task_definition containers shape carries
// "environment" (redacted) and "secrets" (name/valueFrom references) fields
// alongside "image" (see
// go/internal/collector/awscloud/services/ecs/scanner.go containerMaps);
// this function reads only the "image" key off each element and never
// touches any other key, so those fields never reach the drift evidence
// surface. A non-[]any input, a non-map element, or a missing/blank "image"
// key is skipped rather than erroring. A non-nil value of any other Go type is
// reported as Degraded, on the same absent-versus-unreadable distinction
// ExtractDeclaredContainerImages documents (#5837).
func ExtractObservedContainerImages(containers any) ContainerImageExtractionResult {
	list, ok := containers.([]any)
	if !ok {
		return ContainerImageExtractionResult{Degraded: containers != nil}
	}
	images := make([]string, 0, len(list))
	for _, item := range list {
		container, ok := item.(map[string]any)
		if !ok {
			continue
		}
		image, _ := container["image"].(string)
		images = append(images, image)
	}
	return boundedContainerImages(images)
}

// boundedContainerImages trims blank entries, deduplicates by exact string
// match while preserving first-seen order, and caps the result at
// MaxContainerImagesPerResource, flagging Truncated when the source carried
// more distinct non-blank images than the bound allows.
func boundedContainerImages(images []string) ContainerImageExtractionResult {
	seen := make(map[string]struct{}, len(images))
	out := make([]string, 0, len(images))
	truncated := false
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, exists := seen[image]; exists {
			continue
		}
		if len(out) >= MaxContainerImagesPerResource {
			truncated = true
			break
		}
		seen[image] = struct{}{}
		out = append(out, image)
	}
	if len(out) == 0 {
		out = nil
	}
	// Degraded stays false here: this path was reached with a value that
	// parsed, so anything missing from it was genuinely absent from the source.
	return ContainerImageExtractionResult{Images: out, Truncated: truncated}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import "strings"

// backstageEntity is the typed projection of one Backstage catalog descriptor
// document. Only the fields the producer maps into facts are modeled.
type backstageEntity struct {
	APIVersion string                  `yaml:"apiVersion"`
	Kind       string                  `yaml:"kind"`
	Metadata   backstageEntityMetadata `yaml:"metadata"`
	Spec       backstageEntitySpec     `yaml:"spec"`
}

type backstageEntityMetadata struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace"`
	Title       string            `yaml:"title"`
	Description string            `yaml:"description"`
	Annotations map[string]string `yaml:"annotations"`
	Tags        []string          `yaml:"tags"`
	Links       []backstageLink   `yaml:"links"`
}

type backstageEntitySpec struct {
	Type      string   `yaml:"type"`
	Lifecycle string   `yaml:"lifecycle"`
	Owner     string   `yaml:"owner"`
	System    string   `yaml:"system"`
	DependsOn []string `yaml:"dependsOn"`
}

type backstageLink struct {
	URL   string `yaml:"url"`
	Title string `yaml:"title"`
	Type  string `yaml:"type"`
}

const (
	// backstageSourceLocationAnnotation declares a component's source repository.
	backstageSourceLocationAnnotation = "backstage.io/source-location"
	// backstageProjectSlugAnnotation declares a provider repository slug.
	backstageProjectSlugAnnotation = "github.com/project-slug"
	// backstageGenericProjectSlugAnnotation is the provider-neutral slug form.
	backstageGenericProjectSlugAnnotation = "backstage.io/project-slug"
)

// supportedBackstageAPIVersions enumerates the descriptor versions the producer
// fully understands. Other versions still emit a minimally-parsed entity plus an
// unsupported_descriptor_version warning rather than a silent drop.
var supportedBackstageAPIVersions = map[string]bool{
	"backstage.io/v1alpha1": true,
	"backstage.io/v1beta1":  true,
}

// defaultNamespace is Backstage's implicit entity namespace.
const defaultNamespace = "default"

// entityRef returns the canonical `kind:namespace/name` reference for one
// Backstage entity, or an empty string when name is missing.
func (e backstageEntity) entityRef() string {
	name := strings.TrimSpace(e.Metadata.Name)
	if name == "" {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(e.Kind))
	if kind == "" {
		kind = "component"
	}
	namespace := strings.ToLower(strings.TrimSpace(e.Metadata.Namespace))
	if namespace == "" {
		namespace = defaultNamespace
	}
	return kind + ":" + namespace + "/" + strings.ToLower(name)
}

// ownerRef returns the Backstage owner reference verbatim, preserving any
// `kind:namespace/name` provenance (for example `group:default/payments`). The
// reducer records the full reference as provenance; collapsing it to a bare
// name would merge distinct owners such as `group:default/team-x` and
// `user:default/team-x` and break owner filters and reporting.
func (e backstageEntity) ownerRef() string {
	return strings.TrimSpace(e.Spec.Owner)
}

// repositoryURL returns the declared source-repository URL from the
// source-location annotation. Backstage prefixes locations with a `url:` target
// type, which is stripped. Only credential-free, query-free URLs are returned.
func (e backstageEntity) repositoryURL() string {
	raw := strings.TrimSpace(e.Metadata.Annotations[backstageSourceLocationAnnotation])
	if raw == "" {
		return ""
	}
	raw = strings.TrimPrefix(raw, "url:")
	// A source-location can point at a tree path; trim to the repo root marker.
	if idx := strings.Index(raw, "/tree/"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimSpace(raw)
	if !isSafeURL(raw) {
		return ""
	}
	return raw
}

// repositoryName returns a name-only repository locator declared via a slug
// annotation that carries no host. A bare slug cannot prove ownership, so it is
// emitted as repository_name and the reducer rejects it.
func (e backstageEntity) repositoryName() string {
	for _, key := range []string{backstageProjectSlugAnnotation, backstageGenericProjectSlugAnnotation} {
		slug := strings.TrimSpace(e.Metadata.Annotations[key])
		if slug != "" {
			return slug
		}
	}
	return ""
}

// tier extracts a tier label from `tier-N` tags, matching common Backstage tag
// conventions. It returns the first matching tag's full label.
func (e backstageEntity) tier() string {
	for _, tag := range e.Metadata.Tags {
		tag = strings.TrimSpace(tag)
		if strings.HasPrefix(tag, "tier-") {
			return tag
		}
	}
	return ""
}

// dependencies returns the declared dependency entity references.
func (e backstageEntity) dependencies() []string {
	out := make([]string, 0, len(e.Spec.DependsOn))
	for _, dep := range e.Spec.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep != "" {
			out = append(out, dep)
		}
	}
	return out
}

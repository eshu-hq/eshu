package ociregistry

import "time"

// CollectorKind is the durable collector family name for OCI registry facts.
const CollectorKind = "oci_registry"

// Provider identifies the registry provider adapter that reported evidence.
type Provider string

const (
	// ProviderDockerHub identifies Docker Hub registry repositories.
	ProviderDockerHub Provider = "dockerhub"
	// ProviderECR identifies Amazon Elastic Container Registry.
	ProviderECR Provider = "ecr"
	// ProviderGHCR identifies GitHub Container Registry repositories.
	ProviderGHCR Provider = "ghcr"
	// ProviderJFrog identifies JFrog Artifactory Docker/OCI repositories.
	ProviderJFrog Provider = "jfrog"
)

// Visibility describes source-reported repository visibility.
type Visibility string

const (
	// VisibilityUnknown marks registries that did not report visibility.
	VisibilityUnknown Visibility = "unknown"
	// VisibilityPublic marks public repositories.
	VisibilityPublic Visibility = "public"
	// VisibilityPrivate marks private repositories.
	VisibilityPrivate Visibility = "private"
)

// AuthMode describes whether the repository was observed anonymously or with
// credentials.
type AuthMode string

const (
	// AuthModeUnknown marks registries that did not report auth mode.
	AuthModeUnknown AuthMode = "unknown"
	// AuthModeAnonymous marks anonymous observations.
	AuthModeAnonymous AuthMode = "anonymous"
	// AuthModeCredentialed marks credential-backed observations.
	AuthModeCredentialed AuthMode = "credentialed"
)

const (
	// MediaTypeOCIImageManifest is the OCI image manifest media type.
	MediaTypeOCIImageManifest = "application/vnd.oci.image.manifest.v1+json"
	// MediaTypeOCIImageIndex is the OCI image index media type.
	MediaTypeOCIImageIndex = "application/vnd.oci.image.index.v1+json"
	// MediaTypeDockerImageManifest is the Docker schema 2 image manifest media
	// type accepted by OCI Distribution-compatible registries.
	MediaTypeDockerImageManifest = "application/vnd.docker.distribution.manifest.v2+json"
	// MediaTypeDockerManifestList is the Docker schema 2 manifest-list media
	// type accepted by OCI Distribution-compatible registries.
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	// IdentityStrengthWeakTag marks tag evidence as mutable and weak.
	IdentityStrengthWeakTag = "weak_tag"
	// RedactedValue replaces annotation values that are not allowlisted.
	RedactedValue = "[redacted]"
	// WarningUnsupportedReferrersAPI marks registries without Referrers API
	// support.
	WarningUnsupportedReferrersAPI = "unsupported_referrers_api"
	// WarningComputedManifestDigest marks registries that returned manifest
	// bytes without a Docker-Content-Digest header.
	WarningComputedManifestDigest = "computed_manifest_digest"
	// SeverityInfo marks informational warnings.
	SeverityInfo = "info"
	// ReferrersUnsupported records unsupported Referrers API state.
	ReferrersUnsupported = "unsupported"
)

// RepositoryIdentity is the raw registry/repository tuple observed from a
// provider.
type RepositoryIdentity struct {
	Provider   Provider
	Registry   string
	Repository string
}

// NormalizedRepositoryIdentity is the stable repository identity used in OCI
// registry fact keys and scope IDs.
type NormalizedRepositoryIdentity struct {
	Provider     Provider
	Registry     string
	Repository   string
	RepositoryID string
	ScopeID      string
}

// DescriptorIdentity is the raw digest identity for an OCI descriptor.
type DescriptorIdentity struct {
	Repository RepositoryIdentity
	Digest     string
	MediaType  string
}

// NormalizedDescriptorIdentity is the stable descriptor identity.
type NormalizedDescriptorIdentity struct {
	Repository   NormalizedRepositoryIdentity
	Digest       string
	MediaType    string
	DescriptorID string
}

// Platform describes a platform-specific manifest entry from an image index.
type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

// Descriptor is a digest-addressed OCI descriptor.
type Descriptor struct {
	Digest       string
	MediaType    string
	SizeBytes    int64
	ArtifactType string
	Annotations  map[string]string
	Platform     Platform
}

// RepositoryObservation is one repository ready for envelope emission.
type RepositoryObservation struct {
	Identity            RepositoryIdentity
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	Visibility          Visibility
	AuthMode            AuthMode
	SourceURI           string
}

// TagObservation is one mutable tag-to-digest observation.
type TagObservation struct {
	Repository          RepositoryIdentity
	Tag                 string
	Digest              string
	MediaType           string
	PreviousDigest      string
	Mutated             bool
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// ManifestObservation is one digest-addressed image manifest observation.
type ManifestObservation struct {
	Repository          RepositoryIdentity
	Descriptor          Descriptor
	Config              Descriptor
	Layers              []Descriptor
	SourceTag           string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// IndexObservation is one digest-addressed image index observation.
type IndexObservation struct {
	Repository          RepositoryIdentity
	Descriptor          Descriptor
	Manifests           []Descriptor
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// DescriptorObservation is one reusable descriptor observation.
type DescriptorObservation struct {
	Repository          RepositoryIdentity
	Descriptor          Descriptor
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// ReferrerObservation is one descriptor reported as a referrer for a subject
// digest.
type ReferrerObservation struct {
	Repository          RepositoryIdentity
	Subject             Descriptor
	Referrer            Descriptor
	SourceAPIPath       string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// WarningObservation is one non-fatal OCI registry warning.
type WarningObservation struct {
	WarningKey          string
	WarningCode         string
	Severity            string
	Message             string
	Repository          *RepositoryIdentity
	Digest              string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

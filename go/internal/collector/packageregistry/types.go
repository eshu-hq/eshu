package packageregistry

import "time"

// CollectorKind is the durable collector family name for package-registry facts.
const CollectorKind = "package_registry"

// Ecosystem identifies the package-native contract used to normalize identity.
type Ecosystem string

const (
	// EcosystemNPM identifies npm package metadata.
	EcosystemNPM Ecosystem = "npm"
	// EcosystemPyPI identifies Python package metadata.
	EcosystemPyPI Ecosystem = "pypi"
	// EcosystemGoModule identifies Go module proxy metadata.
	EcosystemGoModule Ecosystem = "gomod"
	// EcosystemMaven identifies Maven repository metadata.
	EcosystemMaven Ecosystem = "maven"
	// EcosystemNuGet identifies NuGet V3 feed metadata.
	EcosystemNuGet Ecosystem = "nuget"
	// EcosystemGeneric identifies provider-specific generic package metadata.
	EcosystemGeneric Ecosystem = "generic"
)

// Visibility describes source-reported package visibility when a registry
// exposes it.
type Visibility string

const (
	// VisibilityUnknown marks registries that did not report visibility.
	VisibilityUnknown Visibility = "unknown"
	// VisibilityPublic marks a package/feed visible without private credentials.
	VisibilityPublic Visibility = "public"
	// VisibilityPrivate marks a package/feed that requires private access.
	VisibilityPrivate Visibility = "private"
)

// PackageIdentity is the raw identity tuple observed from a registry or feed.
type PackageIdentity struct {
	Ecosystem  Ecosystem
	Registry   string
	RawName    string
	Namespace  string
	Classifier string
}

// NormalizedPackageIdentity is the feed-aware identity used for stable package
// fact keys.
type NormalizedPackageIdentity struct {
	Ecosystem      Ecosystem
	Registry       string
	RawName        string
	NormalizedName string
	Namespace      string
	Classifier     string
	PackageID      string
}

// PackageObservation is one observed package identity ready for envelope
// emission.
type PackageObservation struct {
	Identity            PackageIdentity
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	Visibility          Visibility
	SourceURI           string
}

// PackageVersionObservation is one observed package version ready for envelope
// emission.
type PackageVersionObservation struct {
	Package             PackageIdentity
	Version             string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	PublishedAt         time.Time
	Yanked              bool
	Unlisted            bool
	Deprecated          bool
	Retracted           bool
	ArtifactURLs        []string
	Checksums           map[string]string
	SourceURI           string
}

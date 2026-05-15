package projector

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// PackageRegistryPackageRow carries one stable package identity for canonical
// graph projection. Source hints are intentionally not represented here because
// registry metadata alone is provenance, not repository ownership truth.
type PackageRegistryPackageRow struct {
	UID                 string
	Ecosystem           string
	Registry            string
	RawName             string
	NormalizedName      string
	Namespace           string
	Classifier          string
	Visibility          string
	SourceFactID        string
	StableFactKey       string
	SourceSystem        string
	SourceRecordID      string
	SourceConfidence    string
	CollectorKind       string
	CorrelationAnchors  []string
	CollectorInstanceID string
	ObservedAt          time.Time
}

// PackageRegistryVersionRow carries one stable package version identity for
// canonical graph projection.
type PackageRegistryVersionRow struct {
	UID                 string
	PackageID           string
	Ecosystem           string
	Registry            string
	Version             string
	PublishedAt         time.Time
	IsYanked            bool
	IsUnlisted          bool
	IsDeprecated        bool
	IsRetracted         bool
	ArtifactURLs        []string
	Checksums           map[string]string
	SourceFactID        string
	StableFactKey       string
	SourceSystem        string
	SourceRecordID      string
	SourceConfidence    string
	CollectorKind       string
	CorrelationAnchors  []string
	CollectorInstanceID string
	ObservedAt          time.Time
}

// PackageRegistryDependencyRow carries one package-native dependency edge with
// ecosystem-specific scope, type, marker, and optional/exclusion semantics.
type PackageRegistryDependencyRow struct {
	UID                  string
	PackageID            string
	VersionID            string
	Version              string
	DependencyPackageID  string
	DependencyEcosystem  string
	DependencyRegistry   string
	DependencyNamespace  string
	DependencyNormalized string
	DependencyRange      string
	DependencyType       string
	TargetFramework      string
	Marker               string
	Optional             bool
	Excluded             bool
	SourceFactID         string
	StableFactKey        string
	SourceSystem         string
	SourceRecordID       string
	SourceConfidence     string
	CollectorKind        string
	CorrelationAnchors   []string
	CollectorInstanceID  string
	ObservedAt           time.Time
}

func extractPackageRegistryRows(mat *CanonicalMaterialization, envelopes []facts.Envelope) {
	if mat == nil || len(envelopes) == 0 {
		return
	}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.PackageRegistryPackageFactKind:
			if row, ok := packageRegistryPackageRow(envelope); ok {
				mat.PackageRegistryPackages = append(mat.PackageRegistryPackages, row)
			}
		case facts.PackageRegistryPackageVersionFactKind:
			if row, ok := packageRegistryVersionRow(envelope); ok {
				mat.PackageRegistryVersions = append(mat.PackageRegistryVersions, row)
			}
		case facts.PackageRegistryPackageDependencyFactKind:
			if row, ok := packageRegistryDependencyRow(envelope); ok {
				mat.PackageRegistryDependencies = append(mat.PackageRegistryDependencies, row)
			}
		}
	}
}

func validatePackageRegistrySchemaVersion(envelope facts.Envelope) error {
	want, ok := facts.PackageRegistrySchemaVersion(envelope.FactKind)
	if !ok {
		return nil
	}
	got := strings.TrimSpace(envelope.SchemaVersion)
	if got == "" {
		return fmt.Errorf("package registry fact %q schema_version must not be blank", envelope.FactID)
	}
	if got != want {
		return fmt.Errorf(
			"package registry fact %q schema_version %q is unsupported for %s; want %q",
			envelope.FactID,
			got,
			envelope.FactKind,
			want,
		)
	}
	return nil
}

func packageRegistryPackageRow(envelope facts.Envelope) (PackageRegistryPackageRow, bool) {
	if envelope.IsTombstone {
		return PackageRegistryPackageRow{}, false
	}
	packageID, _ := payloadString(envelope.Payload, "package_id")
	if packageID == "" {
		return PackageRegistryPackageRow{}, false
	}
	ecosystem, _ := payloadString(envelope.Payload, "ecosystem")
	registry, _ := payloadString(envelope.Payload, "registry")
	rawName, _ := payloadString(envelope.Payload, "raw_name")
	normalizedName, _ := payloadString(envelope.Payload, "normalized_name")
	namespace, _ := payloadString(envelope.Payload, "namespace")
	classifier, _ := payloadString(envelope.Payload, "classifier")
	visibility, _ := payloadString(envelope.Payload, "visibility")
	collectorInstanceID, _ := payloadString(envelope.Payload, "collector_instance_id")
	return PackageRegistryPackageRow{
		UID:                 packageID,
		Ecosystem:           ecosystem,
		Registry:            registry,
		RawName:             rawName,
		NormalizedName:      normalizedName,
		Namespace:           namespace,
		Classifier:          classifier,
		Visibility:          visibility,
		SourceFactID:        envelope.FactID,
		StableFactKey:       envelope.StableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistryCorrelationAnchors(envelope.Payload),
		CollectorInstanceID: collectorInstanceID,
		ObservedAt:          envelope.ObservedAt,
	}, true
}

func packageRegistryVersionRow(envelope facts.Envelope) (PackageRegistryVersionRow, bool) {
	if envelope.IsTombstone {
		return PackageRegistryVersionRow{}, false
	}
	packageID, _ := payloadString(envelope.Payload, "package_id")
	versionID, _ := payloadString(envelope.Payload, "version_id")
	version, _ := payloadString(envelope.Payload, "version")
	if packageID == "" || versionID == "" || version == "" {
		return PackageRegistryVersionRow{}, false
	}
	ecosystem, _ := payloadString(envelope.Payload, "ecosystem")
	registry, _ := payloadString(envelope.Payload, "registry")
	collectorInstanceID, _ := payloadString(envelope.Payload, "collector_instance_id")
	publishedAt := packageRegistryPublishedAtFromPayload(envelope.Payload)
	return PackageRegistryVersionRow{
		UID:                 versionID,
		PackageID:           packageID,
		Ecosystem:           ecosystem,
		Registry:            registry,
		Version:             version,
		PublishedAt:         publishedAt,
		IsYanked:            packageRegistryPayloadBool(envelope.Payload, "is_yanked"),
		IsUnlisted:          packageRegistryPayloadBool(envelope.Payload, "is_unlisted"),
		IsDeprecated:        packageRegistryPayloadBool(envelope.Payload, "is_deprecated"),
		IsRetracted:         packageRegistryPayloadBool(envelope.Payload, "is_retracted"),
		ArtifactURLs:        packageRegistryStringSlice(envelope.Payload, "artifact_urls"),
		Checksums:           packageRegistryStringMap(envelope.Payload, "checksums"),
		SourceFactID:        envelope.FactID,
		StableFactKey:       envelope.StableFactKey,
		SourceSystem:        packageRegistrySourceSystem(envelope),
		SourceRecordID:      envelope.SourceRef.SourceRecordID,
		SourceConfidence:    envelope.SourceConfidence,
		CollectorKind:       envelope.CollectorKind,
		CorrelationAnchors:  packageRegistryCorrelationAnchors(envelope.Payload),
		CollectorInstanceID: collectorInstanceID,
		ObservedAt:          envelope.ObservedAt,
	}, true
}

func packageRegistryDependencyRow(envelope facts.Envelope) (PackageRegistryDependencyRow, bool) {
	if envelope.IsTombstone {
		return PackageRegistryDependencyRow{}, false
	}
	packageID, _ := payloadString(envelope.Payload, "package_id")
	versionID, _ := payloadString(envelope.Payload, "version_id")
	dependencyPackageID, _ := payloadString(envelope.Payload, "dependency_package_id")
	if packageID == "" || versionID == "" || dependencyPackageID == "" {
		return PackageRegistryDependencyRow{}, false
	}
	version, _ := payloadString(envelope.Payload, "version")
	collectorInstanceID, _ := payloadString(envelope.Payload, "collector_instance_id")
	return PackageRegistryDependencyRow{
		UID:                  packageRegistryDependencyUID(envelope),
		PackageID:            packageID,
		VersionID:            versionID,
		Version:              version,
		DependencyPackageID:  dependencyPackageID,
		DependencyEcosystem:  packageRegistryPayloadString(envelope.Payload, "dependency_ecosystem"),
		DependencyRegistry:   packageRegistryPayloadString(envelope.Payload, "dependency_registry"),
		DependencyNamespace:  packageRegistryPayloadString(envelope.Payload, "dependency_namespace"),
		DependencyNormalized: packageRegistryPayloadString(envelope.Payload, "dependency_normalized"),
		DependencyRange:      packageRegistryPayloadString(envelope.Payload, "dependency_range"),
		DependencyType:       packageRegistryPayloadString(envelope.Payload, "dependency_type"),
		TargetFramework:      packageRegistryPayloadString(envelope.Payload, "target_framework"),
		Marker:               packageRegistryPayloadString(envelope.Payload, "marker"),
		Optional:             packageRegistryPayloadBool(envelope.Payload, "optional"),
		Excluded:             packageRegistryPayloadBool(envelope.Payload, "excluded"),
		SourceFactID:         envelope.FactID,
		StableFactKey:        envelope.StableFactKey,
		SourceSystem:         packageRegistrySourceSystem(envelope),
		SourceRecordID:       envelope.SourceRef.SourceRecordID,
		SourceConfidence:     envelope.SourceConfidence,
		CollectorKind:        envelope.CollectorKind,
		CorrelationAnchors:   packageRegistryCorrelationAnchors(envelope.Payload),
		CollectorInstanceID:  collectorInstanceID,
		ObservedAt:           envelope.ObservedAt,
	}, true
}

func packageRegistryPayloadString(payload map[string]any, key string) string {
	value, _ := payloadString(payload, key)
	return value
}

func packageRegistryDependencyUID(envelope facts.Envelope) string {
	if stableFactKey := strings.TrimSpace(envelope.StableFactKey); stableFactKey != "" {
		return stableFactKey
	}
	return strings.TrimSpace(envelope.FactID)
}

func packageRegistryPayloadBool(payload map[string]any, key string) bool {
	value := false
	if ptr := payloadBoolPtr(payload, key); ptr != nil {
		value = *ptr
	}
	return value
}

func packageRegistryPublishedAtFromPayload(payload map[string]any) time.Time {
	text, ok := payloadString(payload, "published_at")
	if !ok {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func packageRegistryStringSlice(payload map[string]any, key string) []string {
	values := payloadStringSlice(payload, key)
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	return values
}

func packageRegistryStringMap(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil
	}
	out := make(map[string]string)
	switch typed := raw.(type) {
	case map[string]string:
		for key, value := range typed {
			if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
				out[trimmedKey] = strings.TrimSpace(value)
			}
		}
	case map[string]any:
		for key, value := range typed {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
				out[trimmedKey] = strings.TrimSpace(text)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func packageRegistryCorrelationAnchors(payload map[string]any) []string {
	anchors := payloadStringSlice(payload, "correlation_anchors")
	if len(anchors) == 0 {
		return nil
	}
	sort.Strings(anchors)
	return anchors
}

func packageRegistrySourceSystem(envelope facts.Envelope) string {
	if sourceSystem := strings.TrimSpace(envelope.SourceRef.SourceSystem); sourceSystem != "" {
		return sourceSystem
	}
	return strings.TrimSpace(envelope.CollectorKind)
}

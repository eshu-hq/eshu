package packageruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLiveJFrogPackageFeed(t *testing.T) {
	if livePackageEnvFirst("ESHU_JFROG_PACKAGE_LIVE") != "1" {
		t.Skip("set ESHU_JFROG_PACKAGE_LIVE=1 to run the live JFrog package-registry smoke")
	}

	target, secrets := liveJFrogPackageTarget(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry-live-jfrog",
		Targets:             []TargetConfig{target},
		Provider: HTTPMetadataProvider{
			Client: &http.Client{Timeout: 30 * time.Second},
		},
		Now: time.Now,
	})
	if err != nil {
		livePackageAssertNoSecrets(t, "NewClaimedSource error", err.Error(), secrets)
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "package-registry-live-jfrog-work-item",
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "collector-package-registry-live-jfrog",
		ScopeID:             target.Base.ScopeID,
		GenerationID:        "package_registry:live-jfrog",
		SourceRunID:         "package_registry:live-jfrog",
		CurrentFencingToken: 1,
	})
	if err != nil {
		livePackageAssertNoSecrets(t, "NextClaimed error", err.Error(), secrets)
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	gotKinds := map[string]int{}
	for envelope := range collected.Facts {
		gotKinds[envelope.FactKind]++
		livePackageAssertEnvelopeSanitized(t, envelope, secrets)
	}
	for _, wantKind := range []string{
		facts.PackageRegistryPackageFactKind,
		facts.PackageRegistryPackageVersionFactKind,
		facts.PackageRegistryPackageArtifactFactKind,
		facts.PackageRegistryRepositoryHostingFactKind,
	} {
		if gotKinds[wantKind] == 0 {
			t.Fatalf("fact kinds = %#v, missing %q", gotKinds, wantKind)
		}
	}
}

func TestLiveJFrogPackageTargetIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		ecosystem     string
		namespace     string
		packageName   string
		wantIdentity  string
		wantNamespace string
		wantScopePath string
	}{
		{
			name:          "npm scope",
			ecosystem:     string(packageregistry.EcosystemNPM),
			namespace:     "team",
			packageName:   "web",
			wantIdentity:  "@team/web",
			wantScopePath: "@team/web",
		},
		{
			name:          "maven group",
			ecosystem:     string(packageregistry.EcosystemMaven),
			namespace:     "org.example",
			packageName:   "core-api",
			wantIdentity:  "core-api",
			wantNamespace: "org.example",
			wantScopePath: "org.example:core-api",
		},
		{
			name:          "generic namespace",
			ecosystem:     string(packageregistry.EcosystemGeneric),
			namespace:     "payments",
			packageName:   "team-api",
			wantIdentity:  "team-api",
			wantNamespace: "payments",
			wantScopePath: "payments/team-api",
		},
		{
			name:          "go module namespace",
			ecosystem:     string(packageregistry.EcosystemGoModule),
			namespace:     "golang.org/x",
			packageName:   "mod",
			wantIdentity:  "golang.org/x/mod",
			wantScopePath: "golang.org/x/mod",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotIdentity, gotNamespace, gotScopePath, err :=
				liveJFrogPackageTargetIdentity(tt.ecosystem, tt.namespace, tt.packageName)
			if err != nil {
				t.Fatalf("liveJFrogPackageTargetIdentity() error = %v", err)
			}
			if gotIdentity != tt.wantIdentity {
				t.Fatalf("identity = %q, want %q", gotIdentity, tt.wantIdentity)
			}
			if gotNamespace != tt.wantNamespace {
				t.Fatalf("namespace = %q, want %q", gotNamespace, tt.wantNamespace)
			}
			if gotScopePath != tt.wantScopePath {
				t.Fatalf("scope path = %q, want %q", gotScopePath, tt.wantScopePath)
			}
		})
	}
}

func TestLiveJFrogPackageTargetIdentityRequiresMavenNamespace(t *testing.T) {
	t.Parallel()

	_, _, _, err := liveJFrogPackageTargetIdentity(
		string(packageregistry.EcosystemMaven),
		"",
		"core-api",
	)
	if err == nil {
		t.Fatal("liveJFrogPackageTargetIdentity() error = nil, want missing Maven namespace error")
	}
	if !strings.Contains(err.Error(), "ESHU_JFROG_PACKAGE_NAMESPACE") {
		t.Fatalf("liveJFrogPackageTargetIdentity() error = %q, want namespace env guidance", err)
	}
}

func TestLivePackageContainsSecretScansEnvelopeSourceRefAndTypedComposites(t *testing.T) {
	t.Parallel()

	secrets := []string{"secret-token"}
	envelope := facts.Envelope{
		FactID:        "package-registry.package:secret-token",
		SourceRef:     facts.Ref{SourceRecordID: "record:secret-token"},
		StableFactKey: "safe-key",
		Payload: map[string]any{
			"typed_strings": []string{"safe", "secret-token"},
			"typed_maps": []map[string]string{
				{"header": "Bearer secret-token"},
			},
			"nested_typed_map": map[string][]string{
				"authorization": {"secret-token"},
			},
			"stringer": livePackageSecretStringer{value: "secret-token"},
		},
	}

	if !livePackageContainsSecret(envelope, secrets) {
		t.Fatal("livePackageContainsSecret() = false, want true for envelope typed composites")
	}
	if livePackageContainsSecret(facts.Envelope{FactID: "safe"}, secrets) {
		t.Fatal("livePackageContainsSecret() = true, want false for safe envelope")
	}
}

type livePackageSecretStringer struct {
	value string
}

func (s livePackageSecretStringer) String() string {
	return s.value
}

func liveJFrogPackageTarget(t *testing.T) (TargetConfig, []string) {
	t.Helper()

	metadataURL := livePackageRequiredEnv(t, "ESHU_JFROG_PACKAGE_METADATA_URL")
	ecosystem := livePackageRequiredEnv(t, "ESHU_JFROG_PACKAGE_ECOSYSTEM")
	packageName := livePackageRequiredEnv(t, "ESHU_JFROG_PACKAGE_NAME")
	namespace := strings.TrimSpace(livePackageEnvFirst("ESHU_JFROG_PACKAGE_NAMESPACE"))
	registry := liveJFrogPackageRegistry(t, metadataURL)
	identity, targetNamespace, scopePath, err := liveJFrogPackageTargetIdentity(ecosystem, namespace, packageName)
	if err != nil {
		t.Skip(err.Error())
	}
	scopeID := fmt.Sprintf("package-registry://jfrog/%s/%s", ecosystem, scopePath)
	username := livePackageEnvFirst("ESHU_JFROG_PACKAGE_USERNAME", "JFROG_USERNAME", "JFROG_USER")
	password := livePackageEnvFirst("ESHU_JFROG_PACKAGE_PASSWORD", "JFROG_PASSWORD")
	bearerToken := livePackageEnvFirst(
		"ESHU_JFROG_PACKAGE_BEARER_TOKEN",
		"JFROG_ACCESS_TOKEN",
		"JFROG_BEARER_TOKEN",
	)

	target := TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "jfrog",
			Ecosystem:    packageregistry.Ecosystem(ecosystem),
			Registry:     registry,
			ScopeID:      scopeID,
			Namespace:    targetNamespace,
			Packages:     []string{identity},
			PackageLimit: 1,
			VersionLimit: 20,
			Visibility:   packageregistry.VisibilityPrivate,
		},
		MetadataURL:    metadataURL,
		DocumentFormat: DocumentFormatArtifactoryPackage,
		Username:       username,
		Password:       password,
		BearerToken:    bearerToken,
	}
	return target, livePackageSecrets(username, password, bearerToken)
}

func liveJFrogPackageRegistry(t *testing.T, metadataURL string) string {
	t.Helper()

	if registry := livePackageEnvFirst(
		"ESHU_JFROG_PACKAGE_REGISTRY",
		"JFROG_PACKAGE_REGISTRY",
		"JFROG_URL",
		"JFROG_BASE_URL",
	); registry != "" {
		return strings.TrimRight(strings.TrimSpace(registry), "/")
	}
	parsed, err := url.Parse(metadataURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Skip("set ESHU_JFROG_PACKAGE_REGISTRY when metadata URL does not include an absolute host")
	}
	return parsed.Scheme + "://" + parsed.Host
}

func liveJFrogPackageTargetIdentity(ecosystem, namespace, packageName string) (string, string, string, error) {
	if namespace == "" {
		if packageregistry.Ecosystem(ecosystem) == packageregistry.EcosystemMaven {
			return "", "", "", fmt.Errorf(
				"set ESHU_JFROG_PACKAGE_NAMESPACE to the Maven groupId when ESHU_JFROG_PACKAGE_ECOSYSTEM=maven",
			)
		}
		return packageName, "", packageName, nil
	}
	switch packageregistry.Ecosystem(ecosystem) {
	case packageregistry.EcosystemNPM:
		scoped := "@" + strings.TrimPrefix(namespace, "@") + "/" + packageName
		return scoped, "", scoped, nil
	case packageregistry.EcosystemMaven:
		return packageName, namespace, namespace + ":" + packageName, nil
	case packageregistry.EcosystemGoModule:
		modulePath := namespace + "/" + packageName
		return modulePath, "", modulePath, nil
	default:
		return packageName, namespace, namespace + "/" + packageName, nil
	}
}

func livePackageRequiredEnv(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("set %s to run the live JFrog package-registry smoke", key)
	}
	return value
}

func livePackageEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func livePackageSecrets(values ...string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) >= 3 {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func livePackageAssertEnvelopeSanitized(t *testing.T, envelope facts.Envelope, secrets []string) {
	t.Helper()

	livePackageAssertSourceRefSanitized(t, envelope.SourceRef, secrets)
	livePackageAssertNoSecrets(t, envelope.FactKind+" envelope", envelope, secrets)
}

func livePackageAssertSourceRefSanitized(t *testing.T, sourceRef facts.Ref, secrets []string) {
	t.Helper()

	if strings.Contains(sourceRef.SourceURI, "?") || strings.Contains(sourceRef.SourceURI, "#") {
		t.Fatalf("SourceRef.SourceURI = %q, want no query or fragment", sourceRef.SourceURI)
	}
	livePackageAssertNoSecrets(t, "SourceRef", sourceRef, secrets)
}

func livePackageAssertNoSecrets(t *testing.T, label string, value any, secrets []string) {
	t.Helper()

	if livePackageContainsSecret(value, secrets) {
		t.Fatalf("%s leaked live credential material", label)
	}
}

func livePackageContainsSecret(value any, secrets []string) bool {
	if len(secrets) == 0 || value == nil {
		return false
	}
	return livePackageValueContainsSecret(reflect.ValueOf(value), secrets, map[visit]bool{})
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

func livePackageValueContainsSecret(value reflect.Value, secrets []string, seen map[visit]bool) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		if value.Kind() == reflect.Pointer {
			key := visit{typ: value.Type(), ptr: value.Pointer()}
			if seen[key] {
				return false
			}
			seen[key] = true
		}
		value = value.Elem()
	}
	if value.CanInterface() {
		if stringer, ok := value.Interface().(fmt.Stringer); ok &&
			livePackageStringContainsSecret(stringer.String(), secrets) {
			return true
		}
		if err, ok := value.Interface().(error); ok && livePackageStringContainsSecret(err.Error(), secrets) {
			return true
		}
	}

	switch value.Kind() {
	case reflect.String:
		return livePackageStringContainsSecret(value.String(), secrets)
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if livePackageValueContainsSecret(key, secrets, seen) ||
				livePackageValueContainsSecret(value.MapIndex(key), secrets, seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if livePackageValueContainsSecret(value.Index(i), secrets, seen) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if livePackageValueContainsSecret(value.Field(i), secrets, seen) {
				return true
			}
		}
	default:
		if value.CanInterface() && livePackageStringContainsSecret(fmt.Sprint(value.Interface()), secrets) {
			return true
		}
	}
	return false
}

func livePackageStringContainsSecret(value string, secrets []string) bool {
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

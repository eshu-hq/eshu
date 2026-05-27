package awsruntime

import (
	"fmt"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ScannerDeps carries the runtime inputs every AWS scanner builder may consume.
//
// Builders treat all fields as inputs, not configuration to mutate. Optional
// fields such as RedactionKey and Checkpoints stay zero when the production
// command does not provide them. Service builders that require an optional
// field MUST validate it themselves and return a typed error.
type ScannerDeps struct {
	// AWSConfig is the claim-scoped AWS SDK configuration the credential lease
	// produced. Builders pass it to per-service SDK adapters.
	AWSConfig aws.Config
	// Boundary names the (account, region, service_kind) tuple under scan. SDK
	// adapters keep it for span and metric attribution.
	Boundary awscloud.Boundary
	// Tracer is the runtime tracer scanners and SDK adapters use.
	Tracer trace.Tracer
	// Instruments are the runtime telemetry instruments scanners and SDK
	// adapters use for counters and gauges.
	Instruments *telemetry.Instruments
	// Checkpoints persists pagination state for scanners that opt in. Builders
	// that ignore checkpoints leave it untouched.
	Checkpoints CheckpointStore
	// RedactionKey produces deterministic markers for sensitive metadata.
	// Builders that need redaction MUST verify it is non-zero before use.
	RedactionKey redact.Key
}

// ScannerBuilder constructs a ServiceScanner for one claimed target from the
// runtime-provided dependencies. Builders MUST return a typed error rather
// than panic when required dependencies are missing.
type ScannerBuilder func(ScannerDeps) (ServiceScanner, error)

// ScannerRegistration binds an AWS service_kind to a builder. Production
// registrations happen from runtimebind sub-packages so the awsruntime
// registry has no compile-time dependency on individual service packages.
type ScannerRegistration struct {
	// ServiceKind must equal an awscloud.ServiceXxx constant. The registry
	// rejects empty values to prevent silent misroutes.
	ServiceKind string
	// Build constructs the scanner. The registry rejects nil to prevent
	// placeholder bindings from shipping to production.
	Build ScannerBuilder
}

// registryState guards the package-level service_kind -> builder map. A
// read-write mutex keeps Register/LookupBuilder honest under -race even though
// production registrations only happen at process start from init().
var (
	registryMu       sync.RWMutex
	scannerRegistry  = map[string]ScannerBuilder{}
)

// Register binds a ScannerRegistration into the package registry. Production
// callers invoke Register from a service runtimebind package init() so the
// awsruntime command surface stays additive when new scanners arrive.
//
// Register panics on empty ServiceKind, nil Build, or duplicate ServiceKind
// because every such case is a programmer error that must surface at process
// start rather than at first scan claim.
func Register(reg ScannerRegistration) {
	if reg.ServiceKind == "" {
		panic("awsruntime.Register: ServiceKind is empty")
	}
	if reg.Build == nil {
		panic(fmt.Sprintf("awsruntime.Register: nil Build for service_kind %q", reg.ServiceKind))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := scannerRegistry[reg.ServiceKind]; exists {
		panic(fmt.Sprintf("awsruntime.Register: duplicate registration for service_kind %q", reg.ServiceKind))
	}
	scannerRegistry[reg.ServiceKind] = reg.Build
}

// LookupBuilder returns the registered builder for service_kind. The bool
// result mirrors the map idiom so consumers cannot confuse a missing
// registration with a nil builder.
func LookupBuilder(service string) (ScannerBuilder, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	build, ok := scannerRegistry[service]
	return build, ok
}

// RegisteredServiceKinds returns the sorted snapshot of registered
// service_kind values. The slice is independent of registry state so callers
// can iterate without holding the read lock.
func RegisteredServiceKinds() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	kinds := make([]string, 0, len(scannerRegistry))
	for kind := range scannerRegistry {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

// unregisterForTest removes a registration. It exists only for tests that
// install fixture builders; production code never calls it. Tests use
// t.Cleanup to keep the package registry stable across runs.
func unregisterForTest(service string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(scannerRegistry, service)
}

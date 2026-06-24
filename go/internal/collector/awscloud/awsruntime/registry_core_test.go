package awsruntime

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestRegisterStoresBuilder confirms Register and LookupBuilder roundtrip a
// registration so init-time bindings can resolve at scan time.
func TestRegisterStoresBuilder(t *testing.T) {
	service := uniqueServiceKind(t, "register-store")
	sentinel := errors.New("builder invoked")
	t.Cleanup(func() { unregisterForTest(service) })

	Register(ScannerRegistration{
		ServiceKind: service,
		Build: func(ScannerDeps) (ServiceScanner, error) {
			return nil, sentinel
		},
	})
	build, ok := LookupBuilder(service)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", service)
	}
	if build == nil {
		t.Fatalf("LookupBuilder(%q) returned nil builder", service)
	}
	if _, err := build(ScannerDeps{}); !errors.Is(err, sentinel) {
		t.Fatalf("builder returned err = %v, want %v", err, sentinel)
	}
}

// TestRegisterPanicsOnDuplicate keeps copy-paste binding bugs surface-able at
// process start, not at first scan claim.
func TestRegisterPanicsOnDuplicate(t *testing.T) {
	service := uniqueServiceKind(t, "register-duplicate")
	t.Cleanup(func() { unregisterForTest(service) })

	Register(ScannerRegistration{
		ServiceKind: service,
		Build:       func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Register did not panic on duplicate %q", service)
		}
		msg, ok := r.(string)
		if !ok {
			if err, isErr := r.(error); isErr {
				msg = err.Error()
			}
		}
		if !strings.Contains(msg, service) {
			t.Fatalf("panic = %v, want service name %q in message", r, service)
		}
	}()
	Register(ScannerRegistration{
		ServiceKind: service,
		Build:       func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
	})
}

// TestRegisterPanicsOnEmptyServiceKind guards the registry map key.
func TestRegisterPanicsOnEmptyServiceKind(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("Register did not panic on empty ServiceKind")
		}
	}()
	Register(ScannerRegistration{
		ServiceKind: "",
		Build:       func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
	})
}

// TestRegisterPanicsOnNilBuild guards against placeholder registrations.
func TestRegisterPanicsOnNilBuild(t *testing.T) {
	service := uniqueServiceKind(t, "register-nil-build")
	defer func() {
		if recover() == nil {
			t.Fatalf("Register did not panic on nil Build")
		}
		unregisterForTest(service)
	}()
	Register(ScannerRegistration{ServiceKind: service, Build: nil})
}

// TestRegisteredServiceKindsSortedSnapshot proves the snapshot is sorted and
// independent of the underlying map iteration order.
func TestRegisteredServiceKindsSortedSnapshot(t *testing.T) {
	prefix := uniqueServiceKind(t, "registered-sorted") + "-"
	names := []string{prefix + "z", prefix + "a", prefix + "m"}
	for _, name := range names {
		name := name
		Register(ScannerRegistration{
			ServiceKind: name,
			Build: func(ScannerDeps) (ServiceScanner, error) {
				return nil, nil
			},
		})
		t.Cleanup(func() { unregisterForTest(name) })
	}

	snapshot := RegisteredServiceKinds()
	if !sort.StringsAreSorted(snapshot) {
		t.Fatalf("RegisteredServiceKinds() = %v, want sorted", snapshot)
	}
	have := map[string]bool{}
	for _, kind := range snapshot {
		have[kind] = true
	}
	for _, name := range names {
		if !have[name] {
			t.Fatalf("RegisteredServiceKinds() missing %q", name)
		}
	}
	// Returned slice must be a copy so callers cannot mutate registry state.
	snapshot[0] = "mutated"
	for _, kind := range RegisteredServiceKinds() {
		if kind == "mutated" {
			t.Fatalf("RegisteredServiceKinds() returned shared slice")
		}
	}
}

// TestLookupBuilderMiss is the negative case the production fallback path
// depends on.
func TestLookupBuilderMiss(t *testing.T) {
	if _, ok := LookupBuilder("not-a-real-service-kind-xyzzy"); ok {
		t.Fatalf("LookupBuilder(unknown) ok = true, want false")
	}
}

// TestServiceRequiresRedactionKeyRoundtrip proves the registry records and
// reports the per-service RequiresRedactionKey flag, so the command can derive
// the requirement instead of hardcoding a service switch.
func TestServiceRequiresRedactionKeyRoundtrip(t *testing.T) {
	requiring := uniqueServiceKind(t, "requires-redaction")
	notRequiring := uniqueServiceKind(t, "no-redaction")
	t.Cleanup(func() {
		unregisterForTest(requiring)
		unregisterForTest(notRequiring)
	})

	Register(ScannerRegistration{
		ServiceKind:          requiring,
		RequiresRedactionKey: true,
		Build:                func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
	})
	Register(ScannerRegistration{
		ServiceKind: notRequiring,
		Build:       func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
	})

	if !ServiceRequiresRedactionKey(requiring) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = false, want true", requiring)
	}
	if ServiceRequiresRedactionKey(notRequiring) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", notRequiring)
	}
	if ServiceRequiresRedactionKey("not-a-real-service-kind-xyzzy") {
		t.Fatalf("ServiceRequiresRedactionKey(unknown) = true, want false")
	}
}

// TestServiceKindsRequiringRedactionKeySortedSnapshot proves the derived set is
// sorted, contains only the flagged kinds, and is independent of registry
// state so callers cannot mutate the registry through the returned slice.
func TestServiceKindsRequiringRedactionKeySortedSnapshot(t *testing.T) {
	prefix := uniqueServiceKind(t, "redaction-set") + "-"
	flagged := []string{prefix + "z", prefix + "a", prefix + "m"}
	for _, name := range flagged {
		name := name
		Register(ScannerRegistration{
			ServiceKind:          name,
			RequiresRedactionKey: true,
			Build:                func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
		})
		t.Cleanup(func() { unregisterForTest(name) })
	}
	unflagged := prefix + "plain"
	Register(ScannerRegistration{
		ServiceKind: unflagged,
		Build:       func(ScannerDeps) (ServiceScanner, error) { return nil, nil },
	})
	t.Cleanup(func() { unregisterForTest(unflagged) })

	snapshot := ServiceKindsRequiringRedactionKey()
	if !sort.StringsAreSorted(snapshot) {
		t.Fatalf("ServiceKindsRequiringRedactionKey() = %v, want sorted", snapshot)
	}
	have := map[string]bool{}
	for _, kind := range snapshot {
		have[kind] = true
	}
	for _, name := range flagged {
		if !have[name] {
			t.Fatalf("ServiceKindsRequiringRedactionKey() missing flagged %q", name)
		}
	}
	if have[unflagged] {
		t.Fatalf("ServiceKindsRequiringRedactionKey() included unflagged %q", unflagged)
	}
	// Returned slice must be a copy so callers cannot mutate registry state.
	snapshot[0] = "mutated"
	for _, kind := range ServiceKindsRequiringRedactionKey() {
		if kind == "mutated" {
			t.Fatalf("ServiceKindsRequiringRedactionKey() returned shared slice")
		}
	}
}

// TestConcurrentRegisterIsRaceFree exercises the registry under -race so a
// future contributor cannot remove the mutex by accident.
func TestConcurrentRegisterIsRaceFree(t *testing.T) {
	prefix := uniqueServiceKind(t, "concurrent-register") + "-"
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		name := prefix + workerIndexLabel(i)
		t.Cleanup(func() { unregisterForTest(name) })
		go func() {
			defer wg.Done()
			Register(ScannerRegistration{
				ServiceKind: name,
				Build: func(ScannerDeps) (ServiceScanner, error) {
					return nil, nil
				},
			})
		}()
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		name := prefix + workerIndexLabel(i)
		if _, ok := LookupBuilder(name); !ok {
			t.Fatalf("LookupBuilder(%q) missing after concurrent Register", name)
		}
	}
}

// uniqueServiceKind builds a test-scoped service-kind so parallel tests cannot
// fight over registry entries.
func uniqueServiceKind(t *testing.T, label string) string {
	t.Helper()
	return "test-" + label + "-" + t.Name()
}

func workerIndexLabel(i int) string {
	const digits = "0123456789abcdef"
	if i < len(digits) {
		return string(digits[i])
	}
	return string(digits[i/len(digits)]) + string(digits[i%len(digits)])
}

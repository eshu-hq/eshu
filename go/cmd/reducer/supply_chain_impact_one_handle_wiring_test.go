// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactLoadsClaimsAndFanOutShareOneHandle is the #10972 D4b
// precondition. The two-read in-flight narrowing is sound only when the impact
// pass's evidence loads, its writer, the queue's claims, and the fan-out's
// claim reads all run on one primary: a claim seen by one read is then visible
// to every later read. This drives the real buildReducerService with a read
// replica configured (ESHU_POSTGRES_READ_DSN, standby topology, staged-audits
// routing, the QA reducer's shape) and asserts every one of those components
// holds the single database handle the entrypoint passed in. The read replica
// pool is opened only by the winners Auditor (observed_service_wiring.go), so a
// component that reached it, or any second handle, fails here.
func TestSupplyChainImpactLoadsClaimsAndFanOutShareOneHandle(t *testing.T) {
	t.Parallel()

	database := &supplyChainReadinessWiringDB{}
	replica := map[string]string{
		"ESHU_POSTGRES_READ_DSN":      "postgres://replica.invalid:1/eshu?sslmode=disable",
		"ESHU_POSTGRES_READ_TOPOLOGY": "standby",
		"ESHU_POSTGRES_READ_ROUTING":  "staged-audits",
	}
	service, err := buildReducerService(
		context.Background(), database, stubGraphExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(database), stubCypherReader{}, stubCypherReader{},
		obligationConsumerDisabledGetenv(func(key string) string { return replica[key] }), nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v", err)
	}
	want := reflect.ValueOf(database).Pointer()
	handler := supplyChainImpactRegisteredHandler(t, service)

	for name, got := range map[string]reflect.Value{
		"impact fact loader":         wiringField(t, wiringField(t, handler, "FactLoader"), "database"),
		"authorized evidence loader": wiringField(t, wiringField(t, handler, "AuthorizedEvidenceLoader"), "database"),
		"impact writer":              wiringField(t, wiringField(t, wiringField(t, handler, "Writer"), "DB"), "Beginner"),
		"fan-out store reads":        wiringField(t, wiringField(t, handler, "FanOut"), "Beginner"),
		"fan-out store statements":   wiringField(t, wiringField(t, handler, "FanOut"), "DB"),
		"group projection queue":     wiringField(t, wiringField(t, wiringField(t, handler, "GroupProjectionEnqueuer"), "queue"), "database"),
		"work queue claims":          wiringField(t, reflect.ValueOf(service.WorkSource), "database"),
	} {
		if wiringHandle(t, name, got) != want {
			t.Errorf("%s does not hold the entrypoint's database handle", name)
		}
	}
	port := wiringField(t, handler, "FanOut").Elem()
	if !port.Type().Implements(reflect.TypeOf((*supplychaincore.InFlightClaimReader)(nil)).Elem()) {
		t.Fatalf("fan-out port %s does not serve the claim reads", port.Type())
	}
}

// supplyChainImpactRegisteredHandler walks the service's executor chain to the
// reducer runtime and returns the registered supply_chain_impact handler as a
// read-only reflected value (the registry is unexported).
func supplyChainImpactRegisteredHandler(t *testing.T, service reducer.Service) reflect.Value {
	t.Helper()
	executor := service.Executor
	for {
		wrapped, ok := executor.(*activeWorkerExecutor)
		if !ok {
			break
		}
		executor = wrapped.inner
	}
	runtime, ok := executor.(*reducer.Runtime)
	if !ok {
		t.Fatalf("service.Executor chain reaches %T, want *reducer.Runtime", executor)
	}
	defs := reflect.ValueOf(runtime).Elem().FieldByName("registry").FieldByName("defs")
	def := defs.MapIndex(reflect.ValueOf(reducer.DomainSupplyChainImpact))
	if !def.IsValid() {
		t.Fatal("supply_chain_impact is not registered")
	}
	handler := def.FieldByName("Handler").Elem()
	if handler.Type() != reflect.TypeOf(supplychaincore.SupplyChainImpactHandler{}) {
		t.Fatalf("registered handler is %s, want supplychaincore.SupplyChainImpactHandler", handler.Type())
	}
	return handler
}

// wiringField returns the named field of v after unwrapping interfaces and pointers.
func wiringField(t *testing.T, v reflect.Value, name string) reflect.Value {
	t.Helper()
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			t.Fatalf("nil %s before field %q", v.Type(), name)
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		t.Fatalf("cannot read field %q of %v", name, v)
	}
	f := v.FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("%s has no field %q", v.Type(), name)
	}
	return f
}

// wiringHandle returns the address of the database handle v holds: an interface
// whose dynamic value is the fake database pointer.
func wiringHandle(t *testing.T, name string, v reflect.Value) uintptr {
	t.Helper()
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			t.Fatalf("%s: nil handle", name)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Pointer {
		t.Fatalf("%s: handle is %s, want a pointer", name, v.Type())
	}
	return v.Pointer()
}

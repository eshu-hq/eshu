// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestBootstrapTemplateFlavourValidation pins the flavour contract: only a
// short safe charset becomes part of a template database name, so neither a
// hostile nor a mistyped flavour can escape the generated namespace or the
// 63-byte identifier limit.
func TestBootstrapTemplateFlavourValidation(t *testing.T) {
	t.Parallel()
	for _, flavour := range []string{"public", "public_nocsearch", "a", "x_y_2"} {
		if err := validateBootstrapTemplateFlavour(flavour); err != nil {
			t.Errorf("valid flavour %q rejected: %v", flavour, err)
		}
	}
	for _, flavour := range []string{"", "Public", "public-bootstrap", "a b", "x;DROP", strings.Repeat("a", 25)} {
		if err := validateBootstrapTemplateFlavour(flavour); err == nil {
			t.Errorf("invalid flavour %q accepted", flavour)
		}
	}
}

// TestBootstrapTemplateNameStaysInNamespace pins that generated template
// names fit the identifier limit and validate back through the namespace
// gate clone and discard rely on.
func TestBootstrapTemplateNameStaysInNamespace(t *testing.T) {
	t.Parallel()
	for _, flavour := range []string{"public", "public_nocsearch", strings.Repeat("a", 24)} {
		name, err := bootstrapTemplateName(flavour)
		if err != nil {
			t.Fatalf("flavour %q: %v", flavour, err)
		}
		if len(name) > 63 {
			t.Errorf("flavour %q: template name %q exceeds 63 bytes", flavour, name)
		}
		if err := validateBootstrapTemplateName(name); err != nil {
			t.Errorf("generated name %q refused by the namespace gate: %v", name, err)
		}
	}
	for _, name := range []string{"", "postgres", "template0", "eshu_content_index_proof_abcd", bootstrapTemplateNamePrefix + "UPPER", bootstrapTemplateNamePrefix + "has space"} {
		if err := validateBootstrapTemplateName(name); err == nil {
			t.Errorf("non-template name %q accepted by the namespace gate", name)
		}
	}
}

// TestBootstrapTemplateKeyScopesServerAndFlavour pins that the cache key
// separates proof servers and flavours, so two families on different servers
// never share a template under one flavour name.
func TestBootstrapTemplateKeyScopesServerAndFlavour(t *testing.T) {
	t.Parallel()
	a := bootstrapTemplateKey("postgres://eshu@host-a/postgres?sslmode=disable", "public")
	b := bootstrapTemplateKey("postgres://eshu@host-b/postgres?sslmode=disable", "public")
	c := bootstrapTemplateKey("postgres://eshu@host-a/postgres?sslmode=disable", "public_nocsearch")
	if a == b || a == c || b == c {
		t.Fatalf("cache keys collide: %q %q %q", a, b, c)
	}
	if got := bootstrapTemplateKey("dsn", "public"); !strings.HasPrefix(got, "dsn\x00") {
		t.Fatalf("cache key %q does not scope the DSN from the flavour", got)
	}
}

// TestDiscardBootstrapTemplatesEmpty is the no-op half of the TestMain
// teardown contract: a process that built no template discards nothing and
// reports no error.
func TestDiscardBootstrapTemplatesEmpty(t *testing.T) {
	t.Parallel()
	if errs := DiscardBootstrapTemplates(); len(errs) != 0 {
		t.Fatalf("empty discard reported %v", errs)
	}
}

// TestDiscardBootstrapTemplatesReportsDropFailure is the failing-drop half
// of the TestMain teardown contract (#10148 F1): a template whose force-drop
// fails must surface in the Discard return (wrapping the template name) so
// TestMain can fail the process instead of leaking the template silently.
// The cached entry points at a refused port, so the drop fails without a
// live server.
func TestDiscardBootstrapTemplatesReportsDropFailure(t *testing.T) {
	bootstrapTemplateCache.Lock()
	saved := bootstrapTemplateCache.byKey
	bootstrapTemplateCache.byKey = map[string]string{}
	dsn := "postgres://eshu:change-me@127.0.0.1:1/postgres?sslmode=disable"
	name := bootstrapTemplateNamePrefix + "public_deadbeef"
	bootstrapTemplateCache.byKey[bootstrapTemplateKey(dsn, "public")] = name
	bootstrapTemplateCache.Unlock()
	defer func() {
		bootstrapTemplateCache.Lock()
		bootstrapTemplateCache.byKey = saved
		bootstrapTemplateCache.Unlock()
	}()
	errs := DiscardBootstrapTemplates()
	if len(errs) == 0 {
		t.Fatal("discard with a failing drop reported no error, want the drop failure")
	}
	for _, err := range errs {
		if err == nil {
			t.Fatal("discard returned a nil error entry")
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("discard error = %v, want it to wrap template name %q", err, name)
		}
	}
}

// TestDropDisposableDatabaseNameReportsFailure pins the F1 primitive: the
// error-returning drop used by Discard must propagate a failing drop
// instead of discarding it.
func TestDropDisposableDatabaseNameReportsFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	admin := &scriptedDropAdmin{dropResults: []error{boom}}
	if err := dropDisposableDatabaseName(context.Background(), admin, "eshu_content_index_proof_deadbeef"); !errors.Is(err, boom) {
		t.Fatalf("dropDisposableDatabaseName() = %v, want the drop failure %v", err, boom)
	}
}

// scriptedCloneAdmin scripts CREATE DATABASE outcomes for the clone retry
// proof: it records every CREATE without touching a server, so the 55006
// retry matrix runs deterministically.
type scriptedCloneAdmin struct {
	createCalls   int
	createResults []error
}

func (f *scriptedCloneAdmin) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if strings.HasPrefix(query, "CREATE DATABASE") {
		f.createCalls++
		if len(f.createResults) > 0 {
			err := f.createResults[0]
			f.createResults = f.createResults[1:]
			return stubSQLResult{}, err
		}
		return stubSQLResult{}, nil
	}
	return stubSQLResult{}, errors.New("unexpected admin query: " + query)
}

// TestCloneRetriesObjectInUseThenSucceeds pins the F2 remedy: a clone CREATE
// that reports SQLSTATE 55006 (a transient backend on the template winning
// the race after waitForTemplateIdle proved zero sessions) is retried with
// the same bound as the disposable drop, instead of failing the test.
func TestCloneRetriesObjectInUseThenSucceeds(t *testing.T) {
	t.Parallel()
	admin := &scriptedCloneAdmin{createResults: []error{objectInUseError(), objectInUseError(), nil}}
	if err := cloneBootstrapTemplateDatabase(context.Background(), admin, "eshu_content_index_proof_clone", bootstrapTemplateNamePrefix+"public_deadbeef"); err != nil {
		t.Fatalf("cloneBootstrapTemplateDatabase() = %v, want success after 55006 retries", err)
	}
	if admin.createCalls != 3 {
		t.Fatalf("CREATE calls = %d, want 3 (initial plus two 55006 retries)", admin.createCalls)
	}
}

// TestCloneAbortsOnNonObjectInUse pins that only the clone race (55006)
// retries: any other CREATE failure returns immediately.
func TestCloneAbortsOnNonObjectInUse(t *testing.T) {
	t.Parallel()
	admin := &scriptedCloneAdmin{createResults: []error{errors.New("permission denied")}}
	if err := cloneBootstrapTemplateDatabase(context.Background(), admin, "eshu_content_index_proof_clone", bootstrapTemplateNamePrefix+"public_deadbeef"); err == nil {
		t.Fatal("cloneBootstrapTemplateDatabase() = nil, want the permission error")
	}
	if admin.createCalls != 1 {
		t.Fatalf("CREATE calls = %d, want 1 (no retry on non-55006)", admin.createCalls)
	}
}

// recordingDropTB scripts the minimal testing surface the clone-drop
// verification needs, capturing loud reports without failing the test.
type recordingDropTB struct {
	errors []string
}

func (f *recordingDropTB) Helper() {}

func (f *recordingDropTB) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

// stubExistenceProber scripts the pg_database probe: it reports the clone
// as present, absent, or unprobable without touching a server.
type stubExistenceProber struct {
	exists bool
	err    error
}

func (s stubExistenceProber) CloneExists(context.Context, string) (bool, error) {
	return s.exists, s.err
}

// TestCloneDropVerificationReportsLeakedClone is the RED half of the P2
// silent-cleanup remedy (#10148 review): a clone that survives its cleanup
// must fail the test loudly like DiscardBootstrapTemplates does, instead of
// leaking on the shared proof server silently.
func TestCloneDropVerificationReportsLeakedClone(t *testing.T) {
	t.Parallel()
	tb := &recordingDropTB{}
	assertCloneDropped(tb, context.Background(), stubExistenceProber{exists: true}, "eshu_content_index_proof_leaked")
	if len(tb.errors) == 0 {
		t.Fatal("leaked clone reported no error, want a loud drop-failure report")
	}
	for _, msg := range tb.errors {
		if !strings.Contains(msg, "eshu_content_index_proof_leaked") {
			t.Fatalf("drop-failure report = %q, want it to name the leaked clone", msg)
		}
	}
}

// TestCloneDropVerificationReportsProbeFailure pins that an unverified drop
// is not a verified one: when the pg_database probe itself fails, the
// verification reports loudly instead of assuming the drop worked.
func TestCloneDropVerificationReportsProbeFailure(t *testing.T) {
	t.Parallel()
	tb := &recordingDropTB{}
	assertCloneDropped(tb, context.Background(), stubExistenceProber{err: errors.New("boom")}, "eshu_content_index_proof_unprobed")
	if len(tb.errors) == 0 {
		t.Fatal("failing drop probe reported no error, want a loud report")
	}
}

// TestCloneDropVerificationStaysSilentOnCleanDrop pins the quiet half: a
// clone that dropped cleanly reports nothing, so the verification never
// fails a healthy teardown.
func TestCloneDropVerificationStaysSilentOnCleanDrop(t *testing.T) {
	t.Parallel()
	tb := &recordingDropTB{}
	assertCloneDropped(tb, context.Background(), stubExistenceProber{exists: false}, "eshu_content_index_proof_gone")
	if len(tb.errors) != 0 {
		t.Fatalf("clean drop reported %v, want silence", tb.errors)
	}
}

// TestCloneGivesUpAfterBoundedAttempts pins the F2 retry bound: a template
// that never goes idle fails loudly instead of stalling the test, so a
// genuinely parked session still observes 55006 after the bound.
func TestCloneGivesUpAfterBoundedAttempts(t *testing.T) {
	t.Parallel()
	results := make([]error, disposableDropMaxAttempts+2)
	for i := range results {
		results[i] = objectInUseError()
	}
	admin := &scriptedCloneAdmin{createResults: results}
	err := cloneBootstrapTemplateDatabase(context.Background(), admin, "eshu_content_index_proof_clone", bootstrapTemplateNamePrefix+"public_deadbeef")
	if err == nil || !strings.Contains(err.Error(), "55006") {
		t.Fatalf("cloneBootstrapTemplateDatabase() = %v, want the persistent 55006", err)
	}
	if admin.createCalls != disposableDropMaxAttempts {
		t.Fatalf("CREATE calls = %d, want %d (bounded, not endless)", admin.createCalls, disposableDropMaxAttempts)
	}
}

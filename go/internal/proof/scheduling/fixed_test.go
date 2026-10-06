// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSelectFixedCanonical(t *testing.T) {
	workload, err := selectFixedCanonical("canonical")
	if err != nil {
		t.Fatal(err)
	}
	if workload.name != "canonical" || len(workload.terms) != len(terms) {
		t.Fatalf("wrong canonical workload: %#v", workload)
	}
	if len(workload.filters) != 1 || workload.filters[0] != "eshu_require_content_substring_indexes_ready()" {
		t.Fatalf("wrong canonical readiness filter: %v", workload.filters)
	}
	for index, term := range terms {
		if workload.terms[index] != term {
			t.Fatalf("term %d: got %q, want %q", index, workload.terms[index], term)
		}
	}
	for _, name := range []string{"", "punctuation", "timing_canonical", "parallel8"} {
		if _, err := selectFixedCanonical(name); err == nil {
			t.Fatalf("accepted unsupported workload %q", name)
		}
	}
}

func TestConfigureProofTargetSocketOnlyForFixedCanonical(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		socketDir string
		wantHost  string
		wantPort  uint16
		wantErr   bool
	}{
		{mode: "fixed_canonical", socketDir: "/tmp/eshu7033-fixture/socket", wantHost: "/tmp/eshu7033-fixture/socket", wantPort: 5432},
		{mode: "fixed_diagnostic", socketDir: "/tmp/eshu7033-fixture/socket", wantHost: "/tmp/eshu7033-fixture/socket", wantPort: 5432},
		{mode: "fixed_canonical", socketDir: "relative/socket", wantErr: true},
		{mode: "parallel8", socketDir: "/tmp/eshu7033-fixture/socket", wantErr: true},
		{mode: "parallel8", wantHost: "127.0.0.1", wantPort: 15433},
		{mode: "fixed_canonical", wantHost: "127.0.0.1", wantPort: 15433},
	} {
		config, err := pgx.ParseConfig("postgres://eshu7033@localhost/fixture")
		if err != nil {
			t.Fatal(err)
		}
		err = configureProofTarget(config, tc.mode, tc.socketDir)
		if (err != nil) != tc.wantErr {
			t.Fatalf("mode=%q socket=%q error=%v", tc.mode, tc.socketDir, err)
		}
		if !tc.wantErr && (config.Host != tc.wantHost || config.Port != tc.wantPort) {
			t.Fatalf("mode=%q socket=%q host=%q port=%d", tc.mode, tc.socketDir, config.Host, config.Port)
		}
	}
}

func TestConfigureProofTargetFixedPort(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    string
		port    string
		want    uint16
		wantErr bool
	}{
		{name: "remote fixed proof", mode: "fixed_canonical", port: "25433", want: 25433},
		{name: "remote fixed diagnostic", mode: "fixed_diagnostic", port: "25433", want: 25433},
		{name: "reject other mode", mode: "parallel8", port: "25433", wantErr: true},
		{name: "reject zero", mode: "fixed_canonical", port: "0", wantErr: true},
		{name: "reject overflow", mode: "fixed_canonical", port: "65536", wantErr: true},
		{name: "reject nonnumeric", mode: "fixed_canonical", port: "abc", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := pgx.ParseConfig("postgres://eshu7033@localhost/fixture")
			if err != nil {
				t.Fatal(err)
			}
			err = configureProofTargetPort(config, tc.mode, "", tc.port)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, wantErr=%t", err, tc.wantErr)
			}
			if !tc.wantErr && (config.Host != "127.0.0.1" || config.Port != tc.want) {
				t.Fatalf("host=%q port=%d", config.Host, config.Port)
			}
		})
	}
}

type fakeFixedReader struct {
	deadline time.Time
	closed   bool
	err      error
}

func (reader *fakeFixedReader) Close(ctx context.Context) error {
	reader.closed = true
	reader.deadline, _ = ctx.Deadline()
	return reader.err
}

func TestFixedCaseAndCleanupBounds(t *testing.T) {
	caseCtx, cancel := fixedCaseContext(context.Background())
	deadline, ok := caseCtx.Deadline()
	if !ok || time.Until(deadline) < 49*time.Second || time.Until(deadline) > 50*time.Second {
		t.Fatalf("fixed case deadline = %v, present=%t", deadline, ok)
	}
	cancel()
	if !errors.Is(caseCtx.Err(), context.Canceled) {
		t.Fatalf("fixed case cancel = %v", caseCtx.Err())
	}
	wantErr := errors.New("close failed")
	first := &fakeFixedReader{err: wantErr}
	second := &fakeFixedReader{}
	closeErr := closeFixedReaders([]*fakeFixedReader{first, second})
	if !first.closed || !second.closed {
		t.Fatal("cleanup did not attempt every reader")
	}
	for _, reader := range []*fakeFixedReader{first, second} {
		remaining := time.Until(reader.deadline)
		if remaining < 9*time.Second || remaining > 10*time.Second {
			t.Fatalf("cleanup deadline remaining = %s", remaining)
		}
	}
	if !errors.Is(closeErr, wantErr) {
		t.Fatalf("cleanup error = %v", closeErr)
	}
}

func TestFixedPrimaryGuard(t *testing.T) {
	if err := requireFixedPrimary("eshu7033", "eshu7033", "123456789", "123456789", false, "on"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, expected, actual, expectedSystem, actualSystem, readOnly string
		recovery                                                       bool
	}{
		{name: "missing expected", actual: "eshu7033", expectedSystem: "123456789", actualSystem: "123456789", readOnly: "on"},
		{name: "wrong database", expected: "eshu7033", actual: "postgres", expectedSystem: "123456789", actualSystem: "123456789", readOnly: "on"},
		{name: "missing expected system", expected: "eshu7033", actual: "eshu7033", actualSystem: "123456789", readOnly: "on"},
		{name: "wrong system", expected: "eshu7033", actual: "eshu7033", expectedSystem: "123456789", actualSystem: "987654321", readOnly: "on"},
		{name: "standby", expected: "eshu7033", actual: "eshu7033", expectedSystem: "123456789", actualSystem: "123456789", recovery: true, readOnly: "on"},
		{name: "writable", expected: "eshu7033", actual: "eshu7033", expectedSystem: "123456789", actualSystem: "123456789", readOnly: "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := requireFixedPrimary(tc.expected, tc.actual, tc.expectedSystem, tc.actualSystem, tc.recovery, tc.readOnly); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
	if err := requireFixedPrimary("eshu7033", "eshu7033", "123456789", "123456789", false, "ON"); err == nil {
		t.Fatal("noncanonical read-only setting accepted")
	}
	for _, value := range []string{"", "0", "01", "abc", "123 ", "-1"} {
		if err := validateExpectedSystemID(value); err == nil {
			t.Fatalf("accepted expected system identifier %q", value)
		}
	}
	if err := validateFixedDatabase("eshu7033", "postgres"); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("config database mismatch accepted: %v", err)
	}
	if err := validateFixedDatabase("", "eshu7033"); err == nil {
		t.Fatal("missing expected database accepted")
	}
}

func TestFixedCanonicalRejectsTargetBeforeConnect(t *testing.T) {
	if err := runFixedCanonical(context.Background(), nil, "eshu7033"); err == nil {
		t.Fatal("nil config accepted")
	}
	config := &pgx.ConnConfig{}
	if err := runFixedCanonical(context.Background(), config, ""); err == nil {
		t.Fatal("missing expected database accepted")
	}
	config.Database = "postgres"
	if err := runFixedCanonical(context.Background(), config, "eshu7033"); err == nil {
		t.Fatal("mismatched database accepted")
	}
	config.Database = "eshu7033"
	t.Setenv("ESHU7033_EXPECTED_SYSTEM_ID", "")
	if err := runFixedCanonical(context.Background(), config, "eshu7033"); err == nil {
		t.Fatal("missing system identifier accepted")
	}
}

func TestFixedDiagnosticRejectsTargetBeforeConnect(t *testing.T) {
	if err := runFixedDiagnostic(context.Background(), nil, "eshu7033"); err == nil {
		t.Fatal("nil config accepted")
	}
	config, err := pgx.ParseConfig("postgres://eshu7033@localhost/postgres")
	if err != nil {
		t.Fatal(err)
	}
	if err := runFixedDiagnostic(context.Background(), config, "eshu7033"); err == nil {
		t.Fatal("mismatched database accepted")
	}
	config.Database = "eshu7033"
	t.Setenv("ESHU7033_EXPECTED_SYSTEM_ID", "")
	if err := runFixedDiagnostic(context.Background(), config, "eshu7033"); err == nil {
		t.Fatal("missing system identifier accepted")
	}
}

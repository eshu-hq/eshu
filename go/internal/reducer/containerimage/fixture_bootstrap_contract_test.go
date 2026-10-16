// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestContainerImageDisposableBootstrapConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, optIn, expected, dsn, want string
		wantError                        bool
	}{
		{name: "ordinary test run"},
		{name: "ordinary configured run", dsn: "postgres://localhost/existing"},
		{name: "explicit disposable", optIn: "1", expected: "eshu_containerimage_contract", dsn: "postgres://localhost/eshu_containerimage_contract", want: "eshu_containerimage_contract"},
		{name: "missing opt in", expected: "eshu_containerimage_contract", wantError: true},
		{name: "invalid opt in", optIn: "true", wantError: true},
		{name: "missing DSN", optIn: "1", expected: "eshu_containerimage_contract", wantError: true},
		{name: "missing database identity", optIn: "1", dsn: "postgres://localhost/eshu_containerimage_contract", wantError: true},
		{name: "foreign database", optIn: "1", expected: "production", dsn: "postgres://localhost/production", wantError: true},
		{name: "mismatched database", optIn: "1", expected: "eshu_containerimage_contract", dsn: "postgres://localhost/eshu_containerimage_other", wantError: true},
		{name: "query database override", optIn: "1", expected: "eshu_containerimage_contract", dsn: "postgres://localhost/eshu_containerimage_contract?dbname=production", wantError: true},
		{name: "non PostgreSQL URI", optIn: "1", expected: "eshu_containerimage_contract", dsn: "https://localhost/eshu_containerimage_contract", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := disposableBootstrapDatabase(test.optIn, test.expected, test.dsn)
			if (err != nil) != test.wantError || got != test.want {
				t.Fatalf("database = %q, error = %v; want %q, error=%v", got, err, test.want, test.wantError)
			}
		})
	}
}

func TestContainerImageBootstrapFailureStopsTestBinary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, optIn, database, dsn string }{
		{name: "missing opt in", database: "eshu_containerimage_contract"},
		{name: "missing DSN", optIn: "1", database: "eshu_containerimage_contract"},
		{name: "connection failure", optIn: "1", database: "eshu_containerimage_contract", dsn: "postgres://eshu:change-me@127.0.0.1:1/eshu_containerimage_contract?connect_timeout=1&sslmode=disable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(executable, "-test.run=^$", "-containerimage-bootstrap="+test.optIn, "-containerimage-database="+test.database)
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "ESHU_POSTGRES_DSN=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "ESHU_POSTGRES_DSN="+test.dsn)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "container-image disposable bootstrap:") {
				t.Fatalf("bootstrap child error=%v, output=%s; want explicit bootstrap failure", err, output)
			}
		})
	}
}

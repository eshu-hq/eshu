// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package partitionguard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/partitionguard"
)

// fixtureService is one synthetic scanner package exercising every shape the
// guard must classify: commercial-prefixed ARN synthesis via concatenation, a
// printf format string, a fmt.Sprint (concatenating, no format string), and a
// commercial-prefixed const used in concatenation — all of which must be
// flagged — plus a derived-partition ARN, ARN parse calls, and a non-commercial
// (GovCloud) literal, which must NOT be flagged.
const fixtureService = `package svc

import (
	"fmt"
	"strings"
)

const commercialPrefix = "arn:aws:foo:"

func build(region, part string, x int) string {
	_ = "arn:aws:ec2:" + region                          // FLAG: concatenation
	_ = fmt.Sprintf("arn:aws:codedeploy:%s", region)     // FLAG: format string
	_ = fmt.Sprint("arn:aws:lambda:", region)            // FLAG: fmt.Sprint argument
	_ = commercialPrefix + region                        // FLAG: const-bound concatenation
	_ = "arn:" + part + ":ec2:" + region                 // ok: derived partition
	_ = "arn:aws-us-gov:ec2:" + region                   // ok: non-commercial literal
	if strings.HasPrefix(region, "arn:aws:s3:::") {      // ok: parse, not synthesis
		_ = strings.TrimPrefix(region, "arn:aws:s3:::")  // ok: parse, not synthesis
	}
	return region
}
`

func TestScanClassifiesSynthesisVsParse(t *testing.T) {
	servicesDir := t.TempDir()
	svcDir := filepath.Join(servicesDir, "svc")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "relationships.go"), []byte(fixtureService), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := partitionguard.ScanForHardcodedPartitions(servicesDir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Exactly the four synthesis sites, none of the parse/derived/gov sites.
	wantContexts := map[string]int{
		"concatenation":       2, // "arn:aws:ec2:" + region, commercialPrefix + region
		"format string":       1, // fmt.Sprintf("arn:aws:codedeploy:%s", ...)
		"fmt.Sprint argument": 1, // fmt.Sprint("arn:aws:lambda:", ...)
	}
	gotContexts := map[string]int{}
	for _, v := range violations {
		gotContexts[v.Context]++
	}
	if len(violations) != 4 {
		t.Fatalf("got %d violations, want 4:\n%v", len(violations), violations)
	}
	for ctx, want := range wantContexts {
		if gotContexts[ctx] != want {
			t.Fatalf("context %q count = %d, want %d (all: %v)", ctx, gotContexts[ctx], want, violations)
		}
	}

	// A non-commercial (GovCloud) literal and ARN parse calls must never be
	// flagged, even though they contain `arn:aws-`/`arn:aws:` substrings.
	for _, v := range violations {
		if v.Literal == "arn:aws-us-gov:ec2:" {
			t.Fatalf("non-commercial literal was flagged: %v", v)
		}
		if v.Context != "concatenation" && v.Context != "format string" && v.Context != "fmt.Sprint argument" {
			t.Fatalf("unexpected violation context: %v", v)
		}
	}
}

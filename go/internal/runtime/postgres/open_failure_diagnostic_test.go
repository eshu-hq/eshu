// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestOpenFailureSummaryClassifiesReaderPingTransient pins the #8486
// diagnostic contract: a masked reader-ping failure unwraps to closed
// classification tokens, never to driver text.
func TestOpenFailureSummaryClassifiesReaderPingTransient(t *testing.T) {
	err := privateFailure(failureReaderPing, errors.Join(ErrReaderUnavailable, context.DeadlineExceeded))
	summary := OpenFailureSummary(err)
	for _, want := range []string{
		"site=reader-ping",
		"class=transient",
		"unavailable=true",
		"deadline-exceeded=true",
		"wrong-topology=false",
		"canceled=false",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing token %q", summary, want)
		}
	}
}

// TestOpenFailureSummaryClassifiesWrongTopologyFatal pins the fatal-fast
// branch: a topology mismatch beside the unavailable sentinel is fatal,
// not transient.
func TestOpenFailureSummaryClassifiesWrongTopologyFatal(t *testing.T) {
	err := privateFailure(failureReaderPing, errors.Join(ErrReaderUnavailable, ErrWrongTopology))
	summary := OpenFailureSummary(err)
	for _, want := range []string{
		"site=reader-ping",
		"class=fatal",
		"unavailable=true",
		"wrong-topology=true",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing token %q", summary, want)
		}
	}
}

// TestOpenFailureSummaryCountsUnwrapDepth checks the depth token on a
// single chain: privateError -> a -> b -> sentinel is four levels.
func TestOpenFailureSummaryCountsUnwrapDepth(t *testing.T) {
	err := privateFailure(failureWriterIdentity,
		fmt.Errorf("a: %w", fmt.Errorf("b: %w", ErrReaderUnavailable)))
	summary := OpenFailureSummary(err)
	if !strings.Contains(summary, "site=writer-identity") {
		t.Errorf("summary %q missing site token", summary)
	}
	if !strings.Contains(summary, "depth=4") {
		t.Errorf("summary %q missing depth=4", summary)
	}
}

// TestOpenFailureSummaryHandlesNil documents the nil input shape so test
// call sites can log unconditionally.
func TestOpenFailureSummaryHandlesNil(t *testing.T) {
	summary := OpenFailureSummary(nil)
	if !strings.Contains(summary, "site=none") {
		t.Errorf("nil summary %q missing site=none", summary)
	}
}

// TestOpenFailureSummaryKeepsMetadataPrivate is the redaction half of the
// #8486 contract: a DSN-shaped cause must not surface any of its text in
// the summary under any fmt verb.
func TestOpenFailureSummaryKeepsMetadataPrivate(t *testing.T) {
	cause := errors.New("postgres://reader:private_password_9z@standby-7.internal:5433/postgres?sslmode=require")
	summary := OpenFailureSummary(privateFailure(failureReaderPing,
		errors.Join(ErrReaderUnavailable, cause)))
	for _, secret := range []string{"private_password_9z", "standby-7.internal"} {
		if strings.Contains(summary, secret) {
			t.Errorf("summary exposed backend metadata: %q", summary)
		}
	}
	for _, format := range []string{"%v", "%s", "%+v", "%#v"} {
		if got := fmt.Sprintf(format, summary); strings.Contains(got, "private_") {
			t.Errorf("format %s exposed backend metadata: %s", format, got)
		}
	}
}

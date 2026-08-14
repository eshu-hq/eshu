// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import (
	"fmt"
	"strings"
	"testing"
)

// passwordAssignmentCarriers are the password assignments the colon rule has to
// catch. The first four are the original hole ("password=" was listed, the YAML
// and log-line spelling was not); the rest are the two shapes the first version
// of that rule still walked past -- a single-quoted or escaped key, and the
// snake_case env key that is how a password is actually spelled in a config
// dump or a log line.
var passwordAssignmentCarriers = []string{
	"password: S3NT1NEL",
	"password:S3NT1NEL",
	`"password": "S3NT1NEL"`,
	"  password:  S3NT1NEL",
	`'password': 'S3NT1NEL'`,
	`'password':'S3NT1NEL'`,
	`\"password\": \"S3NT1NEL\"`,
	"POSTGRES_PASSWORD: hunter2",
	"DB_PASSWORD: hunter2",
	"my_password: hunter2",
	"neo4j_password: S3NT1NEL",
	`{"db_password": "S3NT1NEL"}`,
}

// TestUnsafeStringRejectsColonSpelledPasswordAssignment pins the third hole.
func TestUnsafeStringRejectsColonSpelledPasswordAssignment(t *testing.T) {
	t.Parallel()

	for _, value := range passwordAssignmentCarriers {
		t.Run(value, func(t *testing.T) {
			if !UnsafeString(value) {
				t.Fatalf("UnsafeString(%q) = false, want true", value)
			}
		})
	}
}

// TestUnsafeStringScreensPasswordKeysByTheirValue states the false-positive
// boundary the widened password rule draws, in one table, because the two
// halves only mean something together: the key alone does not decide, the value
// does. A rule that rejected every "password:" line would pass a table of
// credentials, and a rule that rejected none would pass a table of schemas.
func TestUnsafeStringScreensPasswordKeysByTheirValue(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		// Assigned a credential.
		"password: hunter2":      true,
		"db_password: hunter2":   true,
		"password: correcthorse": true,
		`"password": "S3NT1NEL"`: true,
		// Assigned a type: an answer about a schema, not a credential.
		"password: string":         false,
		"password: String!":        false,
		"password: string | null":  false,
		"password: Option<String>": false,
		"password: SecretStr":      false,
		"password: varchar(255)":   false,
		"password: []byte":         false,
		"password: *string":        false,
		"user_password: boolean":   false,
		"password: null":           false,
		// Assigned a count. random_password is a real Terraform resource, so
		// this is the same shape as "aws_secretsmanager_secret: 5 resources".
		"random_password: 3 resources":  false,
		"random_password: 12 resources": false,
		// Assigned a placeholder: punctuation, naming no secret.
		"password: <redacted>":     false,
		"password: ${DB_PASSWORD}": false,
		"password: ***":            false,
		// Two assignments in one string. The classifier reads the VALUE, so it
		// has to read every assignment the key rule matches: a screen that
		// decides on the leftmost one publishes a credential that follows an
		// honest declaration, and swapping the two must not change the answer.
		"password: string, password: hunter2": true,
		"password: hunter2, password: string": true,
		// The same pair with no space or comma between the two assignments.
		// Every separator here is a character the capture class stops at, so one
		// FindAllStringSubmatch pass sees both: the separator is still sitting
		// there to serve as the next match's left boundary.
		//
		// These rows guard that multi-match scan, NOT the capture class. Widening
		// the class back leaves them all green -- the swallowed run
		// "string;password:hunter2" is not a declaration value, so the classifier
		// still answers true. They go red when the scan is cut to the leftmost
		// match, which is row 5 of the mutation table in
		// docs/internal/evidence/publish-safety-screen-shape-coverage.md.
		//
		// The last row is the swap control: its leftmost assignment is the
		// credential, so it passed before this fix and only in that order.
		"password:string;password:hunter2":  true,
		"password:int|password:hunter2":     true,
		"password:3;password:hunter2":       true,
		"password:string:password:hunter2":  true,
		"password:string(password:hunter2)": true,
		"password:hunter2;password:string":  true,
		// The negative control for the pairs above. Scanning every assignment
		// must not degrade into rejecting any string with two of them.
		"password: string, confirm_password: string": false,
		// No value at all.
		"the field is named password:": false,
		// Accepted miss, stated in the README: a key with no separator before
		// the word is shape-identical to checkPassword.
		"PGPASSWORD: hunter2": false,
	} {
		t.Run(value, func(t *testing.T) {
			if got := UnsafeString(value); got != want {
				t.Fatalf("UnsafeString(%q) = %v, want %v", value, got, want)
			}
		})
	}
}

// BenchmarkUnsafeStringPasswordGateOpen measures the one string shape that pays
// for the password rule's value classification: an honest answer that carries
// the word "password", so the substring gate opens and the regex plus the
// classifier both run to completion before the value is called safe. Every
// other honest string skips all of it on the gate. Terraform's random_password
// is the real resource that makes this shape worth keeping publishable.
//
// It is a 42-byte string with ONE assignment, so it measures the fixed cost and
// nothing about how the scan grows. BenchmarkUnsafeStringPasswordRunTogetherScale
// is the one that measures growth; keep both.
func BenchmarkUnsafeStringPasswordGateOpen(b *testing.B) {
	const value = "random_password: 3 resources in the module"
	for i := 0; i < b.N; i++ {
		if UnsafeString(value) {
			b.Fatalf("%q is not publish-safe; the benchmark is measuring the wrong path", value)
		}
	}
}

// passwordRunTogetherInput builds size bytes of back-to-back password
// assignments carrying none of whitespace, a quote, or a comma. That is the
// worst case for this rule: every assignment matches the key rule, every value
// is honest so the scan never short-circuits, and no separator lets a scan
// treat the rest of the string as one value.
//
// A scan that re-ran the regex from each assignment's value instead of
// continuing past the match cost 3.34ms here at 4KB and 12.3s at 128KB. The
// input reaching this rule is not capped -- ask_sse.go screens the whole joined
// model answer, and answerquality screens evidence values taken from indexed
// repository content -- so growth is the property worth pinning, not the fixed
// cost.
func passwordRunTogetherInput(size int) string {
	const unit = "password:string;"
	return strings.Repeat(unit, size/len(unit)+1)[:size]
}

// BenchmarkUnsafeStringPasswordRunTogetherScale pins that the scan stays linear
// in input length. Read it across the sizes, not as one number: a scan that is
// quadratic in the assignment count passes any single size and fails the ratio
// between two.
func BenchmarkUnsafeStringPasswordRunTogetherScale(b *testing.B) {
	for _, size := range []int{4096, 32768, 131072} {
		value := passwordRunTogetherInput(size)
		if !strings.Contains(value, "password") {
			b.Fatalf("size %d built no assignment; the benchmark is measuring the wrong path", size)
		}
		if passwordAssignmentIsUnsafe(value) {
			b.Fatalf("size %d input is not publish-safe, so the scan short-circuits "+
				"and the benchmark is measuring the wrong path", size)
		}
		// The claim is about the assignment COUNT, and neither guard above pins
		// it: one assignment padded out to size satisfies both, leaving the
		// benchmark green while it measures nothing about growth. The unit is 16
		// bytes, so size/32 is half the real count and leaves room for the
		// generator to change shape without going red for the wrong reason.
		if n := len(passwordAssignmentPattern.FindAllStringSubmatchIndex(value, -1)); n < size/32 {
			b.Fatalf("size %d built %d assignments, want at least %d; the input is "+
				"padded rather than back-to-back, so the benchmark is measuring the wrong path",
				size, n, size/32)
		}
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				if passwordAssignmentIsUnsafe(value) {
					b.Fatal("input classified unsafe mid-benchmark")
				}
			}
		})
	}
}

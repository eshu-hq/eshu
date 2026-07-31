// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
)

// TestResourceAddressKeyStripsModulePrefixes proves resourceAddressKey
// recovers the "TYPE.NAME" segment of a canonical Terraform address by
// FRONT-stripping leading "module.<name>[<index>]." pairs (not taking the
// last two dot-separated segments — reviewer-supplied empirical proof showed
// that end-taking shape collapsing
// `aws_route53_record.this["api.example.com"]` and
// `aws_acm_certificate.cert["www.example.com"]` to the identical, wrong key
// "example.com\"]", and colliding `data.aws_ami.ubuntu` with the unrelated
// managed resource `aws_ami.ubuntu`) and then stripping any trailing
// "[INDEX]" instance suffix.
//
// The trailing strip is required for a SEPARATE reason a second reviewer
// found: config-side rows never carry a per-instance index at all (the
// parser has no state to know how many instances a `count`/`for_each` block
// produces), while state-side rows for an indexed resource DO
// (internal/collector/terraformstate/identity.go emits "[index:<N>]" or
// "[key:<hash>]"). Without stripping, a config-only key like
// "aws_instance.web" never equals a state-only "aws_instance.web[index:0]"
// -- pairSpuriousModuleMismatches was a silent no-op for every
// count/for_each resource. Stripping makes the keys equal again; the
// unambiguity guard in pairSpuriousModuleMismatches then decides whether
// that's safe to act on (see tfstate_drift_evidence_pairing_cardinality_test.go's
// TestPairSpuriousModuleMismatches* tests for the count>1 vs count=1
// distinction; those live in a sibling file to keep this one under the
// CLAUDE.md 500-line cap).
func TestResourceAddressKeyStripsModulePrefixes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		address string
		want    string
		wantOK  bool
	}{
		{
			name:    "plain root resource",
			address: "aws_security_group.vpc_endpoints",
			want:    "aws_security_group.vpc_endpoints",
			wantOK:  true,
		},
		{
			name:    "single module prefix",
			address: "module.vpc.aws_security_group.vpc_endpoints",
			want:    "aws_security_group.vpc_endpoints",
			wantOK:  true,
		},
		{
			name:    "nested module prefix",
			address: "module.platform.module.vpc.aws_instance.web",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "numeric count index, no dots, gets stripped",
			address: "aws_instance.web[0]",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "collector-shaped count index, no module prefix",
			address: "aws_instance.web[index:0]",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "collector-shaped count index, with module prefix",
			address: "module.x.aws_instance.web[index:1]",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "collector-shaped for_each hash key, with module prefix",
			address: "module.x.aws_instance.web[key:9f86d081884c7d659a2feaa0c55ad015]",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "unindexed resource of the same type/name equals its indexed sibling's stripped key",
			address: "module.x.aws_instance.web",
			want:    "aws_instance.web",
			wantOK:  true,
		},
		{
			name:    "for_each key containing dots gets stripped, no module prefix",
			address: `aws_route53_record.this["api.example.com"]`,
			want:    `aws_route53_record.this`,
			wantOK:  true,
		},
		{
			name:    "for_each key containing dots gets stripped, with module prefix",
			address: `module.dns.aws_route53_record.this["api.example.com"]`,
			want:    `aws_route53_record.this`,
			wantOK:  true,
		},
		{
			name:    "different resource, same dotted-index tail, must not collapse even after stripping",
			address: `aws_acm_certificate.cert["www.example.com"]`,
			want:    `aws_acm_certificate.cert`,
			wantOK:  true,
		},
		{
			name:    "data source keeps its data. token, distinct from a managed resource",
			address: "data.aws_ami.ubuntu",
			want:    "data.aws_ami.ubuntu",
			wantOK:  true,
		},
		{
			name:    "managed resource sharing a data source's type and name",
			address: "aws_ami.ubuntu",
			want:    "aws_ami.ubuntu",
			wantOK:  true,
		},
		{
			name:    "indexed module name whose own index contains a dot",
			address: `module.vpc["a.b"].aws_security_group.x`,
			want:    "aws_security_group.x",
			wantOK:  true,
		},
		{
			name:    "indexed module name AND an indexed resource combined",
			address: `module.vpc["a.b"].aws_security_group.x[index:2]`,
			want:    "aws_security_group.x",
			wantOK:  true,
		},
		{
			name:    "empty address",
			address: "",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "single segment, no type.name shape",
			address: "aws_instance",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "module name consumes the whole address, no trailing resource",
			address: "module.vpc",
			want:    "",
			wantOK:  false,
		},
		{
			name:    "unterminated bracket refuses to guess",
			address: `module.vpc["unterminated.aws_instance.web`,
			want:    "",
			wantOK:  false,
		},
		{
			name:    "unterminated trailing index bracket refuses to guess",
			address: `aws_instance.web[index:0`,
			want:    "",
			wantOK:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resourceAddressKey(tc.address)
			if ok != tc.wantOK {
				t.Fatalf("resourceAddressKey(%q) ok = %v, want %v", tc.address, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("resourceAddressKey(%q) = %q, want %q", tc.address, got, tc.want)
			}
		})
	}

	// Explicit non-collision assertions mirroring the reviewer's empirical
	// proof of the old end-taking bug: these two pairs MUST resolve to
	// different keys, or pairSpuriousModuleMismatches's "unambiguous 1:1"
	// guard would see a false-unambiguous match and mirror a
	// ModuleResolutionReason onto a genuinely unrelated resource.
	t.Run("dotted for_each keys of different resources never collide", func(t *testing.T) {
		t.Parallel()
		key1, ok1 := resourceAddressKey(`aws_route53_record.this["api.example.com"]`)
		key2, ok2 := resourceAddressKey(`aws_acm_certificate.cert["www.example.com"]`)
		if !ok1 || !ok2 {
			t.Fatalf("resourceAddressKey() ok = (%v, %v), want (true, true)", ok1, ok2)
		}
		if key1 == key2 {
			t.Fatalf("collision: both resolved to %q", key1)
		}
	})
	t.Run("data source never collides with a managed resource of the same type and name", func(t *testing.T) {
		t.Parallel()
		key1, ok1 := resourceAddressKey("data.aws_ami.ubuntu")
		key2, ok2 := resourceAddressKey("aws_ami.ubuntu")
		if !ok1 || !ok2 {
			t.Fatalf("resourceAddressKey() ok = (%v, %v), want (true, true)", ok1, ok2)
		}
		if key1 == key2 {
			t.Fatalf("collision: both resolved to %q", key1)
		}
	})
	// Two DIFFERENT for_each instances of the SAME resource must strip to
	// the SAME key (that's the whole point -- it lets
	// pairSpuriousModuleMismatches's ambiguity guard correctly see "2 state
	// instances share this key" and refuse to attribute the mismatch to
	// either one specifically).
	t.Run("two instances of the same indexed resource strip to the same key", func(t *testing.T) {
		t.Parallel()
		key1, ok1 := resourceAddressKey("module.x.aws_instance.web[index:1]")
		key2, ok2 := resourceAddressKey("module.x.aws_instance.web[index:2]")
		if !ok1 || !ok2 {
			t.Fatalf("resourceAddressKey() ok = (%v, %v), want (true, true)", ok1, ok2)
		}
		if key1 != key2 {
			t.Fatalf("key1 = %q, key2 = %q, want equal (same resource, different instances)", key1, key2)
		}
	})
}

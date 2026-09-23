// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestImageTagsArePseudonymized (P1): a customer-chosen image tag is
// customer data; "latest", pure semver and digests are structural and kept.
func TestImageTagsArePseudonymized(t *testing.T) {
	key := mustKey(t, keyA)
	host := acct + ".dkr.ecr.us-east-1.amazonaws.com/"
	pseudoHost := `^` + pseudoAcct + `\.dkr\.ecr\.us-east-1\.amazonaws\.com/` + hexName
	cases := []struct{ ref, want string }{
		{host + "payments-api:jdoe-hotfix", pseudoHost + `:` + hexName + `$`},
		{host + "payments-api:release-2026-06-acme", pseudoHost + `:` + hexName + `$`},
		{host + "payments-api:rc1", pseudoHost + `:` + hexName + `$`}, // below the free-text floor: rewritten via the path:tag composite
		{host + "payments-api:latest", pseudoHost + `:latest$`},
		{host + "payments-api:v1.2.3", pseudoHost + `:v1\.2\.3$`},
		{host + "payments-api:1.2.3", pseudoHost + `:1\.2\.3$`},
		{host + "payments-api@sha256:" + strings.Repeat("ab", 32), pseudoHost + `@sha256:` + strings.Repeat("ab", 32) + `$`},
		{host + "payments-api:jdoe-hotfix@sha256:" + strings.Repeat("ab", 32), pseudoHost + `:` + hexName + `@sha256:` + strings.Repeat("ab", 32) + `$`},
	}
	for _, tc := range cases {
		got := pseudonymOf(t, key, "resolved_image_uri", tc.ref)
		mustMatch(t, "image ref", got, tc.want)
		if strings.Contains(got, "jdoe") || strings.Contains(got, "acme") {
			t.Errorf("customer tag survived in shape %q", shapeOf(got))
		}
	}
	// The ECR image-reference fact carries the tag in its own field too.
	if got := pseudonymOf(t, key, "tag", "jdoe-hotfix"); !regexp.MustCompile(`^` + hexName + `$`).MatchString(got) {
		t.Errorf("tag field shape %q, want a name pseudonym", shapeOf(got))
	}
	for _, kept := range []string{"latest", "v1.2.3", "1.2.3", "7"} {
		if got := pseudonymOf(t, key, "tag", kept); got != kept {
			t.Errorf("structural tag %q was rewritten to shape %q", kept, shapeOf(got))
		}
	}
	// The tag pseudonym is the same in the ref and in the tag field.
	ref := pseudonymOf(t, key, "resolved_image_uri", host+"payments-api:jdoe-hotfix")
	if !strings.HasSuffix(ref, ":"+pseudonymOf(t, key, "tag", "jdoe-hotfix")) {
		t.Errorf("tag pseudonym differs between the image ref and the tag field")
	}
}

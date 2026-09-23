// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// Review finding F3 on 40a4ac36e (verdict-p3.md). Every literal is synthetic.

// TestMetadataUnknownKeyIsOpaque (F3): scope metadata goes through the
// policy like a payload -- an unlisted key is made opaque and reported.
func TestMetadataUnknownKeyIsOpaque(t *testing.T) {
	key := mustKey(t, keyA)
	wrapped, report, err := recordpseudo.Wrap(&sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:eks", map[string]string{"account_id": acct, "region": "us-east-1", "cluster_name": "acme-prod-cluster"}, map[string]any{"name": "x"}),
	}}, key, recordpolicy.Policy())
	if err != nil {
		t.Fatal(err)
	}
	gen, _, err := wrapped.Next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for range gen.Facts {
	}
	if got := gen.Scope.Metadata["cluster_name"]; !regexp.MustCompile(`^o[0-9a-f]{11}$`).MatchString(got) {
		t.Errorf("unknown metadata key has value shape %q, want opaque", shapeOf(got))
	}
	if gen.Scope.Metadata["region"] != "us-east-1" || !regexp.MustCompile(`^0000[0-9]{8}$`).MatchString(gen.Scope.Metadata["account_id"]) {
		t.Errorf("classified metadata was not handled by class: %v", shapeOf(fmt.Sprint(gen.Scope.Metadata)))
	}
	if strings.Join(report.UnclassifiedPaths, ",") != "scope.metadata.cluster_name" {
		t.Errorf("unclassified paths = %v", report.UnclassifiedPaths)
	}
}

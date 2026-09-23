// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducerawscloud "github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// shape is what the reducer extractors see: the exact inputs the graph
// writers MERGE. The numbers asserted below are the design's (#6965 Phase 3,
// shim run 3); if one differs, the assertion is NOT adjusted -- the
// discrepancy is reported.
type shape struct {
	nodes        int
	nodeTypes    map[string]int
	distinctUIDs int
	relRows      map[string]int
	imageRows    map[string]int
	usesRows     int
	usesEnvs     map[string]int
	canPerform   int
	canPerformBy map[string]int
	ciDecisions  int
}

func loadAll(t *testing.T, path string) ([][]facts.Envelope, []string) {
	t.Helper()
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatal(err)
	}
	return drainSource(t, src)
}

func drainSource(t *testing.T, src collector.Source) ([][]facts.Envelope, []string) {
	t.Helper()
	var gens [][]facts.Envelope
	var ids []string
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return gens, ids
		}
		var envs []facts.Envelope
		for e := range gen.Facts {
			envs = append(envs, e)
		}
		gens = append(gens, envs)
		ids = append(ids, gen.Scope.ScopeID)
	}
}

func measure(t *testing.T, gens [][]facts.Envelope, scopeIDs []string) shape {
	t.Helper()
	sh := shape{nodeTypes: map[string]int{}, relRows: map[string]int{}, imageRows: map[string]int{}, usesEnvs: map[string]int{}, canPerformBy: map[string]int{}}
	var all, allRes, allPerm []facts.Envelope
	uids := map[string]struct{}{}
	for gi, envs := range gens {
		all = append(all, envs...)
		var res, rel []facts.Envelope
		for _, e := range envs {
			switch e.FactKind {
			case facts.AWSResourceFactKind:
				res = append(res, e)
				allRes = append(allRes, e)
			case facts.AWSRelationshipFactKind:
				rel = append(rel, e)
			case facts.AWSIAMPermissionFactKind:
				allPerm = append(allPerm, e)
			}
		}
		rows, quarantined, err := reducer.ExtractCloudResourceNodeRows(res)
		if err != nil || len(quarantined) != 0 {
			t.Fatalf("ExtractCloudResourceNodeRows: err=%v quarantined=%d", err, len(quarantined))
		}
		sh.nodes += len(rows)
		for _, r := range rows {
			sh.nodeTypes[fmt.Sprint(r["resource_type"])]++
			uids[fmt.Sprint(r["uid"])] = struct{}{}
		}
		relRows, _, relQ, err := reducer.ExtractAWSRelationshipEdgeRows(res, rel, scopeIDs[gi])
		if err != nil || len(relQ) != 0 {
			t.Fatalf("ExtractAWSRelationshipEdgeRows: err=%v quarantined=%d", err, len(relQ))
		}
		for _, r := range relRows {
			sh.relRows[fmt.Sprint(r["relationship_type"])+"|"+fmt.Sprint(r["resolution_mode"])]++
		}
		imageRows, _, imageQ, err := reducerawscloud.ExtractAWSCloudImageEdgeRows(res, rel)
		if err != nil || len(imageQ) != 0 {
			t.Fatalf("ExtractAWSCloudImageEdgeRows: err=%v quarantined=%d", err, len(imageQ))
		}
		for _, r := range imageRows {
			sh.imageRows[fmt.Sprint(r["relationship_type"])+"|"+fmt.Sprint(r["resolution_mode"])]++
		}
	}
	sh.distinctUIDs = len(uids)
	usesRows, _, usesQ, err := reducer.ExtractWorkloadCloudRelationshipRows(all)
	if err != nil || len(usesQ) != 0 {
		t.Fatalf("ExtractWorkloadCloudRelationshipRows: err=%v quarantined=%d", err, len(usesQ))
	}
	sh.usesRows = len(usesRows)
	for _, r := range usesRows {
		sh.usesEnvs[fmt.Sprint(r["environment"])]++
	}
	canPerform, err := iamcan.ExtractIAMCanPerformEdges(allRes, allPerm)
	if err != nil {
		t.Fatalf("ExtractIAMCanPerformEdges: %v", err)
	}
	sh.canPerform = len(canPerform.Edges)
	sh.canPerformBy = canPerform.EdgesByMode
	sh.ciDecisions = len(containerimage.BuildContainerImageIdentityDecisions(all))
	return sh
}

func TestAWSCorpusShapePreserved(t *testing.T) {
	rawPath := filepath.Join(corpusDir, "awscloud", "supply-chain-demo.json")
	key := mustKey(t, keyA)
	pseudoPath := filepath.Join(t.TempDir(), "pseudo.json")
	recordBytes(t, pseudoPath, committedSource(t, "awscloud"), key)

	rawGens, rawIDs := loadAll(t, rawPath)
	pseudoGens, pseudoIDs := loadAll(t, pseudoPath)
	raw := measure(t, rawGens, rawIDs)
	pseudo := measure(t, pseudoGens, pseudoIDs)

	// The design's exact numbers, asserted on BOTH sides.
	wantTypes := map[string]int{"aws_ec2_ami": 1, "aws_iam_role": 1, "aws_s3_bucket": 1, "ecr.repository": 1, "ecs.service": 1, "ecs.task": 1, "ecs.task_definition": 1, "lambda.function": 3}
	for label, sh := range map[string]shape{"raw": raw, "pseudonymized": pseudo} {
		if sh.nodes != 10 || sh.distinctUIDs != 10 {
			t.Errorf("%s: CloudResource rows=%d distinct uids=%d, want 10/10", label, sh.nodes, sh.distinctUIDs)
		}
		if fmt.Sprint(sh.nodeTypes) != fmt.Sprint(wantTypes) {
			t.Errorf("%s: resource_type multiset=%v, want %v", label, sh.nodeTypes, wantTypes)
		}
		if fmt.Sprint(sh.relRows) != fmt.Sprint(map[string]int{"ec2_instance_uses_ami|bare_id": 1}) {
			t.Errorf("%s: relationship rows=%v, want ec2_instance_uses_ami|bare_id:1", label, sh.relRows)
		}
		if fmt.Sprint(sh.imageRows) != fmt.Sprint(map[string]int{"lambda_function_uses_image|container_image_digest": 1}) {
			t.Errorf("%s: image rows=%v, want lambda_function_uses_image|container_image_digest:1", label, sh.imageRows)
		}
		if sh.usesRows != 2 || fmt.Sprint(sh.usesEnvs) != fmt.Sprint(map[string]int{"prod": 1, "stage": 1}) {
			t.Errorf("%s: USES rows=%d envs=%v, want 2 (prod 1, stage 1)", label, sh.usesRows, sh.usesEnvs)
		}
		if sh.canPerform != 1 || fmt.Sprint(sh.canPerformBy) != fmt.Sprint(map[string]int{"exact_arn": 1}) {
			t.Errorf("%s: CAN_PERFORM=%d by mode %v, want 1 exact_arn", label, sh.canPerform, sh.canPerformBy)
		}
		if sh.ciDecisions != 2 {
			t.Errorf("%s: container-image identity decisions=%d, want 2", label, sh.ciDecisions)
		}
	}

	// Fact-level bijection: 22 facts in 7 scopes, paired 1:1 by pseudonymized
	// scope id and stable key with the same fact kind, 22 distinct keys.
	wrapped, _, err := recordpseudo.Wrap(committedSource(t, "awscloud"), key, recordpolicy.Policy())
	if err != nil {
		t.Fatal(err)
	}
	mappedGens, mappedIDs := drainSource(t, wrapped)
	byScope := map[string]map[string]facts.Envelope{}
	pseudoKeys := map[string]int{}
	for gi := range pseudoGens {
		m := map[string]facts.Envelope{}
		for _, e := range pseudoGens[gi] {
			m[e.StableFactKey] = e
			pseudoKeys[e.StableFactKey]++
		}
		byScope[pseudoIDs[gi]] = m
	}
	paired, total, rawKeys := 0, 0, map[string]int{}
	for gi := range rawGens {
		if len(mappedGens[gi]) != len(rawGens[gi]) {
			t.Fatalf("scope %d: %d raw facts, %d mapped", gi, len(rawGens[gi]), len(mappedGens[gi]))
		}
		m, ok := byScope[mappedIDs[gi]]
		if !ok {
			t.Fatalf("raw scope %d has no recorded counterpart", gi)
		}
		for fi, e := range rawGens[gi] {
			total++
			rawKeys[e.StableFactKey]++
			pe, ok := m[mappedGens[gi][fi].StableFactKey]
			if !ok {
				t.Fatalf("raw fact %d/%d has no recorded counterpart", gi, fi)
			}
			if pe.FactKind != e.FactKind {
				t.Fatalf("fact kind drift on %d/%d", gi, fi)
			}
			paired++
		}
	}
	if total != 22 || paired != 22 || len(rawKeys) != 22 || len(pseudoKeys) != 22 || len(rawGens) != 7 || len(pseudoGens) != 7 {
		t.Errorf("facts=%d paired=%d rawKeys=%d pseudoKeys=%d scopes raw=%d pseudo=%d; want 22/22/22/22/7/7", total, paired, len(rawKeys), len(pseudoKeys), len(rawGens), len(pseudoGens))
	}
}

// TestAWSCorpusSiblingsJoin: the ociregistry and terraformstate cassettes
// rewritten with the SAME dictionary still carry the two join literals the
// reducers match against the awscloud recording -- the lambda image's
// oci-descriptor uid and the ec2 instance ARN.
func TestAWSCorpusSiblingsJoin(t *testing.T) {
	key := mustKey(t, keyA)
	pseudoPath := filepath.Join(t.TempDir(), "pseudo.json")
	recordBytes(t, pseudoPath, committedSource(t, "awscloud"), key)
	pseudoGens, _ := loadAll(t, pseudoPath)
	var resolvedImage, ec2ARN string
	for _, envs := range pseudoGens {
		for _, e := range envs {
			if attrs, ok := e.Payload["attributes"].(map[string]any); ok {
				if v, ok := attrs["resolved_image_uri"].(string); ok {
					resolvedImage = v
				}
			}
			if e.FactKind == facts.AWSResourceFactKind && e.Payload["resource_type"] == "aws_ec2_instance" {
				ec2ARN, _ = e.Payload["arn"].(string)
			}
		}
	}
	if resolvedImage == "" || ec2ARN == "" {
		t.Fatal("pseudonymized corpus lost the lambda image uri or the ec2 instance ARN")
	}
	before, digest, _ := strings.Cut(resolvedImage, "@")
	wantDescriptor := "oci-descriptor://" + strings.ToLower(before) + "@" + digest

	// Same-dictionary substitution of the sibling cassettes: wrap them under
	// the same key with a policy that classifies their identity fields.
	siblingPolicy := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{
		"registry": recordpseudo.ClassHost, "repository": recordpseudo.ClassIdent, "descriptor_id": recordpseudo.ClassIdent,
		"image_ref": recordpseudo.ClassECRRef, "reference": recordpseudo.ClassECRRef, "arn": recordpseudo.ClassARN,
		"image_uri": recordpseudo.ClassECRRef, "id": recordpseudo.ClassIdent, "resource_id": recordpseudo.ClassIdent,
	}}
	for _, sibling := range []string{"ociregistry", "terraformstate"} {
		raw, err := os.ReadFile(filepath.Join(corpusDir, sibling, "supply-chain-demo.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "123456789012") {
			t.Fatalf("%s no longer carries the documentation account; the join fixture changed", sibling)
		}
		rewritten := sameDictionary(t, key, siblingPolicy, string(raw))
		switch sibling {
		case "ociregistry":
			if !strings.Contains(rewritten, wantDescriptor) {
				t.Errorf("lambda->ContainerImage join uid is absent from the rewritten ociregistry cassette")
			}
		case "terraformstate":
			if !strings.Contains(rewritten, ec2ARN) {
				t.Errorf("ec2 instance ARN is absent from the rewritten terraformstate cassette")
			}
		}
	}
}

// sameDictionary substitutes the awscloud dictionary into text: it wraps the
// awscloud cassette to learn under key, then wraps a sibling document whose
// only string is text so the same tokens are rewritten.
func sameDictionary(t *testing.T, key recordpseudo.Key, policy recordpseudo.Policy, text string) string {
	t.Helper()
	joined := recordpseudo.Policy{Fields: map[string]recordpseudo.Class{"document": recordpseudo.ClassKeep}}
	for k, v := range recordpolicy.Policy().Fields {
		joined.Fields[k] = v
	}
	for k, v := range policy.Fields {
		joined.Fields[k] = v
	}
	// Learn from the awscloud corpus, then substitute in the sibling text via
	// a Keep field: Keep values are substituted but never learned.
	awsGens, _ := loadAll(t, filepath.Join(corpusDir, "awscloud", "supply-chain-demo.json"))
	var gens []collector.CollectedGeneration
	for _, envs := range awsGens {
		payloads := make([]map[string]any, 0, len(envs))
		for _, e := range envs {
			payloads = append(payloads, e.Payload)
		}
		gens = append(gens, generation("aws:corpus", nil, payloads...))
	}
	gens = append(gens, generation("sibling", nil, map[string]any{"document": text}))
	payloads, _ := wrapAll(t, &sliceSource{gens: gens}, key, joined)
	out, _ := payloads[len(payloads)-1]["document"].(string)
	return out
}

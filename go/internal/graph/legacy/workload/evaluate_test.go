// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

import (
	"reflect"
	"strings"
	"testing"
)

func cand(name, repo string) Candidate {
	return Candidate{ID: "workload:" + name, Name: name, RepoID: repo}
}

func definesFromRepo(repo string) Relationship {
	return Relationship{Type: "DEFINES", Outgoing: false, OtherID: repo, OtherLabels: []string{"Repository"}}
}

func sameName() Relationship {
	return Relationship{Type: "SAME_NAME", Outgoing: true, OtherID: "workload:repository:other:svc", OtherLabels: []string{"Workload"}}
}

func twinFacts(c Candidate) map[string][]string {
	return map[string][]string{TwinID(c.RepoID, c.Name): {c.RepoID}}
}

func TestEvaluateClassifiesEveryCase(t *testing.T) {
	p1 := cand("p1", "repository:a")
	n2 := cand("n2", "repository:c")
	n2b := cand("n2b", "repository:c2")
	n3 := cand("n3", "repository:d")
	n4 := cand("n4", "repository:d")
	n5 := cand("n5", "repository:f")
	n5b := cand("n5b", "repository:g")
	n6 := cand("n6", "repository:h")
	n7 := cand("n7", "repository:i")

	facts := Facts{
		Candidates:    []Candidate{p1, n2, n2b, n3, n4, n5, n5b, n6, n7},
		EdgeInstances: map[string]bool{n2.ID: true},
		PropInstances: map[string]bool{n2b.ID: true},
		Twins: map[string][]string{
			TwinID(p1.RepoID, p1.Name):   {p1.RepoID},
			TwinID(n2.RepoID, n2.Name):   {n2.RepoID},
			TwinID(n2b.RepoID, n2b.Name): {n2b.RepoID},
			// n4's twin lives in a different repository: not a twin.
			TwinID("repository:e", n4.Name): {"repository:e"},
			TwinID(n5.RepoID, n5.Name):      {n5.RepoID},
			TwinID(n5b.RepoID, n5b.Name):    {n5b.RepoID},
			// n6's twin exists but the repository does not define it.
			TwinID(n6.RepoID, n6.Name): {"repository:someone-else"},
			TwinID(n7.RepoID, n7.Name): {n7.RepoID},
		},
		Relationships: map[string][]Relationship{
			p1.ID:  {definesFromRepo(p1.RepoID), sameName()},
			n5.ID:  {definesFromRepo(n5.RepoID), {Type: "DOCUMENTS", OtherID: "repository:doc", OtherLabels: []string{"Repository"}}},
			n5b.ID: {definesFromRepo(n5b.RepoID), {Type: "DEPENDS_ON", Outgoing: true, OtherID: "workload:repository:g:n5b", OtherLabels: []string{"Workload"}}},
			// n7 has a DEFINES from a repository that is not its own repo_id.
			n7.ID: {definesFromRepo("repository:not-mine")},
		},
	}

	got := map[string]Verdict{}
	for _, v := range Evaluate(facts) {
		got[v.ID] = v
	}

	want := map[string]struct {
		eligible bool
		reason   string
		rels     map[string]int
	}{
		p1.ID:  {true, BlockedNone, map[string]int{}},
		n2.ID:  {false, BlockedHasInstances, map[string]int{}},
		n2b.ID: {false, BlockedHasInstances, map[string]int{}},
		n3.ID:  {false, BlockedNoTwin, map[string]int{}},
		n4.ID:  {false, BlockedNoTwin, map[string]int{}},
		n5.ID:  {false, BlockedRelationships, map[string]int{"DOCUMENTS": 1}},
		n5b.ID: {false, BlockedRelationships, map[string]int{"DEPENDS_ON": 1}},
		n6.ID:  {false, BlockedNoTwin, map[string]int{}},
		n7.ID:  {false, BlockedRelationships, map[string]int{"DEFINES": 1}},
	}
	if len(got) != len(want) {
		t.Fatalf("verdict count = %d, want %d", len(got), len(want))
	}
	for id, w := range want {
		v := got[id]
		if v.Eligible != w.eligible || v.BlockedReason != w.reason {
			t.Errorf("%s: eligible=%v reason=%q, want eligible=%v reason=%q", id, v.Eligible, v.BlockedReason, w.eligible, w.reason)
		}
		if !reflect.DeepEqual(v.Relationships, w.rels) {
			t.Errorf("%s: relationships = %v, want %v", id, v.Relationships, w.rels)
		}
	}
	if got[p1.ID].TwinID != TwinID(p1.RepoID, p1.Name) {
		t.Errorf("p1 twin id = %q", got[p1.ID].TwinID)
	}
}

func TestEvaluateReasonPrecedenceInstancesBeforeTwinBeforeRelationships(t *testing.T) {
	c := cand("x", "repository:a")
	facts := Facts{
		Candidates:    []Candidate{c},
		EdgeInstances: map[string]bool{c.ID: true},
		Relationships: map[string][]Relationship{c.ID: {{Type: "DOCUMENTS"}}},
	}
	v := Evaluate(facts)[0]
	if v.BlockedReason != BlockedHasInstances {
		t.Fatalf("reason = %q, want has_instances (no twin and a bad edge are also present)", v.BlockedReason)
	}
	facts.EdgeInstances = nil
	if v := Evaluate(facts)[0]; v.BlockedReason != BlockedNoTwin {
		t.Fatalf("reason = %q, want no_twin before relationships", v.BlockedReason)
	}
}

func TestEvaluateBlankRepoOrNameNeverHasTwin(t *testing.T) {
	for _, c := range []Candidate{
		{ID: "workload:x", Name: "x", RepoID: ""},
		{ID: "workload:", Name: "", RepoID: "repository:a"},
	} {
		facts := Facts{Candidates: []Candidate{c}, Twins: map[string][]string{TwinID(c.RepoID, c.Name): {c.RepoID}}}
		if v := Evaluate(facts)[0]; v.Eligible || v.BlockedReason != BlockedNoTwin {
			t.Errorf("%+v: eligible=%v reason=%q, want blocked no_twin", c, v.Eligible, v.BlockedReason)
		}
	}
}

func TestEvaluatePaddedNameOrRepoUsesTrimmedTwin(t *testing.T) {
	// A padded name or repo id resolves to the trimmed twin: TwinID joins the
	// trimmed parts exactly as the delete statement does with trim(), and the
	// own-repository comparisons trim too, so the candidate is eligible.
	for _, c := range []Candidate{
		{ID: "workload: svc", Name: " svc", RepoID: "repository:a"},
		{ID: "workload:svc ", Name: "svc ", RepoID: "repository:a"},
		{ID: "workload:svc", Name: "svc", RepoID: " repository:a "},
		{ID: "workload:svc", Name: "svc", RepoID: "repository:a\t"},
	} {
		trimmedRepo := strings.TrimSpace(c.RepoID)
		trimmedName := strings.TrimSpace(c.Name)
		wantTwin := "workload:" + trimmedRepo + ":" + trimmedName
		if got := TwinID(c.RepoID, c.Name); got != wantTwin {
			t.Errorf("TwinID(%q, %q) = %q, want %q", c.RepoID, c.Name, got, wantTwin)
		}
		facts := Facts{
			Candidates: []Candidate{c},
			Twins:      map[string][]string{wantTwin: {trimmedRepo}},
			Relationships: map[string][]Relationship{c.ID: {
				{Type: "DEFINES", Outgoing: false, OtherID: trimmedRepo, OtherLabels: []string{"Repository"}},
			}},
		}
		if v := Evaluate(facts)[0]; !v.Eligible {
			t.Errorf("%+v: eligible=%v reason=%q rels=%v, want eligible", c, v.Eligible, v.BlockedReason, v.Relationships)
		}
	}
}

func TestEvaluateTrimmedTwinStillNeedsTrimmedRepoMatch(t *testing.T) {
	// The twin lookup trims, but the twin must still be defined by the
	// candidate's own (trimmed) repository: a Twins entry keyed by the trimmed
	// id that lists only the padded repo string is no twin. Dropping the
	// own-repository trim must fail this test.
	c := Candidate{ID: "workload:svc", Name: "svc", RepoID: " repository:a "}
	twin := TwinID(c.RepoID, c.Name)
	if twin == "" {
		t.Fatal("TwinID must resolve a padded repo to the trimmed twin id")
	}
	facts := Facts{Candidates: []Candidate{c}, Twins: map[string][]string{twin: {c.RepoID}}}
	if v := Evaluate(facts)[0]; v.Eligible || v.BlockedReason != BlockedNoTwin {
		t.Errorf("%+v: eligible=%v reason=%q, want blocked no_twin", c, v.Eligible, v.BlockedReason)
	}
	// Trimming does not wave through a foreign repository: a DEFINES edge from
	// a different repo id still blocks, padded or not.
	own := strings.TrimSpace(c.RepoID)
	relFacts := Facts{
		Candidates: []Candidate{c},
		Twins:      map[string][]string{twin: {own}},
		Relationships: map[string][]Relationship{c.ID: {
			{Type: "DEFINES", Outgoing: false, OtherID: "repository:b", OtherLabels: []string{"Repository"}},
		}},
	}
	if v := Evaluate(relFacts)[0]; v.Eligible || v.BlockedReason != BlockedRelationships {
		t.Errorf("%+v: eligible=%v reason=%q, want blocked relationships", c, v.Eligible, v.BlockedReason)
	}
}

func TestSameNameInEitherDirectionIsSafeButOnlyBetweenWorkloads(t *testing.T) {
	c := cand("p", "repository:a")
	facts := Facts{
		Candidates: []Candidate{c},
		Twins:      twinFacts(c),
		Relationships: map[string][]Relationship{c.ID: {
			definesFromRepo(c.RepoID),
			{Type: "SAME_NAME", Outgoing: false, OtherLabels: []string{"Workload"}},
			{Type: "SAME_NAME", Outgoing: true, OtherLabels: []string{"Workload"}},
		}},
	}
	if v := Evaluate(facts)[0]; !v.Eligible {
		t.Fatalf("reason = %q, want eligible", v.BlockedReason)
	}
	facts.Relationships[c.ID] = append(facts.Relationships[c.ID], Relationship{Type: "SAME_NAME", OtherLabels: []string{"Repository"}})
	if v := Evaluate(facts)[0]; v.Eligible || v.BlockedReason != BlockedRelationships {
		t.Fatalf("SAME_NAME to a non-Workload must block, got eligible=%v reason=%q", v.Eligible, v.BlockedReason)
	}
}

func TestDefinesMustComeFromTheOwnRepositoryNode(t *testing.T) {
	c := cand("p", "repository:a")
	for name, rel := range map[string]Relationship{
		"outgoing":         {Type: "DEFINES", Outgoing: true, OtherID: c.RepoID, OtherLabels: []string{"Repository"}},
		"wrong repo":       {Type: "DEFINES", OtherID: "repository:b", OtherLabels: []string{"Repository"}},
		"not a repository": {Type: "DEFINES", OtherID: c.RepoID, OtherLabels: []string{"Workload"}},
	} {
		facts := Facts{Candidates: []Candidate{c}, Twins: twinFacts(c), Relationships: map[string][]Relationship{c.ID: {rel}}}
		if v := Evaluate(facts)[0]; v.Eligible || v.BlockedReason != BlockedRelationships {
			t.Errorf("%s: eligible=%v reason=%q, want blocked relationships", name, v.Eligible, v.BlockedReason)
		}
	}
}

func TestIsLegacyID(t *testing.T) {
	cases := []struct {
		id, name string
		want     bool
	}{
		{"workload:svc", "svc", true},
		{"workload:repository:r:svc", "svc", false},
		{"workload:svc", "other", false},
		{"", "", false},
		{"workload:", "", false},
		{"svc", "svc", false},
		// The detection and delete Cypher compare 'workload:' + w.name = w.id
		// with no trimming or prefix stripping, so neither shape below is a
		// candidate there; the marker must agree.
		{"workload:svc", " svc ", false},
		{"workload:svc", "workload:svc", false},
	}
	for _, c := range cases {
		if got := IsLegacyID(c.id, c.name); got != c.want {
			t.Errorf("IsLegacyID(%q,%q) = %v, want %v", c.id, c.name, got, c.want)
		}
	}
}

func TestTwinID(t *testing.T) {
	if got := TwinID("repository:r", "svc"); got != "workload:repository:r:svc" {
		t.Fatalf("TwinID = %q", got)
	}
	if TwinID("", "svc") != "" || TwinID("repository:r", "") != "" {
		t.Fatal("blank repo or name must give a blank twin id so it never matches a node")
	}
	for _, padded := range [][2]string{
		{"repository:r", " svc"}, {"repository:r", "svc "}, {" repository:r", "svc"}, {"repository:r\t", "svc"},
	} {
		if got := TwinID(padded[0], padded[1]); got != "workload:repository:r:svc" {
			t.Fatalf("TwinID(%q, %q) = %q, want the trimmed twin id", padded[0], padded[1], got)
		}
	}
}

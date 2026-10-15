// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// TestInfraSearchKeyCensusFlagsSeededViolations proves the census can fail:
// planted writers that set a searched field without search.Statement are
// found, a clause pasted as text (or named only in a comment) does not
// satisfy it, and keyed, unrelated, lookup-only, REMOVE, and label-only
// statements stay clean.
func TestInfraSearchKeyCensusFlagsSeededViolations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, "internal", "storage", "seed", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package seed\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search\"\n)\n\nvar _ = fmt.Sprintf\nvar _ = search.On\n\n" + body + "\n"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	q := func(s string) string { return "`" + s + "`" }

	// Violations.
	write("set_field.go", "const setField = "+q("UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.name = row.name"))
	write("map_merge.go", "func mapMerge(label string) string {\n\treturn fmt.Sprintf("+q("UNWIND $rows AS row\nMERGE (n:%s {uid: row.entity_id})\nSET n += row.props")+", label)\n}")
	write("inline_map.go", "const inlineMap = "+q("UNWIND $rows AS row\nMERGE (k:K8sResource {uid: row.uid, name: row.name})"))
	write("pasted.go", "const pasted = "+q("UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.region = row.region\n"+search.Clause("r")))
	write("comment.go", "// "+search.Clause("r")+"\nconst commented = "+q("UNWIND $rows AS row\nMERGE (r:TerraformVariable {uid: row.uid})\nSET r.id = row.uid"))

	// Clean.
	write("keyed.go", "const keyedBase = "+q("UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.name = row.name")+
		"\n\nfunc keyed(d search.Dialect) string { return search.Statement(d, \"CloudResource\", \"r\", keyedBase) }")
	write("keyed_template.go", "const keyedTemplate = "+q("UNWIND $rows AS row\nMERGE (n:%s {uid: row.entity_id})\nSET n += row.props")+
		"\n\nfunc keyedTpl(d search.Dialect, label string) string {\n\treturn search.Statement(d, label, \"n\", fmt.Sprintf(keyedTemplate, label))\n}")
	write("unrelated.go", "const unrelated = "+q("UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.state = row.state"))
	write("code_label.go", "const codeLabel = "+q("UNWIND $rows AS row\nMERGE (f:Function {uid: row.uid})\nSET f.name = row.name"))
	write("lookup.go", "const lookup = "+q("UNWIND $rows AS row\nMATCH (c:TerraformResource {repo_id: row.repo_id, name: row.address})\nRETURN count(c) AS n"))
	write("remove.go", "const removeOnly = "+q("MATCH (r:TerraformStateResource)\nWHERE r.uid IN $uids\nREMOVE r.kind, r.data_type"))
	write("relabel.go", "const relabel = "+q("MATCH (r:TerraformResource)\nWHERE r.uid IN $uids\nSET r:TerraformStateResource\nREMOVE r:TerraformResource"))

	findings, keyed := newCensusScanner(t, root, []string{"internal/storage"}).scan()
	got := map[string]string{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.file, ":", 2)[0])] = f.problem
	}
	for _, name := range []string{"set_field.go", "map_merge.go", "inline_map.go", "pasted.go", "comment.go"} {
		if _, ok := got[name]; !ok {
			t.Errorf("seeded violation %s not found; findings = %v", name, findings)
		}
	}
	if !strings.Contains(got["pasted.go"], "pasted") {
		t.Errorf("pasted.go problem = %q, want the pasted-clause report", got["pasted.go"])
	}
	for _, name := range []string{"keyed.go", "keyed_template.go", "unrelated.go", "code_label.go", "lookup.go", "remove.go", "relabel.go"} {
		if problem, ok := got[name]; ok {
			t.Errorf("%s flagged (%s), want clean", name, problem)
		}
	}
	if keyed != 2 {
		t.Errorf("keyed writes = %d, want 2 (keyed.go, keyed_template.go)", keyed)
	}
}

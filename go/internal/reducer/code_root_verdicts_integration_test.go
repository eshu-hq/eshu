// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/ruby"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// parseRubyCorpus parses each source file with the REAL Ruby parser and returns
// the class entities and controller-action roots exactly as the collector would
// derive them from the parser payload — no fabricated entities. This is the
// #5376 anti-masking harness: the original P1 false positive was hidden by
// hand-built RubyClassEntity.Name values (e.g. "Admin::BaseController") that the
// parser's constantName can never emit. This test feeds genuine parser output.
func parseRubyCorpus(t *testing.T, files map[string]string) ([]reducer.RubyClassEntity, []reducer.CodeReachabilityRoot) {
	t.Helper()
	dir := t.TempDir()
	var classes []reducer.RubyClassEntity
	var roots []reducer.CodeReachabilityRoot
	for name, src := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		payload, err := ruby.Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		classes = append(classes, rubyClassEntitiesFromPayload(t, payload)...)
		roots = append(roots, rubyControllerRootsFromPayload(t, name, payload)...)
	}
	return classes, roots
}

func rubyClassEntitiesFromPayload(t *testing.T, payload map[string]any) []reducer.RubyClassEntity {
	t.Helper()
	items, _ := payload["classes"].([]map[string]any)
	out := make([]reducer.RubyClassEntity, 0, len(items))
	for _, item := range items {
		name, _ := item["name"].(string)
		qualifiedName, _ := item["qualified_name"].(string)
		qualifiedBases, _ := item["qualified_bases"].([]string)
		out = append(out, reducer.RubyClassEntity{
			Name:           name,
			QualifiedName:  qualifiedName,
			QualifiedBases: qualifiedBases,
		})
	}
	return out
}

func rubyControllerRootsFromPayload(t *testing.T, file string, payload map[string]any) []reducer.CodeReachabilityRoot {
	t.Helper()
	items, _ := payload["functions"].([]map[string]any)
	out := make([]reducer.CodeReachabilityRoot, 0)
	for _, item := range items {
		kinds, _ := item["dead_code_root_kinds"].([]string)
		if len(kinds) == 0 {
			continue
		}
		classContext, _ := item["class_context"].(string)
		name, _ := item["name"].(string)
		out = append(out, reducer.CodeReachabilityRoot{
			EntityID:     file + ":" + name,
			RootKinds:    kinds,
			ClassContext: classContext,
		})
	}
	return out
}

// TestBuildCodeRootVerdictsFromRealParserEmissions parses a real multi-file Ruby
// corpus and feeds the actual parser emissions into BuildCodeRootVerdicts. It
// proves the #5376 P1 fix end-to-end across the parser->reducer seam:
//
//   - WidgetsController < Admin::Base, with Admin::Base < ActionController::Base
//     in another file: the parser roots the action (F1 keep-biased on the
//     unresolved namespaced base), and the reducer CONFIRMS it repo-wide (the
//     genuine controller the old code flagged dead).
//   - OrdersController < BaseController, with BaseController < ApplicationRecord
//     in another file: the parser roots the action (unresolved *Controller
//     suffix), and the reducer DOWNGRADES it (resolves onward to the rejected
//     framework base) — the true cross-file downgrade the fix must preserve.
func TestBuildCodeRootVerdictsFromRealParserEmissions(t *testing.T) {
	t.Parallel()

	classes, roots := parseRubyCorpus(t, map[string]string{
		"app/controllers/widgets_controller.rb": `class WidgetsController < Admin::Base
  def index
    true
  end
end
`,
		"app/controllers/admin/base.rb": `module Admin
  class Base < ActionController::Base
  end
end
`,
		"app/controllers/orders_controller.rb": `class OrdersController < BaseController
  def list
    true
  end
end
`,
		"app/models/base_controller.rb": `class BaseController < ApplicationRecord
end
`,
	})

	// Sanity: the parser must actually have rooted BOTH controller actions, or
	// the test proves nothing.
	if !hasRoot(roots, "index") || !hasRoot(roots, "list") {
		t.Fatalf("parser did not root both controller actions; roots=%+v", roots)
	}

	rows, downgraded, _ := reducer.BuildCodeRootVerdicts(reducer.CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots:        roots,
		RubyClasses:  classes,
	})

	confirmed := verdictForAction(rows, "index")
	if confirmed == nil || confirmed.Verdict != reducer.CodeRootVerdictConfirmed {
		t.Fatalf("WidgetsController#index must be CONFIRMED (genuine controller via namespaced base), got %+v", confirmed)
	}
	if _, isDown := downgraded[confirmed.EntityID]; isDown {
		t.Fatalf("confirmed controller must not be in the downgraded set")
	}

	downgradedRow := verdictForAction(rows, "list")
	if downgradedRow == nil || downgradedRow.Verdict != reducer.CodeRootVerdictDowngraded {
		t.Fatalf("OrdersController#list must be DOWNGRADED (resolves onward to ApplicationRecord), got %+v", downgradedRow)
	}
	if _, isDown := downgraded[downgradedRow.EntityID]; !isDown {
		t.Fatalf("downgraded controller action must be in the downgraded set")
	}
	if downgradedRow.Basis.Reason != "rejected_framework_base" {
		t.Fatalf("downgrade basis reason = %q, want rejected_framework_base (basis=%+v)", downgradedRow.Basis.Reason, downgradedRow.Basis)
	}
}

// TestBuildCodeRootVerdictsLexicalScopeRestrictionFromRealParserEmissions is the
// #5500 end-to-end proof: it parses a REAL, module-nested Ruby corpus (no
// hand-built qualified names) where a bare, unqualified base ref "Base" is
// declared inside module Admin, and a same-last-segment but unrelated
// "Reporting::Base" class also exists elsewhere in the corpus. Pre-#5500 this
// shape was suffix_only_ambiguous (broad, unscoped candidate search) and the
// dead controller action stayed falsely kept; the lexical-scope restriction
// resolves "Base" to the true, lexically-scoped referent "Admin::Base" and lets
// the walk correctly downgrade it.
func TestBuildCodeRootVerdictsLexicalScopeRestrictionFromRealParserEmissions(t *testing.T) {
	t.Parallel()

	classes, roots := parseRubyCorpus(t, map[string]string{
		"app/controllers/admin/orders_controller.rb": `module Admin
  class OrdersController < Base
    def index
      true
    end
  end
end
`,
		"app/controllers/admin/base.rb": `module Admin
  class Base < ActiveRecord::Base
  end
end
`,
		"app/models/reporting/base.rb": `module Reporting
  class Base < ActiveRecord::Base
  end
end
`,
	})

	if !hasRoot(roots, "index") {
		t.Fatalf("parser did not root the controller action; roots=%+v", roots)
	}

	rows, downgraded, _ := reducer.BuildCodeRootVerdicts(reducer.CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots:        roots,
		RubyClasses:  classes,
	})

	row := verdictForAction(rows, "index")
	if row == nil || row.Verdict != reducer.CodeRootVerdictDowngraded {
		t.Fatalf("Admin::OrdersController#index must be DOWNGRADED (lexically resolves to Admin::Base < ActiveRecord::Base), got %+v", row)
	}
	if _, isDown := downgraded[row.EntityID]; !isDown {
		t.Fatalf("downgraded controller action must be in the downgraded set")
	}
}

// TestBuildCodeRootVerdictsLexicalScopeCompactColonFormDoesNotMaskTrueReferentFromRealParserEmissions
// is the #5500 P0 end-to-end proof: it parses a REAL Ruby corpus using Ruby's
// COMPACT COLON class-declaration form (`class Admin::OrdersController < Base`
// with NO enclosing `module Admin` block) instead of nested module blocks.
// Real Ruby Module.nesting for this form does NOT include "Admin" when
// resolving the bare "Base" reference, so the true referent is the TOP-LEVEL
// "Base" class — but the parser's qualifiedClassName (nodes.go) produces the
// identical "Admin::OrdersController" qualified name it would for a genuinely
// nested declaration, so the reducer's registry cannot tell the two forms
// apart. A coincidentally-named, unrelated "Admin::Base" class exists
// elsewhere in the corpus (module-nested, so it is genuinely namespaced) and
// must NOT mask the true top-level "Base" referent — the controller action
// must stay CONFIRMED, not be falsely DOWNGRADED.
func TestBuildCodeRootVerdictsLexicalScopeCompactColonFormDoesNotMaskTrueReferentFromRealParserEmissions(t *testing.T) {
	t.Parallel()

	classes, roots := parseRubyCorpus(t, map[string]string{
		"app/controllers/admin/orders_controller.rb": `class Admin::OrdersController < Base
  def index
    true
  end
end
`,
		"app/controllers/admin/base.rb": `module Admin
  class Base < ActiveRecord::Base
  end
end
`,
		"app/controllers/base.rb": `class Base < ApplicationController
end
`,
	})

	if !hasRoot(roots, "index") {
		t.Fatalf("parser did not root the controller action; roots=%+v", roots)
	}

	rows, downgraded, _ := reducer.BuildCodeRootVerdicts(reducer.CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots:        roots,
		RubyClasses:  classes,
	})

	row := verdictForAction(rows, "index")
	if row == nil || row.Verdict != reducer.CodeRootVerdictConfirmed {
		t.Fatalf("Admin::OrdersController#index (compact-colon form) must be CONFIRMED (the true top-level Base < ApplicationController referent must stay in the candidate set, not be masked by the unrelated Admin::Base), got %+v", row)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("genuine controller action must not be in the downgraded set")
	}
}

// TestBuildCodeRootVerdictsAbsoluteReferenceFromRealParserEmissions is the
// #5733 P1 end-to-end proof (codex review of #5500): it parses a REAL Ruby
// corpus where the base is declared with an explicit ABSOLUTE constant path
// (`class OrdersController < ::Base`, inside `module Admin`) through the
// actual parser, feeding its actual `qualified_bases` emission into the real
// reducer registry. Real Ruby resolves `::Base` starting at Object with NO
// enclosing-namespace search, so it can never mean the unrelated, in-corpus
// `Admin::Base` — only the (here, absent-from-corpus) top-level `Base`. Before
// the fix, the parser stripped the leading "::" when emitting qualified_bases
// and rubycontroller's normalizeBases stripped any surviving "::"
// unconditionally, making the absolute reference indistinguishable from a
// relative one; the #5500 lexical-scope restriction then wrongly resolved it
// onto Admin::Base < ActiveRecord::Base and downgraded a genuinely live
// controller action.
func TestBuildCodeRootVerdictsAbsoluteReferenceFromRealParserEmissions(t *testing.T) {
	t.Parallel()

	classes, roots := parseRubyCorpus(t, map[string]string{
		"app/controllers/admin/orders_controller.rb": `module Admin
  class OrdersController < ::Base
    def index
      true
    end
  end
end
`,
		"app/controllers/admin/base.rb": `module Admin
  class Base < ActiveRecord::Base
  end
end
`,
	})

	if !hasRoot(roots, "index") {
		t.Fatalf("parser did not root the controller action; roots=%+v", roots)
	}

	rows, downgraded, _ := reducer.BuildCodeRootVerdicts(reducer.CodeReachabilityProjectionInput{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepositoryID: "repo-1",
		Roots:        roots,
		RubyClasses:  classes,
	})

	row := verdictForAction(rows, "index")
	if row == nil || row.Verdict != reducer.CodeRootVerdictConfirmed {
		t.Fatalf("Admin::OrdersController#index (absolute `::Base` reference) must be CONFIRMED (the real top-level Base is external to the corpus; it must not resolve onto the unrelated in-corpus Admin::Base), got %+v", row)
	}
	if _, isDown := downgraded[row.EntityID]; isDown {
		t.Fatalf("genuine controller action must not be in the downgraded set")
	}
}

func hasRoot(roots []reducer.CodeReachabilityRoot, action string) bool {
	for _, r := range roots {
		if endsWith(r.EntityID, ":"+action) {
			return true
		}
	}
	return false
}

func verdictForAction(rows []reducer.CodeRootVerdictRow, action string) *reducer.CodeRootVerdictRow {
	for i := range rows {
		if endsWith(rows[i].EntityID, ":"+action) {
			return &rows[i]
		}
	}
	return nil
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

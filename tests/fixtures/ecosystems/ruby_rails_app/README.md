# ruby_rails_app (golden-corpus fixture)

A small multi-file Rails-shaped Ruby app that exercises the #5376 repo-wide
controller dead-code verdict end-to-end across FILE boundaries — something
`ruby.fixture.json` (single-file by construction) cannot regress.

- **Cross-file CONFIRM**: `WidgetsController < Admin::BaseController`
  (`app/controllers/widgets_controller.rb`), whose base is defined in
  `app/controllers/admin/base_controller.rb` and resolves onward to
  `ApplicationController < ActionController::Base`. The same-file parser walk
  keeps it (F1, unresolved qualified base); the reducer CONFIRMS it repo-wide.
- **Cross-file DOWNGRADE**: `LegacyReportsController < LegacyBaseController`
  (`app/controllers/legacy_reports_controller.rb`), whose Controller-suffixed
  base is really a model base (`app/models/legacy_base_controller.rb`,
  `LegacyBaseController < ApplicationRecord < ActiveRecord::Base`). The same-file
  walk keeps it (unresolved `*Controller` suffix); the reducer DOWNGRADES it
  (`rejected_framework_base`).

`WidgetService` (`app/services/widget_service.rb`) is a plain PORO with
intra-repo calls so the corpus also emits CALLS edges alongside the INHERITS
chain. This fixture is code-only (statically parsed); it has no cassette.

## #5726 namespaced controller ancestry (CONFIRM-only regression guards)

Two more cross-file controllers extend the #5376/#5500 discrimination to
namespaced ancestry shapes that the #5500 P0 fix (see
`go/internal/reducer/evidence-5500-lexical-scope-restriction.md`) specifically
targets. Both must stay **CONFIRMED** (suppressed as modeled roots, never
`cleanup_ready`); a regression here would silently re-mask a genuine
controller as dead code.

- **Compact-colon P0 masking guard**: `Admin::ReportsController`
  (`app/controllers/admin/reports_controller.rb`) is declared with Ruby's
  COMPACT COLON form — `class Admin::ReportsController < Base` — with NO
  enclosing `module Admin` block. Its true base is a genuine TOP-LEVEL `class
  Base < ApplicationController` (`app/controllers/base.rb`). A
  coincidentally-named, unrelated, genuinely module-nested `module Admin;
  class Base < ActiveRecord::Base; end; end` also exists
  (`app/models/admin/base.rb`). The parser's `qualifiedClassName` produces the
  identical qualified name `Admin::ReportsController` for the compact-colon
  form as it would for a genuinely nested declaration, so
  `classNamespaceOf` cannot tell them apart — before the #5500 P0 fix, the
  first-hit `lexicalExactMatch` would resolve only the coincidental
  `Admin::Base` and never try the true top-level `Base`, silently downgrading
  `summary` (the exact masking class #5500 fixed). After the fix (union
  across every scope level plus the bare ref), both candidates stay in play
  and any-path-keeps rescues it through `Base -> ApplicationController`, so
  `summary` stays CONFIRMED.
- **Outer-scope preservation guard**: `Api::V1::UsersController`
  (`app/controllers/api/v1/users_controller.rb`) is declared with genuine
  nested `module Api; module V1; ... end; end` blocks, whose bare `Base`
  reference resolves through the OUTER enclosing `Api` scope
  (`app/controllers/api/base.rb`, `class Base < ApplicationController`), not
  the immediate `Api::V1` scope. This proves the lexical-scope walk correctly
  mirrors Ruby's `Module.nesting` innermost-to-outermost search order —
  `profile` stays CONFIRMED.

Both actions are same-file rooted via the `conventionalAmbiguousBases`
("Base"/"API" with zero same-file candidates keep) floor, exactly like the
pre-existing cross-file CONFIRM case above; the reducer's repo-wide walk is
what proves CONFIRMED rather than DOWNGRADED here. See the B-12 snapshot's
`POST /api/v0/code/dead-code/investigate?golden_scope=ruby_rails_app` query
shape, whose `suppressed[]` object-match list now also requires `summary` and
`profile` with classification `excluded`.

## Dead-code root golden gate coverage & Ifá determination

Beyond the INHERITS/CALLS edges above, the B-12 snapshot
(`testdata/golden/e2e-20repo-snapshot.json`, HTTP query shape
`POST /api/v0/code/dead-code/investigate?golden_scope=ruby_rails_app`) pins the
Rails route-handler dead-code discrimination live: `generate`
(`LegacyReportsController#generate`, the downgraded foil) must appear in the
`cleanup_ready` bucket with classification `unused`, and `index`
(`WidgetsController#index`, the rooted handler) must appear in `suppressed` with
classification `excluded`. Both object-matches are closed on
`(name, classification)`, so over-rooting the foil or under-rooting the real
action fails the gate.

Ifá materialized-edge coverage for this **dead-code-root** discrimination is
**N/A**: the Rails route-handler root is a content-store dead-code verdict
governing query-time suppression; it writes no reducer/graph edge and adds no
`reducer.MaterializedEdgeFamilies()` domain. (The INHERITS/CALLS edges are a
separate, pre-existing concern.)

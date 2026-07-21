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

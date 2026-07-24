# ROUTE-LIVENESS GOLDEN COVERAGE (#5723, for the #5494 route_unreachable
# downgrade). This routes.rb is deliberately EXACT-ONLY: every registration is a
# literal, fully-resolved `to: "controller#action"` handler with NO ambiguous
# construct anywhere (no resources/resource macro, no root, no match, no
# controller:/action: pair, no bare/interpolated path, no non-string to:
# target). Per internal/parser/ruby/framework_routes_ambiguity.go's fail-safe
# scan, that keeps framework_semantics.rails.has_unmodeled_routes UNSET, so the
# repo route surface is provably complete and the #5494 reducer join
# (evaluateRouteLiveness in internal/reducer/code_root_verdicts_routes.go) is
# allowed to DOWNGRADE an ancestry-confirmed controller action that no route
# reaches.
#
# The handler keys join on the SIMPLE (last-segment) controller class name the
# Ruby parser stamps as class_context (metadata->>'class_context' -- see
# internal/reducer/code_reachability_projection.go), so `to: "reports#summary"`
# joins Admin::ReportsController#summary (class_context "ReportsController") even
# though that controller is declared with the compact-colon namespace form.
#
# Two golden outcomes ride on this exact-only surface:
#   * KEPT-because-routed: WidgetsController#index/#show, Admin::ReportsController
#     #summary, and Api::V1::UsersController#profile each get an exact route, so
#     evaluateRouteLiveness returns RouteEvidenceRouted and they stay
#     suppressed/excluded (they carry positive route evidence now, not merely
#     the ancestry keep they had when no routes.rb existed).
#   * UNROUTED-downgrade: OrphanedController#dangling (app/controllers/
#     orphaned_controller.rb) is an ancestry-confirmed controller action -- it
#     would be kept by #5376 ancestry alone -- but NO route below reaches it, so
#     on this exact-only surface #5494 downgrades it to route_unreachable and it
#     surfaces as a cleanup_ready/unused dead-code candidate. That downgrade,
#     of a genuinely ancestry-valid controller action, is the signal this
#     fixture exists to regression-guard end to end.
Rails.application.routes.draw do
  get "/widgets", to: "widgets#index"
  get "/widgets/:id", to: "widgets#show"
  # NOTE (simple-name join, intentional): the `to:` handler is deliberately the
  # SIMPLE controller name Eshu keys on, not the real Rails namespaced path. The
  # #5494 route-liveness join matches an ancestry-confirmed action against
  # RoutedHandlers by the parser-stamped SIMPLE class_context ("ReportsController"
  # for the compact-colon `Admin::ReportsController`, "UsersController" for
  # `Api::V1::UsersController` -- see internal/reducer/code_root_verdicts_routes.go
  # and the class_context doc in internal/reducer/code_reachability_projection.go).
  # Real Rails would dispatch these as `admin/reports#summary` / `api/v1/users#profile`,
  # but a namespaced (slash-bearing) `to:` string is deliberately treated as
  # UNMODELED by the fail-safe scan (framework_routes.go rejects "/"), which would
  # flip has_unmodeled_routes and disable the downgrade repo-wide. So an exact-only
  # surface MUST route these namespaced controllers by their simple class_context
  # name -- which is exactly the simple-name join this fixture exercises. This
  # does NOT weaken the OrphanedController#dangling downgrade guard: `dangling`'s
  # simple class_context ("OrphanedController") is unrouted here regardless.
  get "/reports/summary", to: "reports#summary"
  get "/users/profile", to: "users#profile"
  # OrphanedController#dangling is intentionally NOT routed -> route_unreachable.
end

# UNROUTED-DOWNGRADE GOLDEN FOIL (#5723, for the #5494 route_unreachable
# downgrade). OrphanedController inherits straight from ApplicationController
# (-> ActionController::Base), so the #5376 ancestry walk CONFIRMS it as a
# genuine Rails controller and `dangling` as a real controller action: with no
# routes.rb in the repo it would be kept (RouteEvidenceNoData). But config/
# routes.rb registers an EXACT-ONLY surface that never routes it, so the #5494
# reducer join proves the exact route set complete and finds no handler for
# OrphanedController#dangling -- downgrading it to route_unreachable. `dangling`
# has no caller either, so once its controller-action root status is stripped it
# is genuinely unreachable and must surface as a cleanup_ready/unused dead-code
# candidate. This is the positive dead-route signal #5494 exists to produce, and
# the case #5723 adds end-to-end golden coverage for.
class OrphanedController < ApplicationController
  def dangling
    render plain: "unreachable"
  end
end

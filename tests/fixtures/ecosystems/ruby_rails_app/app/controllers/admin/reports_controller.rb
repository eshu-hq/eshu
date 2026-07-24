# COMPACT-COLON P0 REGRESSION GUARD (#5726): Admin::ReportsController uses
# Ruby's COMPACT COLON class-declaration form with NO enclosing `module Admin`
# block. Real Ruby Module.nesting for the bare `Base` reference here does NOT
# include "Admin" -- the true referent is the TOP-LEVEL `Base` class
# (../base.rb), which resolves onward to ApplicationController ->
# ActionController::Base. A coincidentally-named, unrelated `Admin::Base`
# class also exists (../../models/admin/base.rb, genuinely module-nested) and
# must NOT mask that true top-level referent.
#
# This is the exact #5500 P0 false-downgrade class: the parser's
# qualifiedClassName (go/internal/parser/ruby/nodes.go) cannot distinguish
# this compact-colon form from a genuinely nested-module-block declaration, so
# classNamespaceOf derives namespace "Admin" for both. Before the P0 fix,
# lexicalExactMatch returned on the FIRST non-empty ExactMatches hit
# (Admin::Base) and never tried the bare top-level "Base" -- summary would
# have been wrongly DOWNGRADED. After the fix (union across every scope level
# plus the bare ref), both candidates stay in play and any-path-keeps rescues
# it through the genuine Base -> ApplicationController chain. summary must
# stay CONFIRMED.
class Admin::ReportsController < Base
  def summary
    build_summary
  end

  def build_summary
    "summary"
  end
end

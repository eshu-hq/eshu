module Admin
  # #5726: a coincidentally-named, UNRELATED model base. Despite sharing the
  # qualified name "Admin::Base" that a genuinely nested `module Admin; class
  # Base < Bar; end; end` declaration would also produce, this class has
  # nothing to do with Admin::ReportsController's ancestry
  # (../../controllers/admin/reports_controller.rb): that controller uses the
  # COMPACT COLON form (`class Admin::ReportsController < Base` with NO
  # enclosing `module Admin` block), so its `Base` reference resolves to the
  # TOP-LEVEL `Base` (../../controllers/base.rb), not this one. Before the
  # #5500 P0 fix, this coincidental class masked the true top-level referent
  # and caused a false downgrade.
  class Base < ActiveRecord::Base
  end
end

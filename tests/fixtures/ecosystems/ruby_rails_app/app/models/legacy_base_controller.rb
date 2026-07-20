# A mis-named model base: despite the "Controller" suffix it extends
# ApplicationRecord (an ActiveRecord model base), NOT a Rails controller base.
# This is what makes LegacyReportsController a cross-file DOWNGRADE.
class LegacyBaseController < ApplicationRecord
  def audit_columns
    %i[created_at updated_at]
  end
end

# CROSS-FILE DOWNGRADE: LegacyReportsController's base LegacyBaseController is
# unresolved in this file (Controller-suffixed, so the same-file walk KEEPS and
# roots `generate`), but repo-wide it resolves onward to ApplicationRecord ->
# ActiveRecord::Base — a mis-based "controller" that is really a model. The
# #5376 reducer verdict DOWNGRADES `generate` (rejected_framework_base).
class LegacyReportsController < LegacyBaseController
  def generate
    build_report
  end

  def build_report
    "report"
  end
end

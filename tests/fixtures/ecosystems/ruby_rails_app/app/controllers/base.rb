# #5726: the TRUE top-level base for the compact-colon
# Admin::ReportsController (app/controllers/admin/reports_controller.rb). A
# coincidentally-named `Admin::Base` also exists
# (app/models/admin/base.rb) but must never mask this genuine referent — see
# that file's comment and the #5500 P0 fix
# (go/internal/reducer/evidence-5500-lexical-scope-restriction.md).
class Base < ApplicationController
end

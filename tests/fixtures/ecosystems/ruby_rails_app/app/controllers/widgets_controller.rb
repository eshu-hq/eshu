# CROSS-FILE CONFIRM: WidgetsController's real base Admin::BaseController lives
# in another file and resolves onward to ActionController::Base. The same-file
# walk keeps it (F1, unresolved qualified base); the repo-wide #5376 verdict
# CONFIRMS it. index/show are genuine controller actions.
class WidgetsController < Admin::BaseController
  def index
    @widgets = WidgetService.new.all
  end

  def show
    @widget = WidgetService.new.find(params[:id])
  end
end

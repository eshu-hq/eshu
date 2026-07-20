module Admin
  # A namespaced controller base defined in a DIFFERENT file from the
  # controllers that extend it. The same-file parser walk cannot resolve
  # `Admin::BaseController` here; the reducer resolves it repo-wide through
  # qualified_name to ApplicationController -> ActionController::Base.
  class BaseController < ApplicationController
    before_action :require_admin

    def require_admin
      head :forbidden unless current_account&.admin?
    end
  end
end

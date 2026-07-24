module Api
  # OUTER-SCOPE PRESERVATION (#5726): declared in the "Api" module, one
  # lexical level OUT from Api::V1::UsersController's own namespace
  # "Api::V1" (v1/users_controller.rb). Real Ruby Module.nesting search walks
  # enclosing scopes innermost-to-outermost, so a bare `Base` reference inside
  # Api::V1 must still resolve here, not just in the immediate Api::V1 scope.
  # Resolves onward to ApplicationController -> ActionController::Base, a
  # genuine controller base.
  class Base < ApplicationController
  end
end

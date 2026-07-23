module Api
  module V1
    # OUTER-SCOPE PRESERVATION (#5726): the bare `Base` reference is declared
    # ONE lexical level OUT (module Api, not module Api::V1 -- see
    # ../base.rb). The #5500 lexical-scope restriction walks
    # Module.nesting-style candidates from the innermost enclosing scope
    # ("Api::V1::Base", no match) to each outer prefix ("Api::Base", the true
    # referent) before falling back to a bare top-level probe, so profile must
    # still resolve through the outer Api scope and stay CONFIRMED.
    class UsersController < Base
      def profile
        current_profile
      end

      def current_profile
        { id: 1 }
      end
    end
  end
end

class PublicRubyController
  before_action :authenticate_ruby_user!

  def index
    direct_ruby_helper
  end

  private

  def authenticate_ruby_user!
    true
  end

  def controller_private_helper
    'direct'
  end
end

def main
  direct_ruby_helper
  selected_ruby_handler
  PublicRubyController.new.index
end

def unused_ruby_helper
  'unused'
end

def direct_ruby_helper
  'direct'
end

def selected_ruby_handler
  method(:direct_ruby_helper).call
end

class DynamicRubyEndpoint
  def method_missing(name, *args)
    public_send(name, *args)
  end

  def respond_to_missing?(name, include_private = false)
    true
  end
end

def generated_ruby_stub
  'generated'
end

def dynamic_ruby_dispatch(name)
  send(name)
end

if __FILE__ == $PROGRAM_NAME
  main
end

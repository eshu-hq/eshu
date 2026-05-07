class PublicRubyController
  def index
    direct_ruby_helper
  end

  def direct_ruby_helper
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

def generated_ruby_stub
  'generated'
end

def dynamic_ruby_dispatch(name)
  send(name)
end

main

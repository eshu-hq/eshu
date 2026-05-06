# Module and mixin patterns for parser testing.

require_relative 'basic'
require_relative 'inheritance'

module Comprehensive
  module Printable
    def print_details
      puts to_s
    end
  end

  module Cacheable
    def self.included(base)
      base.extend(ClassMethods)
    end

    module ClassMethods
      def cache_method(method_name)
        original = instance_method(method_name)

        define_method(method_name) do |*args|
          @cache ||= {}
          key = [method_name, args]
          @cache[key] ||= original.bind(self).call(*args)
        end
      end
    end
  end

  class Service
    include Printable
    include Cacheable

    attr_reader :name

    def initialize(name)
      @name = name
    end

    def expensive_operation(input)
      sleep(0.1) # simulate work
      input.upcase
    end

    cache_method :expensive_operation

    def to_s
      "Service(#{@name})"
    end
  end
end

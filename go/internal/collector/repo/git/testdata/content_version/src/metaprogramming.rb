# Metaprogramming patterns for parser testing.

module Comprehensive
  module ClassMethods
    def define_attribute(name)
      attr_accessor name

      define_method("#{name}?") do
        !instance_variable_get("@#{name}").nil?
      end
    end
  end

  class DynamicModel
    extend ClassMethods

    define_attribute :title
    define_attribute :description
    define_attribute :status

    def initialize(attrs = {})
      attrs.each do |key, value|
        send("#{key}=", value) if respond_to?("#{key}=")
      end
    end
  end

  class DSLBuilder
    def initialize(&block)
      @config = {}
      instance_eval(&block) if block_given?
    end

    def method_missing(name, *args)
      if name.to_s.end_with?("=")
        @config[name.to_s.chomp("=")] = args.first
      else
        @config[name.to_s]
      end
    end

    def respond_to_missing?(name, include_private = false)
      true
    end

    def to_h
      @config
    end
  end
end

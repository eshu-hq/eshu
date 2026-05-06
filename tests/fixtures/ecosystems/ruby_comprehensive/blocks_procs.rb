# Block, Proc, and Lambda patterns for parser testing.

module Comprehensive
  class Collection
    include Enumerable

    def initialize(*items)
      @items = items.flatten
    end

    def each(&block)
      @items.each(&block)
    end

    def transform(&block)
      @items.map(&block)
    end

    def select_with(&block)
      @items.select(&block)
    end
  end

  # Lambda examples
  class Processor
    def initialize
      @transforms = []
    end

    def add_transform(transform = nil, &block)
      @transforms << (transform || block)
    end

    def process(value)
      @transforms.reduce(value) { |v, t| t.call(v) }
    end
  end

  # Proc vs Lambda
  double = Proc.new { |x| x * 2 }
  safe_divide = lambda { |a, b| b.zero? ? nil : a.to_f / b }

  # Method objects
  class Calculator
    def add(a, b)
      a + b
    end

    def subtract(a, b)
      a - b
    end

    def get_method(name)
      method(name)
    end
  end
end

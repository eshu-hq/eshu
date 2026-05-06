# Inheritance patterns for parser testing.

module Comprehensive
  class Animal
    attr_reader :name

    def initialize(name)
      @name = name
    end

    def speak
      raise NotImplementedError, "Subclass must implement speak"
    end

    def describe
      "#{@name} says #{speak}"
    end
  end

  class Dog < Animal
    def speak
      "Woof!"
    end

    def fetch(item)
      "#{@name} fetched #{item}"
    end
  end

  class Cat < Animal
    def speak
      "Meow!"
    end
  end

  module Loggable
    def log(message)
      puts "[#{self.class.name}] #{message}"
    end
  end

  module Serializable
    def to_hash
      instance_variables.each_with_object({}) do |var, hash|
        hash[var.to_s.delete("@")] = instance_variable_get(var)
      end
    end
  end

  class ServiceDog < Dog
    include Loggable
    include Serializable

    attr_reader :job

    def initialize(name, job)
      super(name)
      @job = job
    end

    def work
      log("Working as #{@job}")
      "#{@name} is working"
    end
  end
end

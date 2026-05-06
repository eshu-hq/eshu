# Basic Ruby constructs for parser testing.

module Comprehensive
  VERSION = "1.0.0"

  def self.greet(name)
    "Hello, #{name}!"
  end

  class Config
    attr_accessor :env, :debug
    attr_reader :version

    def initialize(env: "development")
      @env = env
      @debug = env != "production"
      @version = VERSION
    end

    def production?
      @env == "production"
    end
  end

  class Application
    def initialize(config)
      @config = config
      @running = false
    end

    def start
      @running = true
      puts Comprehensive.greet("World")
    end

    def stop
      @running = false
    end

    def running?
      @running
    end
  end
end

defmodule Comprehensive.App do
  use GenServer
  alias Comprehensive.{Basic, Worker, User}
  import Comprehensive.Patterns, only: [classify: 1]
  require Logger

  def start do
    Logger.info("Starting application")
    greeting = Basic.greet("World")
    Logger.info(greeting)

    user = %User{name: "Alice", email: "alice@example.com", age: 30}
    result = classify(user)
    Logger.info("Classification: #{inspect(result)}")
  end
end

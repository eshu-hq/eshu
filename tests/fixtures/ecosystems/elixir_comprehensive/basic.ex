defmodule Comprehensive.Basic do
  @moduledoc "Basic Elixir constructs."

  @version "1.0.0"

  def greet(name) do
    "Hello, #{name}!"
  end

  def add(a, b), do: a + b

  def divide(_a, 0), do: {:error, :division_by_zero}
  def divide(a, b), do: {:ok, a / b}

  defp validate(input) when is_binary(input), do: :ok
  defp validate(_), do: :error
end

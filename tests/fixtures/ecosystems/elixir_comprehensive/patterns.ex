defmodule Comprehensive.Patterns do
  @moduledoc "Pattern matching and functional patterns."

  alias Comprehensive.User

  def classify(value) do
    case value do
      %User{age: age} when age >= 18 -> :adult
      %User{} -> :minor
      {:ok, result} -> {:success, result}
      {:error, reason} -> {:failure, reason}
      [head | _tail] -> {:list, head}
      _ -> :unknown
    end
  end

  def transform(items) when is_list(items) do
    items
    |> Enum.filter(&(&1 > 0))
    |> Enum.map(&(&1 * 2))
    |> Enum.sort()
  end

  def fibonacci(n) when n <= 1, do: n
  def fibonacci(n), do: fibonacci(n - 1) + fibonacci(n - 2)

  def comprehension_examples do
    for x <- 1..10, y <- 1..10, x + y > 15, do: {x, y}
  end
end

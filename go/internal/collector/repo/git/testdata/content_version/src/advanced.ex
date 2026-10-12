defmodule Comprehensive.Advanced do
  defmacro expose(expr) do
    expr
  end

  defguard is_even(value) when rem(value, 2) == 0

  defdelegate size(values), to: Enum
end

defprotocol Comprehensive.Describable do
  @doc "Returns a description of the data."
  def describe(data)
end

defmodule Comprehensive.User do
  defstruct [:name, :email, :age]
end

defimpl Comprehensive.Describable, for: Comprehensive.User do
  def describe(user) do
    "User: #{user.name} <#{user.email}>"
  end
end

defimpl Comprehensive.Describable, for: Map do
  def describe(map) do
    "Map with #{map_size(map)} keys"
  end
end

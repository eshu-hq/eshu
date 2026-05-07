defmodule DeadCodeFixture.DynamicElixir do
  @moduledoc false

  use GenServer

  def start(_type, _args) do
    direct_elixir_helper()
    selected_elixir_handler()
    GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  def public_elixir_api, do: :public

  def unused_elixir_helper, do: :unused

  def direct_elixir_helper, do: :direct

  def selected_elixir_handler, do: direct_elixir_helper()

  def generated_elixir_stub, do: :generated

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:ping, _from, state), do: {:reply, :pong, state}

  def dynamic_elixir_dispatch(name) do
    apply(__MODULE__, name, [])
  end
end

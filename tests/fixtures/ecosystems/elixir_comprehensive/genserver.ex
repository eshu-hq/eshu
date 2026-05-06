defmodule Comprehensive.Worker do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def get_state do
    GenServer.call(__MODULE__, :get_state)
  end

  def process(item) do
    GenServer.cast(__MODULE__, {:process, item})
  end

  # Server callbacks
  @impl true
  def init(opts) do
    {:ok, %{items: [], config: opts}}
  end

  @impl true
  def handle_call(:get_state, _from, state) do
    {:reply, state, state}
  end

  @impl true
  def handle_cast({:process, item}, state) do
    new_state = %{state | items: [item | state.items]}
    {:noreply, new_state}
  end
end

defmodule Example.OrderService do
  @moduledoc "Coordinates order placement and lookup."

  alias Example.OrderRepository

  def place(customer_id, skus) do
    order = OrderRepository.create(customer_id)

    order =
      Enum.reduce(skus, order, fn sku, acc ->
        add_line(acc, sku)
      end)

    OrderRepository.save(order)
  end

  def find(order_id) do
    OrderRepository.by_id(order_id)
  end

  defp add_line(order, sku) do
    Map.update(order, :lines, [sku], &[sku | &1])
  end
end

package com.example.orders

class OrderService(repository: OrderRepository) {

  def place(customerId: String, skus: List[String]): Order = {
    val order = repository.create(customerId)
    skus.foreach(sku => order.addLine(sku))
    repository.save(order)
  }

  def find(orderId: String): Option[Order] =
    repository.byId(orderId)
}

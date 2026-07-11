package com.example.orders

class OrderService(private val repository: OrderRepository) {

    fun place(customerId: String, skus: List<String>): Order {
        val order = repository.create(customerId)
        for (sku in skus) {
            order.addLine(sku)
        }
        return repository.save(order)
    }

    fun find(orderId: String): Order = repository.byId(orderId)
}

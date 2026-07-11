package com.example.orders;

import java.util.List;

/** OrderService coordinates order placement and lookup. */
public class OrderService {
    private final OrderRepository repository;

    public OrderService(OrderRepository repository) {
        this.repository = repository;
    }

    public Order place(String customerId, List<String> skus) {
        Order order = repository.create(customerId);
        for (String sku : skus) {
            order.addLine(sku);
        }
        return repository.save(order);
    }

    public Order find(String orderId) {
        return repository.byId(orderId);
    }
}

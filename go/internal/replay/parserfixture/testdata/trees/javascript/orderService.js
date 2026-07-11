const { OrderRepository } = require('./orderRepository');

class OrderService {
  constructor(repository) {
    this.repository = repository;
  }

  place(customerId, skus) {
    const order = this.repository.create(customerId);
    skus.forEach((sku) => order.addLine(sku));
    return this.repository.save(order);
  }

  find(orderId) {
    return this.repository.byId(orderId);
  }
}

module.exports = { OrderService };

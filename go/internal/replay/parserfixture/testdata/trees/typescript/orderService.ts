import { Order, OrderRepository } from './orderRepository';

export class OrderService {
  constructor(private readonly repository: OrderRepository) {}

  place(customerId: string, skus: string[]): Order {
    const order = this.repository.create(customerId);
    for (const sku of skus) {
      order.addLine(sku);
    }
    return this.repository.save(order);
  }

  find(orderId: string): Order | undefined {
    return this.repository.byId(orderId);
  }
}

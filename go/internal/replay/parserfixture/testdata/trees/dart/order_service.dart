import 'order_repository.dart';

class OrderService {
  final OrderRepository repository;

  OrderService(this.repository);

  Order place(String customerId, List<String> skus) {
    final order = repository.create(customerId);
    for (final sku in skus) {
      order.addLine(sku);
    }
    return repository.save(order);
  }

  Order find(String orderId) => repository.byId(orderId);
}

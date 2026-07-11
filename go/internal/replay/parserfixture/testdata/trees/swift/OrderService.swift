import Foundation

final class OrderService {
    private let repository: OrderRepository

    init(repository: OrderRepository) {
        self.repository = repository
    }

    func place(customerId: String, skus: [String]) -> Order {
        var order = repository.create(customerId: customerId)
        for sku in skus {
            order.addLine(sku)
        }
        return repository.save(order)
    }

    func find(orderId: String) -> Order? {
        return repository.byId(orderId)
    }
}

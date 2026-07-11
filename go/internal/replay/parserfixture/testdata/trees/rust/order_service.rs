use crate::repository::{Order, OrderRepository};

pub struct OrderService {
    repository: OrderRepository,
}

impl OrderService {
    pub fn new(repository: OrderRepository) -> Self {
        OrderService { repository }
    }

    pub fn place(&self, customer_id: &str, skus: &[String]) -> Order {
        let mut order = self.repository.create(customer_id);
        for sku in skus {
            order.add_line(sku);
        }
        self.repository.save(order)
    }

    pub fn find(&self, order_id: &str) -> Option<Order> {
        self.repository.by_id(order_id)
    }
}

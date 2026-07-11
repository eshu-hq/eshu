using System.Collections.Generic;

namespace Example.Orders
{
    public class OrderService
    {
        private readonly IOrderRepository _repository;

        public OrderService(IOrderRepository repository)
        {
            _repository = repository;
        }

        public Order Place(string customerId, IEnumerable<string> skus)
        {
            var order = _repository.Create(customerId);
            foreach (var sku in skus)
            {
                order.AddLine(sku);
            }
            return _repository.Save(order);
        }
    }
}

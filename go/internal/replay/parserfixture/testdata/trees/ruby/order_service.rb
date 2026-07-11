module Example
  class OrderService
    def initialize(repository)
      @repository = repository
    end

    def place(customer_id, skus)
      order = @repository.create(customer_id)
      skus.each { |sku| order.add_line(sku) }
      @repository.save(order)
    end

    def find(order_id)
      @repository.by_id(order_id)
    end
  end
end

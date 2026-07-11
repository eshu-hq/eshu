<?php

namespace Example\Orders;

class OrderService
{
    private OrderRepository $repository;

    public function __construct(OrderRepository $repository)
    {
        $this->repository = $repository;
    }

    public function place(string $customerId, array $skus): Order
    {
        $order = $this->repository->create($customerId);
        foreach ($skus as $sku) {
            $order->addLine($sku);
        }
        return $this->repository->save($order);
    }

    public function find(string $orderId): Order
    {
        return $this->repository->byId($orderId);
    }
}

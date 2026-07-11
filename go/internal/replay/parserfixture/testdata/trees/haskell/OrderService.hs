module Example.OrderService
  ( placeOrder
  , findOrder
  ) where

import Example.OrderRepository (Order, byId, createOrder, saveOrder)

placeOrder :: String -> [String] -> Order
placeOrder customerId skus =
  saveOrder (foldr addLine (createOrder customerId) skus)

addLine :: String -> Order -> Order
addLine sku order = order

findOrder :: String -> Maybe Order
findOrder = byId

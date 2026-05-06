module Functional where

import Data.List (foldl')

-- Higher-order functions
applyTwice :: (a -> a) -> a -> a
applyTwice f x = f (f x)

-- Map and filter
transform :: (a -> b) -> (b -> Bool) -> [a] -> [b]
transform f p = filter p . map f

-- Fold
sumAll :: [Int] -> Int
sumAll = foldl' (+) 0

productAll :: [Int] -> Int
productAll = foldl' (*) 1

-- Function composition
pipeline :: [Int] -> [Int]
pipeline = filter even . map (*2) . filter (>0)

-- Lambda
incrementAll :: [Int] -> [Int]
incrementAll = map (\x -> x + 1)

-- Currying
multiply :: Int -> Int -> Int
multiply a b = a * b

double :: Int -> Int
double = multiply 2

triple :: Int -> Int
triple = multiply 3

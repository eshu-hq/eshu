module Monads where

import Control.Monad (when, unless, forM_)

-- Maybe monad
safeDivide :: Double -> Double -> Maybe Double
safeDivide _ 0 = Nothing
safeDivide a b = Just (a / b)

safeHead :: [a] -> Maybe a
safeHead []    = Nothing
safeHead (x:_) = Just x

-- Do notation
processData :: Maybe Int
processData = do
  x <- Just 10
  y <- Just 20
  let sum = x + y
  return sum

-- Either monad
data AppError = NotFound String | InvalidInput String
  deriving (Show)

validate :: String -> Either AppError String
validate "" = Left (InvalidInput "empty string")
validate s  = Right s

-- IO monad
greetIO :: String -> IO ()
greetIO name = do
  putStrLn $ "Hello, " ++ name ++ "!"
  when (length name > 10) $
    putStrLn "That's a long name!"

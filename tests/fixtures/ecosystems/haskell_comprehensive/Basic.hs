module Basic where

import Data.List (sort, nub, intercalate)
import Data.Char (toUpper, toLower)

-- Simple function
greet :: String -> String
greet name = "Hello, " ++ name ++ "!"

-- Multiple parameters
add :: Int -> Int -> Int
add a b = a + b

-- Guards
classify :: Int -> String
classify n
  | n < 0     = "negative"
  | n == 0    = "zero"
  | n < 10    = "small"
  | otherwise = "large"

-- Where clause
bmi :: Double -> Double -> String
bmi weight height = category
  where
    index = weight / height ^ 2
    category
      | index < 18.5 = "underweight"
      | index < 25.0 = "normal"
      | otherwise     = "overweight"

-- Pattern matching
head' :: [a] -> Maybe a
head' []    = Nothing
head' (x:_) = Just x

-- List operations
processNames :: [String] -> String
processNames = intercalate ", " . sort . nub . map (map toUpper)

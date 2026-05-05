module Types where

-- Data type
data Shape
  = Circle Double
  | Rectangle Double Double
  | Triangle Double Double Double
  deriving (Show, Eq)

-- Record syntax
data Person = Person
  { personName :: String
  , personAge  :: Int
  , personEmail :: String
  } deriving (Show, Eq)

-- Newtype
newtype Name = Name String deriving (Show, Eq)

-- Type alias
type Point = (Double, Double)

-- Parameterized type
data Result a
  = Ok a
  | Err String
  deriving (Show, Eq)

-- Typeclass instance
class Describable a where
  describe :: a -> String

instance Describable Shape where
  describe (Circle r) = "Circle with radius " ++ show r
  describe (Rectangle w h) = "Rectangle " ++ show w ++ "x" ++ show h
  describe (Triangle a b c) = "Triangle(" ++ show a ++ "," ++ show b ++ "," ++ show c ++ ")"

instance Describable Person where
  describe p = personName p ++ " (age " ++ show (personAge p) ++ ")"

-- Area function
area :: Shape -> Double
area (Circle r) = pi * r * r
area (Rectangle w h) = w * h
area (Triangle a b c) = let s = (a + b + c) / 2
                         in sqrt (s * (s-a) * (s-b) * (s-c))

use crate::structs_enums::{Point, Shape};
use crate::traits::{Describable, Greetable, Container};

/// Inherent impl for Point.
impl Point {
    pub fn new(x: f64, y: f64) -> Self {
        Point { x, y }
    }

    pub fn origin() -> Self {
        Point { x: 0.0, y: 0.0 }
    }

    pub fn distance(&self, other: &Point) -> f64 {
        ((self.x - other.x).powi(2) + (self.y - other.y).powi(2)).sqrt()
    }
}

/// Trait impl for Point.
impl Describable for Point {
    fn describe(&self) -> String {
        format!("Point({}, {})", self.x, self.y)
    }
}

/// Trait impl for Shape.
impl Describable for Shape {
    fn describe(&self) -> String {
        match self {
            Shape::Circle { radius } => format!("Circle with radius {}", radius),
            Shape::Rectangle { width, height } => {
                format!("Rectangle {}x{}", width, height)
            }
            Shape::Triangle(a, b, c) => format!("Triangle({}, {}, {})", a, b, c),
        }
    }
}

/// Generic impl.
pub struct NamedItem<T> {
    pub name: String,
    pub value: T,
}

impl<T: std::fmt::Debug> Describable for NamedItem<T> {
    fn describe(&self) -> String {
        format!("{}: {:?}", self.name, self.value)
    }
}

/// Vec container impl.
pub struct VecContainer<T> {
    items: Vec<T>,
}

impl<T> Container for VecContainer<T> {
    type Item = T;

    fn items(&self) -> &[T] {
        &self.items
    }

    fn add(&mut self, item: T) {
        self.items.push(item);
    }
}

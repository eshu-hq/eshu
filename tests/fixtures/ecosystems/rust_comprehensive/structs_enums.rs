use std::fmt;

/// Regular struct.
pub struct Point {
    pub x: f64,
    pub y: f64,
}

/// Tuple struct.
pub struct Color(pub u8, pub u8, pub u8);

/// Unit struct.
pub struct Marker;

/// Enum with variants.
pub enum Shape {
    Circle { radius: f64 },
    Rectangle { width: f64, height: f64 },
    Triangle(f64, f64, f64),
}

/// Enum implementing Display.
impl fmt::Display for Shape {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Shape::Circle { radius } => write!(f, "Circle(r={})", radius),
            Shape::Rectangle { width, height } => write!(f, "Rect({}x{})", width, height),
            Shape::Triangle(a, b, c) => write!(f, "Triangle({}, {}, {})", a, b, c),
        }
    }
}

/// Result alias.
pub type AppResult<T> = Result<T, AppError>;

/// Custom error enum.
#[derive(Debug)]
pub enum AppError {
    NotFound(String),
    InvalidInput(String),
    Internal(Box<dyn std::error::Error>),
}

impl fmt::Display for AppError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AppError::NotFound(msg) => write!(f, "Not found: {}", msg),
            AppError::InvalidInput(msg) => write!(f, "Invalid input: {}", msg),
            AppError::Internal(err) => write!(f, "Internal error: {}", err),
        }
    }
}

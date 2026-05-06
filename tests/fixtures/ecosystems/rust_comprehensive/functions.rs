/// Basic function.
pub fn greet(name: &str) -> String {
    format!("Hello, {}!", name)
}

/// Function with multiple parameters.
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

/// Function returning Result.
pub fn divide(a: f64, b: f64) -> Result<f64, String> {
    if b == 0.0 {
        Err("Division by zero".to_string())
    } else {
        Ok(a / b)
    }
}

/// Function returning Option.
pub fn find_first(items: &[i32], predicate: fn(i32) -> bool) -> Option<i32> {
    items.iter().copied().find(|&x| predicate(x))
}

/// Closure as parameter.
pub fn apply<F: Fn(i32) -> i32>(value: i32, f: F) -> i32 {
    f(value)
}

/// Higher-order function returning a closure.
pub fn multiplier(factor: i32) -> impl Fn(i32) -> i32 {
    move |x| x * factor
}

/// Closure examples.
pub fn closure_examples() {
    let double = |x: i32| x * 2;
    let add_one = |x: i32| -> i32 { x + 1 };
    let compose = |f: &dyn Fn(i32) -> i32, g: &dyn Fn(i32) -> i32, x: i32| f(g(x));

    let result = compose(&double, &add_one, 5);
    assert_eq!(result, 12);
}

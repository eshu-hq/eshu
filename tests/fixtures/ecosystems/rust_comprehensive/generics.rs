use std::cmp::Ordering;

/// Generic function with trait bound.
pub fn largest<T: PartialOrd>(items: &[T]) -> Option<&T> {
    items.iter().reduce(|a, b| if a >= b { a } else { b })
}

/// Generic function with where clause.
pub fn print_all<T>(items: &[T])
where
    T: std::fmt::Display,
{
    for item in items {
        println!("{}", item);
    }
}

/// Generic struct.
pub struct Wrapper<T> {
    value: T,
}

impl<T> Wrapper<T> {
    pub fn new(value: T) -> Self {
        Wrapper { value }
    }

    pub fn into_inner(self) -> T {
        self.value
    }
}

impl<T: std::fmt::Display> Wrapper<T> {
    pub fn display(&self) {
        println!("{}", self.value);
    }
}

/// Multiple generic parameters.
pub struct KeyValue<K, V> {
    pub key: K,
    pub value: V,
}

impl<K: Eq, V> KeyValue<K, V> {
    pub fn matches_key(&self, key: &K) -> bool {
        self.key == *key
    }
}

use std::fmt;

/// Basic trait.
pub trait Describable {
    fn describe(&self) -> String;
}

/// Trait with default implementation.
pub trait Greetable {
    fn name(&self) -> &str;

    fn greet(&self) -> String {
        format!("Hello, {}!", self.name())
    }
}

/// Trait with associated type.
pub trait Container {
    type Item;

    fn items(&self) -> &[Self::Item];
    fn add(&mut self, item: Self::Item);
    fn len(&self) -> usize {
        self.items().len()
    }
}

/// Supertrait.
pub trait Printable: fmt::Display + fmt::Debug {
    fn print(&self) {
        println!("{}", self);
    }
}

/// Trait with generic method.
pub trait Converter {
    fn convert<T: From<Self>>(&self) -> T
    where
        Self: Sized + Clone,
    {
        T::from(self.clone())
    }
}

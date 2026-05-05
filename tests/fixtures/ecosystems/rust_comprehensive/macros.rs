/// Simple macro.
#[macro_export]
macro_rules! say_hello {
    () => {
        println!("Hello!");
    };
    ($name:expr) => {
        println!("Hello, {}!", $name);
    };
}

/// Macro with repetition.
#[macro_export]
macro_rules! vec_of_strings {
    ($($x:expr),*) => {
        vec![$($x.to_string()),*]
    };
}

/// Derive macros usage.
#[derive(Debug, Clone, PartialEq)]
pub struct Config {
    pub name: String,
    pub value: i32,
}

#[derive(Debug, Default)]
pub struct Builder {
    name: Option<String>,
    value: Option<i32>,
}

impl Builder {
    pub fn name(mut self, name: &str) -> Self {
        self.name = Some(name.to_string());
        self
    }

    pub fn value(mut self, value: i32) -> Self {
        self.value = Some(value);
        self
    }

    pub fn build(self) -> Config {
        Config {
            name: self.name.unwrap_or_default(),
            value: self.value.unwrap_or(0),
        }
    }
}

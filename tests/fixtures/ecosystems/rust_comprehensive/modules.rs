//! Module organization patterns.

pub mod utils {
    pub fn helper() -> String {
        "helper".to_string()
    }
}

pub mod models {
    pub struct User {
        pub name: String,
    }

    impl User {
        pub fn new(name: &str) -> Self {
            User {
                name: name.to_string(),
            }
        }
    }
}

// Re-export
pub use models::User;
pub use utils::helper;

// Glob import usage
use std::collections::*;

pub fn use_collections() -> HashMap<String, Vec<String>> {
    let mut map = HashMap::new();
    map.insert("key".to_string(), vec!["value".to_string()]);
    map
}

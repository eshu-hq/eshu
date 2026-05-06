/// Function with lifetime annotations.
pub fn longest<'a>(x: &'a str, y: &'a str) -> &'a str {
    if x.len() > y.len() {
        x
    } else {
        y
    }
}

/// Struct with lifetime.
pub struct Excerpt<'a> {
    pub text: &'a str,
}

impl<'a> Excerpt<'a> {
    pub fn new(text: &'a str) -> Self {
        Excerpt { text }
    }

    pub fn first_word(&self) -> &str {
        self.text.split_whitespace().next().unwrap_or("")
    }
}

/// Static lifetime.
pub fn static_string() -> &'static str {
    "I live forever"
}

/// Multiple lifetimes.
pub fn pick_first<'a, 'b>(x: &'a str, _y: &'b str) -> &'a str {
    x
}

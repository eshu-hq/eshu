# Rust Sample Fixture

Parser, indexing, relationship, and content tests use this directory as Rust
source input. It is test data, not a production Rust template.

| File | Contract |
| --- | --- |
| `src/lib.rs` | Library entry point and module declarations. |
| `src/basic_functions.rs` | Functions, ownership, borrowing, tuples, options, results, and recursion. |
| `src/structs_enums.rs` | Structs, tuple/unit structs, enums, methods, constructors, and display impls. |
| `src/traits.rs` | Traits, impls, defaults, associated items, bounds, and trait objects. |
| `src/error_handling.rs` | Custom errors, `Result`, `Option`, propagation, conversion, and validation. |
| `src/lifetimes_references.rs` | Explicit lifetimes, elision, structs with lifetimes, and HRTBs. |
| `src/generics.rs` | Generic functions/types, bounds, collections, and const generics. |
| `src/concurrency.rs` | Threads, `Arc`, `Mutex`, `RwLock`, channels, pools, and atomics. |
| `src/iterators_closures.rs` | Custom iterators, adapters, closure traits, and lazy chains. |
| `src/smart_pointers.rs` | `Box`, `Rc`, `Arc`, `RefCell`, `Weak`, `Cow`, and `Drop`. |
| `src/modules.rs` | Nested modules, visibility, re-exports, and module paths. |
| `Cargo.toml` | Cargo package metadata. |

Tests should prove consistent module paths, `pub use` re-exports, stable entity
identifiers, repo-relative source paths, and parser tolerance for ownership,
lifetime, generic, and concurrency-heavy syntax.

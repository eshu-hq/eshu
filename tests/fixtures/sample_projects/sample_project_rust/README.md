# Rust Sample Project

This fixture exercises Rust parser, indexing, relationship, and content shape
behavior. It is test data, not a production Rust template.

## Fixture Map

| File | Coverage |
| --- | --- |
| `src/lib.rs` | library entry point and module declarations |
| `src/basic_functions.rs` | functions, ownership, borrowing, tuples, options, results, recursion |
| `src/structs_enums.rs` | structs, tuple/unit structs, enums, methods, constructors, display impls |
| `src/traits.rs` | traits, impls, defaults, associated items, bounds, trait objects |
| `src/error_handling.rs` | custom errors, `Result`, `Option`, propagation, conversion, validation |
| `src/lifetimes_references.rs` | explicit lifetimes, elision, structs with lifetimes, HRTBs |
| `src/generics.rs` | generic functions/types, bounds, collections, const generics |
| `src/concurrency.rs` | threads, `Arc`, `Mutex`, `RwLock`, channels, pools, atomics |
| `src/iterators_closures.rs` | custom iterators, adapters, closure traits, lazy chains |
| `src/smart_pointers.rs` | `Box`, `Rc`, `Arc`, `RefCell`, `Weak`, `Cow`, `Drop` |
| `src/modules.rs` | nested modules, visibility, re-exports, module paths |
| `Cargo.toml` | Cargo package metadata |

## What Tests Should Prove

- Rust module paths and `pub use` re-exports are indexed consistently.
- Functions, structs, enums, traits, impls, and associated items get stable
  entities.
- Ownership, lifetime, generic, and concurrency-heavy syntax does not break
  parsing.
- Fixture graph/content tests can cite repo-relative source paths.

## Local Fixture Commands

```bash
cargo check
cargo test
cargo clippy
```

These commands are optional for Eshu repository tests unless a test explicitly
compiles this fixture.

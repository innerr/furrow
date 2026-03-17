## Rust Coding Standards

* **Formatting**: Run `cargo fmt` before committing
* **Linting**: Run `cargo clippy -- -D warnings` before committing
* **Import Grouping**: Organize imports in three groups separated by blank lines:
  1. Rust standard library (`std::*`)
  2. External crates (`serde`, `tokio`, etc.)
  3. Local modules (`crate::*`)
* **Error Handling**:
  * Use `thiserror` for custom error types in library code
  * Use `anyhow` for error handling in application code
* **Testing**: New features should include corresponding unit tests
* **Documentation**: Public APIs must have documentation comments (`///`)

---
id: rust
description: Rust and cargo, checking, one test, and caches
files: [Cargo.toml]
check: [cargo, --version]
enabled: false
---
Rust: cargo check finds type errors faster than cargo build; cargo test name_filter runs matching tests only. When ~/.cargo cannot be written in the sandbox, set CARGO_HOME=$TMPDIR/cargo for commands that fetch nothing.

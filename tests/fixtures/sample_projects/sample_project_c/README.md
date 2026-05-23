# C Sample Fixture

Parser and indexing tests use this directory as C source input. It is fixture
data, not a supported sample application.

| Path | Contract |
| --- | --- |
| `Makefile` | Simple build metadata for discovery tests. |
| `include/config.h` | Macros and conditional compilation. |
| `include/platform.h` | Platform-specific declarations. |
| `include/util.h` | Inline functions and function declarations. |
| `include/module.h` | Typedefs, enums, and extern declarations. |
| `include/math/vec.h` | Structs and function declarations. |
| `src/*.c` | Includes, definitions, calls, and static symbols. |
| `src/math/vec.c` | Nested source layout. |

Tests should prove source/header relationships and C symbol extraction without
requiring a full build or relying on the checked-in `eshu_sample` binary.

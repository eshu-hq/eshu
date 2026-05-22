# C Parser Fixture

This fixture is a tiny dependency-free C project used to exercise Eshu's C
indexing behavior. It is fixture input, not a supported sample application.

## Fixture Map

| Path | Parser surface |
| --- | --- |
| `Makefile` | simple build metadata for discovery tests |
| `include/config.h` | macros and conditional compilation |
| `include/platform.h` | platform-specific declarations |
| `include/util.h` | inline functions and function declarations |
| `include/module.h` | typedefs, enums, extern declarations |
| `include/math/vec.h` | structs and function declarations |
| `src/*.c` | includes, definitions, calls, static symbols |
| `src/math/vec.c` | nested directory source layout |

## What Tests Should Prove

- Header includes, macros, typedefs, enums, structs, extern variables, static
  variables, inline functions, and function-pointer typedefs are discovered
  without requiring a full build.
- Source-to-header relationships remain stable across the root `src/` files and
  the nested `src/math/` directory.
- Parser changes should not depend on the checked-in `eshu_sample` binary; the
  source files are the fixture contract.

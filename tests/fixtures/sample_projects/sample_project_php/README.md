# PHP Parser Fixture

This fixture exercises PHP parser behavior across common language surfaces. It
is test input for Eshu, not a runnable application guide.

## Fixture Map

| File | Parser surface |
| --- | --- |
| `functions.php` | functions, anonymous callbacks, closures, variadics |
| `classes_objects.php` | classes, properties, methods, object creation |
| `Inheritance.php` | inheritance and abstract classes |
| `interface_traits.php` | interfaces and traits |
| `error_handling.php` | custom exceptions and try/catch/finally |
| `file_handling.php` | file I/O calls |
| `database.php` | PDO-style database calls |
| `generators_iterators.php` | `yield` and iterator implementation |
| `edgecases.php` | type juggling and array/null edge cases |
| `globals_superglobals.php` | superglobals and `$GLOBALS` usage |

## What Tests Should Prove

- PHP parser changes keep functions, callbacks, object-oriented constructs,
  errors, file/database calls, generators, iterators, globals, and type-edge
  cases visible to the graph.
- Fixture intent is source-shape coverage. Do not add external database or
  runtime requirements to parser tests that consume this tree.

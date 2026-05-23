# PHP Parser Fixture

This fixture exercises PHP parser behavior across common language surfaces. It
is test input for Eshu, not a runnable application guide.

| File | Contract |
| --- | --- |
| `functions.php` | Functions, anonymous callbacks, closures, and variadics. |
| `classes_objects.php` | Classes, properties, methods, and object creation. |
| `Inheritance.php` | Inheritance and abstract classes. |
| `interface_traits.php` | Interfaces and traits. |
| `error_handling.php` | Custom exceptions and `try`/`catch`/`finally`. |
| `file_handling.php` | File I/O calls. |
| `database.php` | PDO-style database calls. |
| `generators_iterators.php` | `yield` and iterator implementation. |
| `edgecases.php` | Type juggling and array/null edge cases. |
| `globals_superglobals.php` | Superglobals and `$GLOBALS` usage. |

Tests should prove these source shapes stay visible to the graph without
adding external database or runtime requirements.

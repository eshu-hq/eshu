# Java Sample Fixture

This fixture is a small dependency-free Java project used to exercise Eshu's
Java parser. It mirrors common application relationships without depending on a
build tool.

| Path | Contract |
| --- | --- |
| `src/com/example/app/Main.java` | Entry point, object creation, and calls. |
| `src/com/example/app/model/*.java` | Enum and model types. |
| `src/com/example/app/service/*.java` | Interface and abstract class surfaces. |
| `src/com/example/app/service/impl/*.java` | Implementation relationship. |
| `src/com/example/app/util/*.java` | Generics, streams, and I/O calls. |
| `src/com/example/app/annotations/Logged.java` | Custom annotation. |
| `src/com/example/app/misc/Outer.java` | Inner class. |

Tests should prove package/import, interface-to-implementation, abstract-class,
enum, generic, exception, annotation, inner-class, lambda, stream, I/O, and
thread surfaces without using checked-in `out/` classes or `sources.txt`.

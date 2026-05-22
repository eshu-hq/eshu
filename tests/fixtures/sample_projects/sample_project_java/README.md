# Java Parser Fixture

This fixture is a small dependency-free Java project used to exercise Eshu's
Java parser. It mirrors common application relationships without depending on a
build tool.

## Fixture Map

| Path | Parser surface |
| --- | --- |
| `src/com/example/app/Main.java` | entry point, object creation, calls |
| `src/com/example/app/model/*.java` | enum and model types |
| `src/com/example/app/service/*.java` | interface and abstract class |
| `src/com/example/app/service/impl/*.java` | implementation relationship |
| `src/com/example/app/util/*.java` | generics, streams, I/O calls |
| `src/com/example/app/annotations/Logged.java` | custom annotation |
| `src/com/example/app/misc/Outer.java` | inner class |

## What Tests Should Prove

- Package, import, interface-to-implementation, abstract-class, enum, generic,
  exception, annotation, inner-class, lambda, stream, I/O, and thread surfaces
  are discovered consistently.
- Relationship extraction should use the source tree, not the checked-in
  `out/` classes or `sources.txt` helper.
- Parser changes should preserve fixture intent without adding runtime or build
  assumptions.

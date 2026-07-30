# meyer

A pure Go parser for the SQLite SQL dialect. Zero dependencies, hand-written
recursive descent — no parser generators.

*(a Meyer lemon, for SQLite's [Lemon](https://sqlite.org/lemon.html) parser
generator)*

meyer is being built to replace the ANTLR-generated SQLite parser in
[sqlc](https://github.com/sqlc-dev/sqlc). See [PLAN.md](PLAN.md) for the
architecture and [CLAUDE.md](CLAUDE.md) for the development workflow.

## Status

Under construction: the test corpus and tooling exist; the parser itself is
being implemented against them.

## Conformance

meyer is tested against SQL extracted from SQLite's own test suite, with
expectations produced by a pinned build of SQLite itself: meyer must accept
exactly the statements SQLite's parser accepts, and reproduce SQLite's exact
syntax-error messages. See [parser/testdata/README.md](parser/testdata/README.md)
for provenance and the pinned version.

## Acknowledgments

SQLite and its test suite are public domain — https://sqlite.org/copyright.html.
This project's grammar and test corpus derive from them.

## License

MIT

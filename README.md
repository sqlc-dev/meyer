# meyer

A pure Go parser for the SQLite SQL dialect. Zero dependencies, hand-written
recursive descent — no parser generators.

*(a Meyer lemon, for SQLite's [Lemon](https://sqlite.org/lemon.html) parser
generator)*

meyer is being built to replace the ANTLR-generated SQLite parser in
[sqlc](https://github.com/sqlc-dev/sqlc). See [PLAN.md](PLAN.md) for the
architecture and [CLAUDE.md](CLAUDE.md) for the development workflow.

## Usage

```go
stmts, err := parser.Parse(ctx, strings.NewReader("SELECT id FROM users WHERE id = ?"))
```

`ParseString` and `ParseStatement` take a string; `ParseExpr` parses a single
expression. A rejected input returns a `*parser.Error` carrying SQLite's
exact message and the byte offset of the fault.

Every node embeds `ast.Span`, so `Pos()` and `End()` give byte offsets into
the original input — sqlc slices the source with them to find `-- name:`
comments and to report errors, so they are load-bearing rather than
diagnostic.

To see what the parser did with something:

```sh
go run ./cmd/debug-parse 'SELECT sum(x) OVER w FROM t'   # the tree
go run ./cmd/debug-parse -tokens 'SELECT 1'              # the token stream
go run ./cmd/debug-parse -render -f query.sql            # re-rendered SQL
```

## Status

The parser covers the whole grammar of the pinned SQLite release and passes
the full corpus: **20,971 of 20,971 cases**, extracted from every test script
in SQLite's own test suite.

Still to come, from [PLAN.md](PLAN.md): the sqlc integration itself, and the
optional sqllogictest smoke gate.

## Conformance

There is no upstream parse-tree oracle — SQLite cannot dump its parse tree —
so conformance is defined three ways, in decreasing order of authority:

1. **Accept/reject against SQLite itself.** SQL is extracted from SQLite's
   test suite and run through a pinned SQLite build. meyer must accept
   exactly what SQLite's parser accepts, and on rejection produce the
   identical message and byte offset. See
   [parser/testdata/README.md](parser/testdata/README.md) for provenance and
   the pinned version.
2. **A round-trip property.** Every accepting case is rendered back to SQL,
   re-parsed, and the two trees compared. This catches dropped clauses and
   mis-associated operators, which accept/reject cannot see.
3. **AST snapshots.** A hand-written tour of the node set under
   `parser/testdata/ast`, reviewed in diffs. These are meyer's own goldens
   and never outrank the oracle.

Plus two searches for cases the corpus does not contain: a fuzz target
asserting that arbitrary bytes terminate without panicking, that a rejection
is always a `*parser.Error`, and that anything accepted survives the round
trip; and `cmd/difftest`, which checks meyer against a live SQLite build,
message and byte offset included, over corpus SQL and the ~36k inputs of
SQLite's own `fuzzdata` databases — each one directly and then mutated a
token at a time.

## Acknowledgments

SQLite and its test suite are public domain —
https://sqlite.org/copyright.html. meyer's grammar and test corpus derive
from them; `internal/reference/` keeps the upstream `parse.y` and
`tokenize.c` alongside the port, as documentation.

## License

MIT

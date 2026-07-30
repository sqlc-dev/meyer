# meyer development guide

meyer is a hand-written, zero-dependency Go parser for the SQLite SQL
dialect. Read PLAN.md first — it defines the architecture, the AST shape,
and what is deliberately out of scope. The grammar ground truth is SQLite's
`parse.y` (Lemon) and `tokenize.c`; the dialect surface is
https://sqlite.org/lang.html.

## Rules

- **Zero dependencies.** `go.mod` must never gain a `require` line.
- **No parser generators.** Everything is hand-written recursive descent.
- **Never edit `parser/testdata/*.test` by hand.** Corpus files are produced
  by `cmd/regenerate-parse` from SQLite's own test suite plus a real SQLite
  build (the oracle). `*.metadata.json` sidecars are updated by tooling
  (`go test ./parser -check-parse`), not by hand.
- Every nontrivial parse function carries a comment naming the `parse.y`
  rule(s) it implements.
- Error messages must match SQLite's parser byte-for-byte
  (`near "X": syntax error`, `unrecognized token: "X"`, `incomplete input`).

## The corpus

Each `parser/testdata/<name>.test` holds cases extracted from SQLite's
`test/<name>.test` TCL scripts. For every case, the raw oracle results (one
line per statement: prepared OK, or the exact error message and offset) are
stored; the harness derives the expectation from them:

- If any statement failed with a **syntax-family** message (`near "…":
  syntax error`, `unrecognized token: "…"`, `incomplete input`), meyer must
  reject the case with that first message.
- Otherwise meyer must accept the whole case. This includes statements that
  failed **semantically** in SQLite (`no such table: …`) — those parsed
  successfully; meyer does no semantic analysis.
- …except in text SQLite's parser never reached. A grammar action can fail
  in the middle of a statement — `sqlite3BeginTrigger` raising `no such
  table` at the `trigger_decl` reduce, before the trigger body is looked at
  — and `sqlite3RunParser` then abandons the rest of the statement. The
  oracle records `pzTail` on every failing statement, so the harness knows
  which byte ranges are unverified and lets meyer fail inside them.

Known looseness: messages produced by grammar *actions* (e.g. `ORDER BY
clause should come after UNION not before`) are currently classified as
semantic, so meyer is permitted to accept such statements. The pattern list
lives in `internal/testfile` (`syntaxFamily`) and can be extended without
regenerating the corpus, because the corpus stores raw oracle output.

## The loop

```sh
go run ./cmd/next-test                 # pick the next todo case to implement
# ... implement in lexer/, ast/, parser/ ...
go test ./parser -run TestParse -check-parse -v 2>&1 | grep "PARSE PASSES NOW"
go test ./...                          # everything else still green
# commit code + metadata.json changes together
```

`-check-parse` re-runs todo cases and deletes metadata entries for cases
that now pass. Never mark a case done by hand, and never "fix" a failing
case by editing the corpus — if you believe a corpus expectation is wrong,
the fix is in `cmd/regenerate-parse` (or the classification in
`internal/testfile`), followed by a full regeneration and a reviewed diff.

## Regenerating the corpus

```sh
go run ./cmd/regenerate-parse                  # the full starter set
go run ./cmd/regenerate-parse -files select1   # one file
```

The tool downloads the pinned SQLite release (version + SHA-256 constants in
`cmd/regenerate-parse/main.go`) into `.sqlite/` (gitignored), compiles the
oracle with the system C compiler, and rewrites corpus + metadata (new cases
start as todo; existing case states are preserved). Advancing the pin means
updating the constants and `parser/testdata/README.md`, regenerating
everything, and reviewing the diff.

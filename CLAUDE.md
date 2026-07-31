# meyer development guide

meyer is a hand-written, zero-dependency Go parser for the SQLite SQL
dialect. Read PLAN.md first — it defines the architecture, the AST shape,
and what is deliberately out of scope. The grammar ground truth is SQLite's
`parse.y` (Lemon) and `tokenize.c`; the dialect surface is
https://sqlite.org/lang.html.

## Rules

- **Zero dependencies.** `go.mod` must never gain a `require` line.
- The keyword table and the `%fallback` set are transcriptions, and tests in
  `token` rebuild both from the vendored upstream sources to keep them
  honest. Advancing the pin surfaces a keyword change as a failure.
- **No parser generators.** Everything is hand-written recursive descent.
- **Never edit `parser/testdata/*.test` by hand.** Corpus files are produced
  by `cmd/regenerate-parse` from SQLite's own test suite plus a real SQLite
  build (the oracle). `*.metadata.json` sidecars are updated by tooling
  (`go test ./parser -check-parse`), not by hand. The hand-written snapshot
  inputs under `parser/testdata/ast/` are the exception — see below.
- Every nontrivial parse function carries a comment naming the `parse.y`
  rule(s) it implements. `TestEveryRuleIsNamed` enforces this from the other
  side: no nonterminal of the vendored grammar may go unmentioned.
- Error messages must match SQLite's parser byte-for-byte
  (`near "X": syntax error`, `unrecognized token: "X"`, `incomplete input`).
- Grammar that a build option turns on — `SQLITE_ENABLE_UPDATE_DELETE_LIMIT`
  so far — is a `parser.Options` field, off by default: the corpus is
  generated from the pinned build, so the default has to stay exactly where
  that build is. Expectations for an option come from a SQLite compiled with
  it, which takes a Lemon run rather than a `-D` on the amalgamation; the
  recipe is at the top of `parser/options_test.go`.

## The corpus

Each `parser/testdata/<name>.test` holds cases extracted from SQLite's
`test/<name>.test` TCL script; there is one for every script in the pinned
source tree that yields at least one literal-SQL block. For every case, the
raw oracle results (one line per statement: prepared OK, or the exact error
message, offset and parse tail) are stored; the harness derives the
expectation from them:

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

## Tree shape

Accept/reject conformance cannot see a dropped clause or a mis-associated
operator, so two further checks stand in for the parse-tree goldens SQLite
cannot produce:

- **Round trip** (`TestRoundTrip`): every corpus case that parses is
  rendered back to SQL with `ast.Statements`, re-parsed, and the two trees
  compared structurally with `internal/dump`. Spans and `Raw` fields are
  excluded — the renderer promises re-parseability and nothing else.
- **Snapshots** (`TestASTSnapshots`): `parser/testdata/ast/*.sql` are
  hand-written and meant to be edited; their `.tree` goldens are rewritten
  with `go test ./parser -update` and reviewed in the diff.

## Differential testing

The corpus is whatever SQLite's test suite happens to contain, which is
overwhelmingly valid SQL: fewer than three hundred of its 20,971 cases are
rejections, so error fidelity is barely exercised by it. `cmd/difftest`
covers that gap by mutating corpus SQL — truncate, delete, duplicate,
replace or insert one token — and checking that meyer and a live SQLite
build still agree on the verdict, the message and the offset.

```sh
go run ./cmd/difftest                        # sweep the whole corpus
go run ./cmd/difftest -files select1,expr    # a few files
go run ./cmd/difftest -per 60 -seed 7        # dig harder, reproducibly
```

It needs the oracle, so it is not part of `go test ./...`; a separate
workflow (`.github/workflows/difftest.yml`) runs it weekly and on demand.
What it finds belongs in `parser/errors_test.go`, whose expectations are
taken from the oracle rather than written by hand.

Two situations it deliberately skips, because the harness cannot compare
them rather than because meyer might be wrong: a `;` inside parentheses,
where `sqlite3_complete` splits statements differently from a real parse,
and a semantic failure on a statement's last token, which leaves `pzTail` at
the end of the input with nothing to mark the parse as abandoned.

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
go run ./cmd/regenerate-parse                  # every script in the pinned tree
go run ./cmd/regenerate-parse -files select1   # one file
```

The tool downloads the pinned SQLite release (version + SHA-256 constants in
`cmd/regenerate-parse/main.go`) into `.sqlite/` (gitignored), compiles the
oracle with the system C compiler, and rewrites corpus + metadata (new cases
start as todo; existing case states are preserved). Advancing the pin means
updating the constants and `parser/testdata/README.md`, regenerating
everything, and reviewing the diff.

# meyer — a pure Go parser for the SQLite SQL dialect

*(a Meyer lemon, for SQLite's [Lemon](https://sqlite.org/lemon.html) parser generator)*

meyer is a hand-written, zero-dependency Go parser for the SQLite SQL dialect,
in the same family as:

- [sqlc-dev/zetajones](https://github.com/sqlc-dev/zetajones) — GoogleSQL (BigQuery)
- [sqlc-dev/teesql](https://github.com/sqlc-dev/teesql) — T-SQL (SQL Server)
- [sqlc-dev/doubleclick](https://github.com/sqlc-dev/doubleclick) — ClickHouse

Its immediate purpose is to replace
[`sqlc/internal/engine/sqlite/parser`](https://github.com/sqlc-dev/sqlc/tree/main/internal/engine/sqlite/parser),
the ANTLR-generated parser currently used by sqlc. ANTLR is explicitly out of
scope: no parser generators of any kind, no runtime dependencies.

The dialect surface is defined by https://sqlite.org/lang.html, with the
authoritative grammar being SQLite's `parse.y` (Lemon grammar, vendored into
this repo under `internal/reference/` for documentation only — it is public
domain and never processed by any tool).

## What carries over from the sibling parsers

These decisions worked in zetajones/teesql/doubleclick and are adopted
unchanged:

1. **Hand-written recursive descent.** No grammar files feed any tool. The
   grammar lives in Go functions, one `parse*` method per production, each
   carrying a comment naming the `parse.y` rule it implements
   (zetajones-style attribution comments, pointing at rules like
   `oneselect ::= SELECT distinct selcollist from where_opt ...`).
2. **Zero dependencies.** `go.mod` contains the module line and the Go
   version. Nothing else, ever.
3. **Package layout**: `token`, `lexer`, `ast`, `parser`, `internal/...`,
   `cmd/...`.
4. **Eager lexer.** `lexer.Lex(src) ([]token.Token, error)` produces the whole
   token slice up front (zetajones model — simplest, and SQL inputs are
   small). Tokens carry byte offsets only; line/col is computed on demand
   from the offset.
5. **Corpus-driven development loop**: vendored test corpus + per-case
   metadata with `todo` flags, `cmd/next-test` to pick the next failing case,
   a `-check-parse` test flag that re-runs todos and rewrites metadata when
   they start passing, and a hard rule that expected outputs are never edited
   by hand.
6. **Fail-fast, single-error reporting.** `parser.Error{Message, Offset,
   SQL}` — first error aborts, like SQLite itself (SQLite reports exactly one
   parse error per statement).
7. **CI**: GitHub Actions, `go build ./...` + `go test -race ./...`.
8. **Public API** mirrors zetajones so sqlc integration is uniform:

   ```go
   package parser // github.com/sqlc-dev/meyer/parser

   func Parse(ctx context.Context, r io.Reader) ([]ast.Stmt, error)
   ```

   plus `ParseStatement`, `ParseExpr` helpers for tests and tooling.

## How meyer differs from the sibling parsers

### 1. The source grammar is LALR with deliberate ambiguity resolutions

zetajones ported a grammar designed for LL-style consumption; teesql and
doubleclick ported parsers that were already recursive descent. meyer ports a
**Lemon LALR grammar** whose conflict resolutions are encoded in precedence
annotations and rule ordering. These must be replicated consciously in
recursive descent, and each gets a dedicated comment + test:

- **`%fallback ID`** — ~80 of SQLite's ~150 keywords fall back to plain
  identifiers when they can't parse as keywords (`ABORT`, `ACTION`, ...,
  `REPLACE`, `VIEW`, ...). SQLite additionally defines token *classes* used
  throughout the grammar:
  - `id` = `ID | INDEXED`
  - `ids` = `ID | STRING`
  - `idj` = `ID | INDEXED | JOIN_KW`
  - `nm` = `idj | STRING` — the "name" production used for table/column names
  - `JOIN_KW` = `NATURAL LEFT RIGHT FULL OUTER INNER CROSS`,
    `LIKE_KW` = `LIKE GLOB REGEXP`, `CTIME_KW` = `CURRENT_TIME
    CURRENT_DATE CURRENT_TIMESTAMP`

  Design: the lexer tags every keyword with its keyword kind (SQLite-style),
  and the parser routes all identifier-position consumption through a small
  set of helpers (`expectName`, `expectId`, `expectIdOrString`,
  `expectIdj`) that encode exactly the fallback sets. `SELECT` can never be a
  bare identifier; `REPLACE` can. Getting this table right is most of the
  battle for corpus conformance (`keyword1.test` exercises it exhaustively).
- **Join `ON` vs `ON CONFLICT`** — in `INSERT INTO t SELECT ... FROM a JOIN b
  ON ...`, SQLite always binds `ON` to the join (the `[OR]` precedence mark on
  the empty `on_using` rule). Recursive descent gets this for free by parsing
  join constraints greedily, but the error behavior for
  `... JOIN b ON CONFLICT ...` must match (CONFLICT falls back to an
  identifier expression).
- **`LIMIT x, y`** — the comma form means `LIMIT y OFFSET x` (operands
  swapped). Easy to get wrong.
- **Window clauses** — `OVER (name PARTITION BY ...)` vs `OVER name`, base
  window names (`window ::= nm frame_opt`), `FILTER (WHERE ...)` with or
  without `OVER`. Requires two-token lookahead in a few places.
- **Expression precedence** — 11 levels, taken verbatim from the `%left` /
  `%right` / `%nonassoc` block in `parse.y` (OR < AND < NOT < IS/MATCH/LIKE/
  BETWEEN/IN/ISNULL/NOTNULL/NE/EQ < GT/LE/LT/GE < ESCAPE < BITAND/BITOR/
  LSHIFT/RSHIFT < PLUS/MINUS < STAR/SLASH/REM < CONCAT/PTR < COLLATE <
  BITNOT). Implemented as a precedence-climbing loop (doubleclick-style Pratt)
  rather than a cascade of 11 functions, since SQLite's operator set is small
  and closed. Special forms handled as postfix/ternary operators at the right
  levels: `COLLATE`, `ESCAPE`, `ISNULL`/`NOTNULL`/`NOT NULL`,
  `IS [NOT] [DISTINCT FROM]`, `[NOT] BETWEEN ... AND ...`, `[NOT] IN`,
  `LIKE/GLOB/REGEXP/MATCH ... [ESCAPE ...]`, `->` / `->>` (PTR).
- **`typetoken`** — column types are *one or more* identifier/string tokens
  concatenated (`UNSIGNED BIG INT`), optionally followed by `(signed)` or
  `(signed, signed)`. No sibling dialect does this. The AST stores the raw
  span plus the joined name.
- **Vector expressions** — `(a, b) = (1, 2)`, `(a, b) IN (SELECT ...)`.
- **Multi-row `VALUES`** and compound selects share machinery
  (`VALUES (...), (...)` is a compound of single-row VALUES in the grammar);
  the AST models VALUES as its own node, not a desugared compound.
- Constructs in `parse.y` that exist only for SQLite internals are
  deliberately **rejected**: `#NNN` register references (nested-parse only)
  produce a syntax error exactly as user-facing SQLite does.

### 2. No upstream parse-tree oracle — the oracle is SQLite itself

This is the biggest structural difference:

| repo | golden output | generated by |
|---|---|---|
| zetajones | `ASTNode::DebugString` trees | vendored from upstream testdata |
| teesql | ScriptDom JSON | bundled C# tool + Microsoft NuGet parser |
| doubleclick | `EXPLAIN AST` text | pinned ClickHouse binary |
| **meyer** | **accept/reject + exact error message + error offset** | **pinned SQLite build** |

SQLite has no way to dump its parse tree. What it does have is:

- deterministic, low-entropy error messages — `near "X": syntax error`,
  `unrecognized token: "X"`, `incomplete input` — and
- `sqlite3_error_offset()` for the error's byte position.

So conformance is defined as: **for every corpus statement, meyer accepts iff
SQLite's parser accepts, and on rejection produces the identical message and
offset.** Statements that fail in SQLite *after* parsing (e.g. `no such
table`) count as must-accept for meyer.

Because accept/reject alone would not pin down tree *shape*, two additional
layers substitute for upstream goldens:

1. **Self-maintained AST snapshots.** `internal/dump` renders a stable,
   human-reviewable tree (Go-syntax-ish, positions included). A curated
   subset of the corpus gets committed snapshots. These are our own goldens —
   regenerated by tooling, reviewed by humans in diffs, never authoritative
   over the accept/reject oracle.
2. **Round-trip property.** meyer ships a minimal SQL renderer
   (`ast.String()` per node) used only for testing: `parse(render(parse(x)))`
   must equal `parse(x)`. This catches structural mistakes (wrong
   associativity, dropped clauses) that accept/reject can't see, without
   needing upstream tree dumps. The renderer is not a formatter and makes no
   fidelity promises beyond re-parseability.

### 3. The corpus must be extracted, not copied

Sibling repos copied upstream test files verbatim. SQLite's tests are TCL
scripts (`test/*.test`, ~1,200 files, ~13,000 extractable
`do_execsql_test` / `do_catchsql_test` blocks whose SQL is in literal braces).
Research findings (all paths in the SQLite source tree, which is public
domain):

- **Primary corpus**: literal-SQL blocks from `test/*.test`. ~95% contain no
  TCL substitution and can be extracted with a brace-matching scanner. The
  richest files for grammar coverage: `parser1.test`, `tokenize.test`,
  `keyword1.test`, `select1`–`selectH`, `expr*`, `with1/with2`,
  `window1`–`windowE` + `windowerr`, `altertab*`, `alterdropcol*`,
  `trigger1`, `upsert*`, `returning1`, and the evidence files
  (`e_select`, `e_expr`, `e_createtable`, `e_insert`, `e_update`,
  `e_delete`) which map 1:1 to sentences of the documented grammar.
- **Negative subset**: the ~160 `catchsql` assertions whose expected message
  contains `syntax error` / `unrecognized token` — precise must-reject cases
  with expected messages.
- **Robustness corpus**: `test/fuzzdata{1,2,4,5,6,8}.db` contain ~36k SQL
  text fuzz inputs (`xsql` tables) — no oracle, invariant is "terminate
  without panic". Used as fuzz seeds, not vendored wholesale.
- **sqllogictest** (~millions of statements, license: "no attribution
  required") — bulk smoke corpus; low grammar diversity. Optional, behind an
  env-var-gated test, not vendored.

Tooling (`cmd/regenerate`):

1. Extracts statements + expectations from a pinned SQLite source checkout
   (TCL brace scanner; skips cases with `$`/`[` substitution).
2. Runs every statement through a **pinned SQLite build** (the official
   amalgamation compiled once into a tiny C oracle binary that calls
   `sqlite3_prepare_v2` statement-by-statement against `:memory:` and reports
   `ok` / error-message + `sqlite3_error_offset`; classification: message in
   the syntax-error family → must-reject, anything else → must-accept).
3. Writes the corpus in a **consolidated format**: one `.test` file per
   upstream file (zetajones-style: cases separated by `==`, SQL / expectation
   separated by `--`), plus one `metadata.json` sidecar per file for
   todo/skip tracking.

Consolidated files are a deliberate correction to doubleclick's layout
(127k files / 570 MB / one directory per case). Expected size here:
single-digit MB, a few hundred files.

The oracle SQLite version is pinned (source tarball hash recorded in the
corpus README). Feature gates in `parse.y` (`%ifndef SQLITE_OMIT_*`,
`%ifdef SQLITE_ENABLE_*`) are resolved the same way the pinned build resolves
them — meyer implements exactly what that build accepts, including
`WITHIN GROUP` ordered-set aggregates and the new `ALTER TABLE ... ADD/DROP
CONSTRAINT` forms if (and only if) the pinned build has them. When the pin
advances, `cmd/regenerate` re-derives expectations and diffs are reviewed.

### 4. Positions are load-bearing (sqlc integration)

teesql has no positions at all; doubleclick's `End()` is vestigial. meyer
cannot afford either shortcut, because sqlc uses byte spans to:

- compute `RawStmt.StmtLocation` / `StmtLen` per statement (it slices the
  original source with these to find `-- name:` comments), and
- report errors with locations.

Design (zetajones model, upgraded to be universal):

- Every AST node embeds `Span{Start, End int}` (byte offsets into the
  original input); `Pos()`/`End()` on the `Node` interface, `Children()` for
  generic traversal, JSON tags on everything.
- `[]ast.Stmt` from `Parse` covers the full input: each statement's span runs
  from its first token to its terminating semicolon (or EOF), so sqlc's
  comment-scanning between statements keeps working. Empty statements (bare
  `;`) are skipped but never break span accounting.
- `parser.Error` carries the byte offset (analog of
  `sqlite3_error_offset`) and renders `near "X": syntax error [at line:col]`.

Parameter markers are first-class AST nodes — this is the single most
important part of the tree for sqlc's parameter inference:

- `?` (anonymous, auto-numbered), `?NNN` (explicit number),
  `:name`, `@name`, `$name` (including TCL-style `$foo::bar(baz)` suffixes,
  which SQLite's tokenizer accepts).
- `ast.BindParam{Span, Kind, Number, Name}` preserving the raw spelling.

### 5. The tokenizer is a port of `tokenize.c`, quirks included

More unusual lexical territory than any sibling:

- **Four quoting styles**: `'...'` string, `"..."` identifier (with SQLite's
  historical "double-quoted string" leniency left to consumers — the token is
  ID; the AST records the quote style), `` `...` `` (MySQL style) and
  `[...]` (MS style) identifiers. Doubled-quote escapes inside all of them.
- **Blob literals** `x'ABCD'` / `X'abcd'` with validation (even length, hex
  only) at lex time, exactly as `tokenize.c` does.
- **Four bind-parameter styles** (above).
- **Numbers**: hex `0x...`, floats `1.`, `.5`, `1e6`, `1_000` digit
  separators (recent SQLite; `QNUMBER` handling), and the rule that
  `1abc` is `unrecognized token`, not `1` followed by `abc`.
- **Operators**: `||`, `->`, `->>`, `==`/`=`, `<>`/`!=`, `<<`, `>>`, and the
  rule that a lone `!` or `|` is an error token.
- **Comments**: `--` to end of line, `/* ... */` (unterminated block comment
  is treated as trailing whitespace, not an error — `tokenize.test` covers
  this). Comments and whitespace are skipped by the parser but their spans
  must not be attributed to any token (statement spans depend on it).
- **Keywords**: case-insensitive; a plain Go `map[string]Kind` over the
  uppercased spelling replaces SQLite's generated perfect hash
  (`keywordhash.h`). The keyword list and the fallback table are transcribed
  from `parse.y`/`mkkeywordhash.c` and cross-checked by `keyword1.test`.
- Input is not required to be valid UTF-8 (SQLite tolerates arbitrary bytes
  in strings/blobs); the lexer is byte-oriented with UTF-8 decoding only
  where identifiers require it.

### 6. License: MIT, and the cleanest upstream situation of the family

- SQLite's source, grammar, and tests are **public domain** (the blessing +
  `LICENSE.md` affidavits). There is no upstream license obligation at all —
  unlike zetajones (Apache-2.0 upstream, so it adopted Apache-2.0) and unlike
  teesql (ScriptDom corpus, an unresolved attribution wrinkle).
- meyer therefore uses **MIT** (consistent with sqlc itself, teesql, and
  doubleclick), copyright the sqlc authors.
- `parser/testdata/README.md` records provenance: extracted from the SQLite
  source tree at pinned version X (public domain, blessing reproduced),
  transformation performed by `cmd/regenerate`. sqllogictest inputs, if ever
  vendored, are likewise unencumbered ("no attribution required").
- `internal/reference/parse.y` (vendored copy of the grammar for
  documentation) keeps its original blessing header.

## AST shape (summary)

Package `ast`, one file per statement family, SQLite-native naming (not
PostgreSQL-shaped, not sqlc-shaped — sqlc's `convert.go` owns the mapping):

- Interfaces: `Node` (`Pos`, `End`, `Children`), `Stmt`, `Expr` with
  unexported marker methods.
- Statements (one node each, mirroring https://sqlite.org/lang.html):
  `SelectStmt` (compound ops, VALUES, WITH, windows), `InsertStmt` (+
  `Upsert`, `Returning`), `UpdateStmt` (+ FROM, indexed-by, or-conflict),
  `DeleteStmt`, `CreateTableStmt` (+ `ColumnDef`, `TypeName`, column/table
  constraints, generated columns, `WITHOUT ROWID`/`STRICT` options),
  `CreateIndexStmt`, `CreateViewStmt`, `CreateTriggerStmt` (trigger body =
  `[]TriggerCmd` restricted statement forms), `CreateVirtualTableStmt`
  (module args kept as raw token spans, as SQLite does), `AlterTableStmt`
  (variant per action), `Drop{Table,View,Index,Trigger}Stmt`, `BeginStmt`,
  `CommitStmt`, `RollbackStmt`, `SavepointStmt`, `ReleaseStmt`,
  `AttachStmt`, `DetachStmt`, `PragmaStmt`, `VacuumStmt`, `AnalyzeStmt`,
  `ReindexStmt`, `ExplainStmt{Query bool, Stmt Stmt}`.
- Expressions: `Ident` (with quote style), `QualifiedRef` (a / a.b / a.b.c),
  `Literal` (int/float/string/blob/null/true/false with raw source),
  `BindParam`, `UnaryExpr`, `BinaryExpr`, `LikeExpr` (op + ESCAPE),
  `IsExpr` (`IS [NOT] [DISTINCT FROM]`), `NullCheckExpr`
  (ISNULL/NOTNULL/NOT NULL), `BetweenExpr`, `InExpr` (list / subquery /
  table-or-function RHS), `CaseExpr`, `CastExpr`, `CollateExpr`,
  `FuncCall` (DISTINCT, inner ORDER BY, `FILTER`, `OVER`, star-arg,
  `WITHIN GROUP`), `ExistsExpr`, `SubqueryExpr`, `VectorExpr`, `RaiseExpr`.
- Clause structs: `FromClause`/`JoinClause` (join type bits as parsed:
  NATURAL/LEFT/RIGHT/FULL/INNER/CROSS/OUTER combinations validated like
  `sqlite3JoinType`), `TableRef` variants (named table [+ `INDEXED BY` /
  `NOT INDEXED`], table-valued function call, parenthesized select,
  parenthesized join list), `OrderingTerm` (ASC/DESC, `NULLS FIRST/LAST`),
  `Limit`, `GroupBy`, `WindowDef`/`FrameSpec`, `With`/`CTE`
  (`[NOT] MATERIALIZED`), `OnConflictTarget`.

Estimated size: SQLite's grammar is much smaller than T-SQL or GoogleSQL —
roughly ~120–150 node types, and a parser in the 12–18k LOC range (vs 25–29k
for the siblings).

## Repository layout

```
meyer/
├── go.mod                    # github.com/sqlc-dev/meyer — no deps
├── LICENSE                   # MIT
├── README.md                 # usage, acknowledgments (SQLite, blessing)
├── PLAN.md                   # this file
├── CLAUDE.md                 # dev-loop guide (next-test → implement → -check-parse)
├── token/
│   ├── token.go              # Kind, Token{Kind, Text, Pos, End}
│   └── keywords.go           # keyword map, fallback set, token classes
├── lexer/
│   └── lexer.go              # port of tokenize.c
├── ast/
│   └── *.go                  # nodes, Span, Children, renderer (String)
├── parser/
│   ├── parser.go             # Parse/ParseStatement entry, cmd dispatch
│   ├── parse_select.go       # select core, compounds, CTEs, windows
│   ├── parse_expr.go         # precedence climbing + special forms
│   ├── parse_dml.go          # insert/update/delete/upsert/returning
│   ├── parse_ddl.go          # create/alter/drop table,index,view,vtab
│   ├── parse_trigger.go      # trigger decl + restricted body
│   ├── parse_misc.go         # txn, pragma, attach, vacuum, analyze, …
│   ├── parser_test.go        # corpus harness (+ -check-parse)
│   ├── fuzz_test.go          # never-panic, fuzzdata seeds
│   ├── bench_test.go
│   └── testdata/
│       ├── README.md         # provenance + pinned SQLite version/hash
│       ├── *.test            # consolidated corpus (== / -- format)
│       └── *.metadata.json   # todo/skip sidecars
├── internal/
│   ├── dump/                 # AST snapshot renderer
│   ├── testfile/             # corpus file format reader/writer
│   └── reference/parse.y     # vendored grammar (documentation only)
└── cmd/
    ├── next-test/            # pick next todo case
    ├── debug-parse/          # parse argv SQL, dump tree or error
    └── regenerate/           # extract TCL corpus + run SQLite oracle
```

## Milestones

1. **Scaffolding** — module, license, CI, token package (full keyword +
   fallback tables), lexer with its own unit tests transcribed from
   `tokenize.test`, AST skeleton, `parser.Parse` returning
   not-implemented errors, corpus format + harness with everything `todo`.
2. **Corpus** — `cmd/regenerate` extraction + oracle. *(Done, but wider than
   planned: rather than vendoring a hand-picked starter set, the tool takes
   every `test/*.test` script in the pinned tree. That costs 4.4 MB and 943
   files, and yields 20,971 cases instead of 4,685, with no curation to redo
   when the pin advances.)*
3. **Expressions + SELECT** — largest single chunk: precedence ladder,
   subqueries, CTEs, compound selects, VALUES, window functions, FROM/joins.
4. **DML** — INSERT (+upsert/returning/default values), UPDATE (+from),
   DELETE; bind parameters end-to-end.
5. **DDL** — CREATE TABLE (constraints, generated columns, options),
   CREATE INDEX/VIEW/TRIGGER/VIRTUAL TABLE, ALTER TABLE (all forms in the
   pinned build), DROP.
6. **Small statements** — transactions/savepoints, PRAGMA (all three value
   forms), ATTACH/DETACH, VACUUM [INTO], ANALYZE, REINDEX,
   EXPLAIN [QUERY PLAN].
7. **Hardening** — error-message/offset fidelity pass over the negative
   corpus, fuzzing with fuzzdata seeds, benchmarks, race-clean parallel
   corpus run, optional sqllogictest smoke gate.
8. **sqlc integration** (in the sqlc repo, separate effort) — new
   `internal/engine/sqlite/parse.go` calling meyer (mirroring the
   zetajones/googlesql pattern), rewritten `convert.go` (meyer AST →
   `sql/ast`), regenerate `reserved.go` from meyer's keyword table, delete
   `internal/engine/sqlite/parser/` (1.2 MB of generated ANTLR code) and the
   `antlr4-go/antlr/v4` dependency; sqlc's existing endtoend tests are the
   acceptance gate.

## Non-goals

- Not a formatter/pretty-printer (the test renderer promises only
  re-parseability).
- No semantic analysis: no name resolution, no type/affinity checking, no
  schema awareness. Errors SQLite raises post-parse are out of scope.
- No error recovery / multi-error reporting in v1 (matches SQLite and all
  three siblings).
- No support for SQLite-internal syntax (`#NNN` registers, nested-parse
  artifacts).

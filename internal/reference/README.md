# Vendored SQLite sources (documentation only)

These files are the grammar, tokenizer and keyword table meyer is a port of,
kept here so that the `parse.y` rule named in a parser comment can be read
without a SQLite checkout. **No tool processes them**: no parser generator
runs and no build step reads them, and the directory deliberately contains
no Go file, so it is not a package and `./...` does not reach it.

Two tests do read them, to check transcriptions that would otherwise only
ever be verified by hand:

- `token`'s tests rebuild the keyword table and the `%fallback` set from
  `mkkeywordhash.c` and `parse.y` and compare them with meyer's, resolved
  for the pinned build's feature flags.
- `parser`'s tests check that every nonterminal in `parse.y` is named by a
  comment somewhere in the parser, which is how the repository's "every
  nontrivial parse function names its rule" rule is enforced.

Advancing the pin therefore surfaces a grammar or keyword change as a test
failure rather than as a silent divergence.

| file | upstream path |
|---|---|
| `parse.y` | `src/parse.y` |
| `tokenize.c` | `src/tokenize.c` |
| `mkkeywordhash.c` | `tool/mkkeywordhash.c` |

Both come from SQLite 3.53.4, the release pinned in
`cmd/regenerate-parse/main.go` and recorded in
`parser/testdata/README.md`. When the pin advances, replace them.

SQLite's source code is in the **public domain**. The authors' statement,
reproduced from the header of these files:

> The author disclaims copyright to this source code. In place of a legal
> notice, here is a blessing:
>
> May you do good and not evil.
> May you find forgiveness for yourself and forgive others.
> May you share freely, never taking more than you give.

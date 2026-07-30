# Vendored SQLite sources (documentation only)

These files are the grammar and tokenizer meyer is a port of, kept here so
that the `parse.y` rule named in a parser comment can be read without a
SQLite checkout. **Nothing in the repository processes them**: no parser
generator runs, no build step reads them, and no test depends on them. They
are reference material for humans, and the directory deliberately contains
no Go file, so it is not a package and `./...` does not reach it.

| file | upstream path |
|---|---|
| `parse.y` | `src/parse.y` |
| `tokenize.c` | `src/tokenize.c` |

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

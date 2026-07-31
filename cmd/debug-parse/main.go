// Command debug-parse parses SQL and prints the result, for working on the
// parser by hand.
//
// It reads the SQL from the command line, from a file, or from stdin, and
// prints one of: the AST, the token stream, the re-rendered SQL, or the
// error meyer would return.
//
// Usage:
//
//	go run ./cmd/debug-parse 'SELECT 1'
//	go run ./cmd/debug-parse -tokens 'SELECT 1'
//	go run ./cmd/debug-parse -f query.sql
//	echo 'SELECT 1' | go run ./cmd/debug-parse
//
// A parse error is printed with a caret under the offending byte, in the
// same wording SQLite uses.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sqlc-dev/meyer/ast"
	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/lexer"
	"github.com/sqlc-dev/meyer/parser"
)

func main() {
	var (
		file      = flag.String("f", "", "read SQL from this file instead of the arguments")
		tokens    = flag.Bool("tokens", false, "print the token stream instead of the tree")
		render    = flag.Bool("render", false, "print the tree rendered back to SQL")
		positions = flag.Bool("pos", true, "include byte spans in the tree")
	)
	flag.Parse()

	src, err := readSource(*file, flag.Args())
	if err != nil {
		fatal(err)
	}

	if *tokens {
		for _, t := range lexer.Lex(src) {
			fmt.Printf("%5d-%-5d %-10s %q\n", t.Pos, t.End, t.Kind, t.Text(src))
		}
		return
	}

	stmts, err := parser.ParseString(src)
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) {
			fmt.Fprint(os.Stderr, describe(src, pe))
			os.Exit(1)
		}
		fatal(err)
	}
	if *render {
		fmt.Print(ast.Statements(stmts))
		return
	}
	fmt.Print(dump.Stmts(stmts, dump.Options{Positions: *positions, Raw: true}))
}

func readSource(file string, args []string) (string, error) {
	switch {
	case file != "":
		b, err := os.ReadFile(file)
		return string(b), err
	case len(args) > 0:
		return strings.Join(args, " "), nil
	default:
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
}

// describe renders an error the way a compiler would: the message with its
// position, then the offending line with a caret under the byte SQLite would
// point at.
func describe(src string, pe *parser.Error) string {
	var b strings.Builder
	fmt.Fprintln(&b, pe)
	if pe.Offset < 0 || pe.Offset > len(src) {
		return b.String()
	}
	_, col := parser.LineCol(src, pe.Offset)
	fmt.Fprintf(&b, "  %s\n  %s^\n", lineAt(src, pe.Offset), strings.Repeat(" ", col-1))
	return b.String()
}

// lineAt returns the text of the line containing offset.
func lineAt(src string, offset int) string {
	start := strings.LastIndexByte(src[:offset], '\n') + 1
	end := strings.IndexByte(src[start:], '\n')
	if end < 0 {
		return src[start:]
	}
	return src[start : start+end]
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "debug-parse:", err)
	os.Exit(1)
}

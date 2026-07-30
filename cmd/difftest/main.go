// Command difftest looks for places where meyer and SQLite disagree.
//
// The vendored corpus is what SQLite's own test suite happens to contain,
// which is overwhelmingly valid SQL: of 20,971 cases fewer than three
// hundred are rejections. Error fidelity — the exact message, at the exact
// byte — is therefore barely exercised by it. difftest fills that gap by
// mutating corpus SQL into inputs nobody wrote on purpose, and checking that
// meyer and the pinned SQLite build still reach the same verdict.
//
// For every mutation both parsers are asked, and the two must agree on:
//
//   - accept versus reject, using the same derivation the test harness uses
//     (a syntax-family message means reject; a semantic one means the
//     statement parsed);
//   - the exact message text on rejection;
//   - the byte offset, wherever sqlite3_error_offset reported one.
//
// A disagreement is a bug in meyer, and the tool prints a reproducer.
//
// Usage:
//
//	go run ./cmd/difftest                        # sweep the whole corpus
//	go run ./cmd/difftest -files select1,expr    # a few files
//	go run ./cmd/difftest -per 200 -seed 7       # dig harder, reproducibly
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sqlc-dev/meyer/internal/dump"
	"github.com/sqlc-dev/meyer/internal/roundtrip"
	"github.com/sqlc-dev/meyer/internal/sqlitesrc"
	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/lexer"
	"github.com/sqlc-dev/meyer/parser"
)

// injections are the tokens a mutation may splice in. They are chosen to
// land on grammar decision points rather than to be plausible: a stray
// keyword where a name belongs is exactly where %fallback, the join
// keywords and the window-function lookahead have to be right.
var injections = []string{
	"(", ")", ",", ".", ";", "*", "+", "-", "||", "->", "=", "<", "?",
	"SELECT", "FROM", "WHERE", "AS", "ON", "USING", "JOIN", "LEFT", "NATURAL",
	"GROUP", "ORDER", "BY", "LIMIT", "OFFSET", "HAVING", "WINDOW", "OVER",
	"FILTER", "PARTITION", "RANGE", "ROWS", "PRECEDING", "EXCLUDE",
	"NOT", "NULL", "IS", "IN", "LIKE", "ESCAPE", "BETWEEN", "AND", "OR",
	"CASE", "WHEN", "THEN", "ELSE", "END", "COLLATE", "DISTINCT", "ALL",
	"VALUES", "SET", "INTO", "DEFAULT", "CONSTRAINT", "PRIMARY", "KEY",
	"REFERENCES", "GENERATED", "ALWAYS", "INDEXED", "RETURNING", "DO",
	"CONFLICT", "MATERIALIZED", "RECURSIVE", "WITH", "UNION", "EXCEPT",
	"abort", "rows", "x", "1", "1.5", "'s'", `"q"`, "x'ab'", "?1", ":v",
}

func main() {
	var (
		cacheDir = flag.String("cache", ".sqlite", "cache directory for SQLite artifacts")
		testdata = flag.String("testdata", "parser/testdata", "corpus directory")
		files    = flag.String("files", "", "comma-separated corpus file base names (default: all)")
		per      = flag.Int("per", 24, "mutations per corpus case")
		depth    = flag.Int("depth", 1, "mutation rounds; 2 applies a second edit to each result")
		seed     = flag.Int64("seed", 1, "PRNG seed")
		jobs     = flag.Int("j", runtime.NumCPU(), "oracle worker processes")
		examples = flag.Int("examples", 3, "reproducers to print per kind of disagreement")
		maxLen   = flag.Int("maxlen", 4096, "skip corpus cases longer than this")
		fuzzdata = flag.Bool("fuzzdata", true, "also use SQLite's test/fuzzdata*.db inputs")
	)
	flag.Parse()

	inputs, err := loadCorpus(*testdata, *files)
	if err != nil {
		fatal(err)
	}
	// The hand-written snapshot inputs are a dense tour of the node set, so
	// mutating them reaches constructs the corpus barely contains.
	if *files == "" {
		extra, err := loadSnapshotInputs(*testdata)
		if err != nil {
			fatal(err)
		}
		inputs = append(inputs, extra...)
	}

	// Build the oracle once, before the workers race for it.
	if _, err := sqlitesrc.Oracle(*cacheDir); err != nil {
		fatal(err)
	}
	if *fuzzdata && *files == "" {
		seeds, err := sqlitesrc.FuzzSeeds(*cacheDir)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%d inputs from SQLite's fuzzdata databases\n", len(seeds))
		inputs = append(inputs, seeds...)
	}
	fmt.Printf("checking %d inputs, %d mutations each\n", len(inputs), *per)

	rep := &report{examples: *examples, buckets: map[string]*bucket{}}
	work := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < *jobs; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r, err := sqlitesrc.NewRunner(*cacheDir)
			if err != nil {
				fatal(err)
			}
			defer r.Close()
			rng := rand.New(rand.NewSource(*seed + int64(w)*1e6))
			for src := range work {
				// The input itself matters, not just its mutations: the
				// fuzzdata seeds are already adversarial.
				check(r, src, rep)
				for _, m := range mutations(src, *per, rng) {
					check(r, m, rep)
					// Further edits reach shapes one cannot: two unbalanced
					// brackets, a keyword in a position only another edit
					// could open up. Each round builds on the last.
					cur := m
					for d := 1; d < *depth; d++ {
						next := mutations(cur, 2, rng)
						if len(next) == 0 {
							break
						}
						for _, m2 := range next {
							check(r, m2, rep)
						}
						cur = next[0]
					}
				}
			}
		}(w)
	}
	for _, src := range inputs {
		if len(src) > *maxLen {
			continue
		}
		work <- src
	}
	close(work)
	wg.Wait()

	rep.print()
	if rep.disagreements.Load() > 0 {
		os.Exit(1)
	}
}

// check asks both parsers about one input and records any disagreement.
func check(r *sqlitesrc.Runner, sql string, rep *report) {
	if sqlitesrc.SplitDiffers(sql) {
		rep.skipped.Add(1)
		return
	}
	results, err := r.Run(sql)
	if err != nil {
		fatal(err)
	}
	rep.checked.Add(1)
	c := testfile.Case{SQL: sql, Results: results}
	exp := c.Expected()

	if exp.OK && exp.Truncated {
		rep.skipped.Add(1)
		return
	}

	stmts, perr := parser.ParseString(sql)
	var pe *parser.Error
	if perr != nil {
		var ok bool
		if pe, ok = perr.(*parser.Error); !ok {
			rep.add("rejection is not a *parser.Error", sql, perr.Error(), "")
			return
		}
	}

	// A grammar action can abandon a parse part-way through a statement;
	// nothing verified the text after that point, whichever way the
	// expectation went.
	if pe != nil && exp.IsUnreached(pe.Offset) {
		rep.skipped.Add(1)
		return
	}
	switch {
	case exp.OK && pe == nil:
		// Both accepted, so this is also a free renderer test: mutated SQL
		// that still parses is a shape nobody wrote on purpose.
		if r := roundtrip.Check(stmts); !r.Ok {
			rep.add(r.Reason, sql, r.Rendered, dump.FirstDiff(r.Want, r.Got))
		}
	case exp.OK:
		rep.add("meyer rejects, SQLite accepts", sql, pe.Message, "accepted")
	case pe == nil:
		rep.add("meyer accepts, SQLite rejects", sql, "accepted", exp.Message)
	case pe.Message != exp.Message:
		rep.add("different message", sql, pe.Message, exp.Message)
	case exp.Offset >= 0 && pe.Offset != exp.Offset:
		rep.add("different offset", sql,
			fmt.Sprintf("%s at %d", pe.Message, pe.Offset),
			fmt.Sprintf("%s at %d", exp.Message, exp.Offset))
	}
}

// mutations derives up to n one-edit variants of src. Every edit works on
// token boundaries, because a byte-level edit mostly produces lexer noise,
// while a token-level one lands on a grammar decision.
func mutations(src string, n int, rng *rand.Rand) []string {
	toks := lexer.Lex(src)
	if len(toks) < 2 {
		return nil
	}
	toks = toks[:len(toks)-1] // drop EOF
	seen := map[string]bool{src: true}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// Bounded by attempts as well as results: with few tokens the edit
	// space is small and most draws repeat one already made.
	for attempts := 0; attempts < 8*n && len(out) < n; attempts++ {
		i := rng.Intn(len(toks))
		t := toks[i]
		switch rng.Intn(5) {
		case 0: // truncate: the commonest way real SQL is broken
			add(src[:t.Pos])
		case 1: // delete a token
			add(src[:t.Pos] + src[t.End:])
		case 2: // duplicate a token
			add(src[:t.End] + " " + t.Text + src[t.End:])
		case 3: // replace a token
			add(src[:t.Pos] + injections[rng.Intn(len(injections))] + src[t.End:])
		case 4: // insert a token
			add(src[:t.Pos] + injections[rng.Intn(len(injections))] + " " + src[t.Pos:])
		}
	}
	return out
}

// loadCorpus returns the SQL of every case in the requested corpus files.
func loadCorpus(dir, files string) ([]string, error) {
	var paths []string
	if files == "" {
		var err error
		paths, err = filepath.Glob(filepath.Join(dir, "*.test"))
		if err != nil {
			return nil, err
		}
	} else {
		for _, name := range strings.Split(files, ",") {
			paths = append(paths, filepath.Join(dir, name+".test"))
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no corpus files under %s", dir)
	}
	var out []string
	for _, path := range paths {
		cases, err := testfile.Read(path)
		if err != nil {
			return nil, err
		}
		for _, c := range cases {
			out = append(out, c.SQL)
		}
	}
	return out, nil
}

// loadSnapshotInputs returns each statement of testdata/ast/*.sql.
func loadSnapshotInputs(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "ast", "*.sql"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		stmts, err := parser.ParseString(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, st := range stmts {
			out = append(out, string(data)[st.Pos():st.End()])
		}
	}
	return out, nil
}

// report groups disagreements so that a hundred instances of one bug read as
// one bug.
type report struct {
	examples      int
	checked       atomic.Int64
	skipped       atomic.Int64
	disagreements atomic.Int64

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	n     int
	shown []string
}

func (r *report) add(kind, sql, got, want string) {
	r.disagreements.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.buckets[kind]
	if b == nil {
		b = &bucket{}
		r.buckets[kind] = b
	}
	b.n++
	if len(b.shown) < r.examples {
		// Quoted, and not trimmed: a reproducer often turns on a byte that
		// prints as nothing, and one that begins or ends in whitespace is
		// the interesting case, not the noisy one.
		b.shown = append(b.shown, fmt.Sprintf("  sql:  %q\n    meyer:  %q\n    sqlite: %q",
			sql, got, want))
	}
}

func (r *report) print() {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds := make([]string, 0, len(r.buckets))
	for k := range r.buckets {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return r.buckets[kinds[i]].n > r.buckets[kinds[j]].n })
	for _, k := range kinds {
		b := r.buckets[k]
		fmt.Printf("\n### %s (%d)\n", k, b.n)
		for _, s := range b.shown {
			fmt.Println(s)
		}
	}
	fmt.Printf("\nchecked %d mutations, %d disagreements (%d skipped as not comparable)\n",
		r.checked.Load(), r.disagreements.Load(), r.skipped.Load())
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "difftest:", err)
	os.Exit(1)
}

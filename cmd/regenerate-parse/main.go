// Command regenerate-parse builds meyer's test corpus from the SQLite
// source tree, using a real SQLite build as the oracle.
//
// Pipeline, per requested test script:
//
//  1. Extract literal SQL cases from test/<name>.test (TCL) with
//     internal/tclextract.
//  2. Run every case through the oracle: a small C program (oracle.c,
//     embedded here) compiled against the pinned SQLite amalgamation. It
//     splits the case into statements exactly like the sqlite3 shell and
//     prepares each one independently against an empty :memory: database —
//     nothing is executed, so statements are order-independent.
//  3. Write parser/testdata/<name>.test with the raw per-statement results,
//     and merge parser/testdata/<name>.metadata.json (new cases start as
//     todo; existing entries are preserved).
//
// The SQLite release is pinned in internal/sqlitesrc, which downloads the
// artifacts into the cache directory (default .sqlite/, gitignored) and
// reuses them on later runs.
//
// Usage:
//
//	go run ./cmd/regenerate-parse [-files select1,expr,...] [flags]
//
// With no -files, every test script in the pinned source tree is
// regenerated. Scripts that yield no literal-SQL cases (the tokenizer
// tests written in C, the VFS and WAL harnesses, and so on) produce no
// corpus file, and a stale one left over from an earlier run is removed.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/sqlc-dev/meyer/internal/sqlitesrc"
	"github.com/sqlc-dev/meyer/internal/tclextract"
	"github.com/sqlc-dev/meyer/internal/testfile"
)

// allScripts lists every test/*.test script of the pinned source tree, in
// sorted order. PLAN.md milestone 2 named a hand-picked starter set; taking
// the whole suite instead costs a few megabytes and quadruples the case
// count, and needs no curation when the pin advances.
func allScripts(testDir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(testDir, "*.test"))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(paths))
	for i, path := range paths {
		names[i] = strings.TrimSuffix(filepath.Base(path), ".test")
	}
	sort.Strings(names)
	return names, nil
}

func main() {
	var (
		files    = flag.String("files", "", "comma-separated test script base names (default: the starter set)")
		cacheDir = flag.String("cache", ".sqlite", "cache directory for SQLite artifacts and the oracle binary")
		testdata = flag.String("testdata", "parser/testdata", "output corpus directory")
		jobs     = flag.Int("j", runtime.NumCPU(), "oracle worker processes")
	)
	flag.Parse()

	// Workers each own an oracle process; the pinned build is compiled on
	// the first call, before any of them start.
	if _, err := sqlitesrc.Oracle(*cacheDir); err != nil {
		fatal(err)
	}
	oracle := func() (*sqlitesrc.Runner, error) { return sqlitesrc.NewRunner(*cacheDir) }
	testDir, err := sqlitesrc.TestScripts(*cacheDir)
	if err != nil {
		fatal(err)
	}

	var names []string
	if *files != "" {
		names = strings.Split(*files, ",")
	} else if names, err = allScripts(testDir); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*testdata, 0o755); err != nil {
		fatal(err)
	}

	var totalCases, totalTodo, usedScripts, emptyScripts int
	var totals tclextract.Stats
	for _, name := range names {
		// Snapshot the previous corpus state before regenerating, so the
		// metadata merge can tell new cases from pre-existing ones.
		testPath := filepath.Join(*testdata, name+".test")
		prevCases := make(map[string]bool)
		if prev, err := testfile.Read(testPath); err == nil {
			for _, c := range prev {
				prevCases[c.Name] = true
			}
		}
		prevMeta, err := testfile.ReadMetadata(testfile.MetadataPath(testPath))
		if err != nil {
			fatal(fmt.Errorf("%s: %w", name, err))
		}

		cases, stats, err := regenerateFile(oracle, testDir, *testdata, name, *jobs)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", name, err))
		}
		totals.Found += stats.Found
		totals.SkippedSubst += stats.SkippedSubst
		totals.SkippedForm += stats.SkippedForm
		if len(cases) == 0 {
			// Nothing extractable: leave no corpus file behind, and clear
			// away one from an earlier run of a differently pinned tree.
			os.Remove(testPath)
			os.Remove(testfile.MetadataPath(testPath))
			emptyScripts++
			continue
		}
		todo, err := mergeMetadata(testPath, prevCases, prevMeta.Todo, cases)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", name, err))
		}
		fmt.Printf("%-14s %4d cases (%d todo) — skipped %d substitution, %d form\n",
			name, len(cases), todo, stats.SkippedSubst, stats.SkippedForm)
		totalCases += len(cases)
		totalTodo += todo
		usedScripts++
	}
	fmt.Printf("\ntotal: %d cases (%d todo) from %d of %d scripts (%d yielded nothing); "+
		"%d of %d found blocks skipped (%d substitution, %d malformed/non-literal)\n",
		totalCases, totalTodo, usedScripts, len(names), emptyScripts,
		totals.SkippedSubst+totals.SkippedForm, totals.Found,
		totals.SkippedSubst, totals.SkippedForm)
}

// newRunner starts an oracle process; regenerateFile calls it once per
// worker.
type newRunner func() (*sqlitesrc.Runner, error)

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "regenerate-parse:", err)
	os.Exit(1)
}

func regenerateFile(oracle newRunner, testDir, outDir, name string, jobs int) ([]testfile.Case, tclextract.Stats, error) {
	src, err := os.ReadFile(filepath.Join(testDir, name+".test"))
	if err != nil {
		return nil, tclextract.Stats{}, err
	}
	extracted, stats := tclextract.File(name, src)

	cases := make([]testfile.Case, len(extracted))
	work := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := oracle()
			if err != nil {
				fail(err)
				for range work { // drain so producers are not blocked
				}
				return
			}
			defer r.Close()
			for i := range work {
				results, err := r.Run(extracted[i].SQL)
				if err != nil {
					fail(fmt.Errorf("case %s: %w", extracted[i].Name, err))
					continue
				}
				cases[i] = testfile.Case{
					Name: extracted[i].Name, SQL: extracted[i].SQL, Results: results,
				}
			}
		}()
	}
	for i := range extracted {
		work <- i
	}
	close(work)
	wg.Wait()
	if firstErr != nil {
		return nil, stats, firstErr
	}

	// Drop cases the file format cannot represent (marker collisions);
	// count them as form skips so the loss is visible.
	kept := cases[:0]
	for _, c := range cases {
		if err := testfile.CheckCase(c); err != nil {
			stats.SkippedForm++
			stats.Extracted--
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == 0 {
		return nil, stats, nil
	}
	if err := testfile.Write(filepath.Join(outDir, name+".test"), kept); err != nil {
		return nil, stats, err
	}
	return kept, stats, nil
}

// mergeMetadata reconciles the sidecar with the regenerated case list:
// genuinely new cases start as todo, cases already in the previous corpus
// keep their previous state (todo or passing), and entries for removed cases
// are dropped. Returns the resulting todo count.
func mergeMetadata(testPath string, prevCases, prevMeta map[string]bool, cases []testfile.Case) (int, error) {
	todo := make(map[string]bool)
	for _, c := range cases {
		if prevMeta[c.Name] || !prevCases[c.Name] {
			todo[c.Name] = true
		}
	}
	metaPath := testfile.MetadataPath(testPath)
	if err := testfile.WriteMetadata(metaPath, testfile.Metadata{Todo: todo}); err != nil {
		return 0, err
	}
	return len(todo), nil
}

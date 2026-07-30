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
// The SQLite release is pinned by version and SHA-256 below. Artifacts are
// downloaded from sqlite.org into the cache directory (default .sqlite/,
// gitignored) and reused on later runs.
//
// Usage:
//
//	go run ./cmd/regenerate-parse [-files select1,expr,...] [flags]
//
// With no -files, the starter set below is regenerated.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	_ "embed"

	"github.com/sqlc-dev/meyer/internal/tclextract"
	"github.com/sqlc-dev/meyer/internal/testfile"
)

//go:embed oracle/oracle.c
var oracleSource []byte

// The pinned SQLite release. When advancing the pin, update all four
// constants and parser/testdata/README.md, rerun with no -files, and review
// the corpus diff.
const (
	sqliteVersion   = "3530400" // 3.53.4
	sqliteYear      = "2026"
	amalgamationSHA = "1e71ddf93849c6a6ecf58b827c0692073d2dd7ee40196158068f7b29f422e87d"
	srcSHA          = "d18fa15aec74d8c17e1463f861095adc01b5ad190256acb4f91d22f0368d232b"
)

// starterSet is the corpus from PLAN.md milestone 2: the dedicated
// parser/tokenizer tests, the grammar-heavy statement suites, and the
// evidence files that map 1:1 to the documented grammar.
var starterSet = []string{
	"parser1", "tokenize", "keyword1",
	"select1", "select2", "select3", "select4", "select5", "select6",
	"select7", "select8", "select9", "selectA", "selectB", "selectC",
	"selectD", "selectE", "selectF", "selectG", "selectH",
	"expr", "expr2", "in", "in2", "in3", "in4", "in5", "between",
	"with1", "with2", "with3", "withM",
	"window1", "window2", "window3", "window4", "window5",
	"window6", "window7", "window8", "window9", "windowA",
	"windowB", "windowC", "windowD", "windowE", "windowerr",
	"alter", "alter2", "alter3", "alter4", "altertab", "altertab2", "altertab3",
	"alterdropcol", "alterdropcol2",
	"trigger1", "upsert1", "upsert2", "upsert3", "upsert4", "upsert5",
	"returning1",
	"e_select", "e_expr", "e_createtable", "e_insert", "e_update", "e_delete",
}

func main() {
	var (
		files    = flag.String("files", "", "comma-separated test script base names (default: the starter set)")
		cacheDir = flag.String("cache", ".sqlite", "cache directory for SQLite artifacts and the oracle binary")
		testdata = flag.String("testdata", "parser/testdata", "output corpus directory")
		jobs     = flag.Int("j", runtime.NumCPU(), "oracle worker processes")
	)
	flag.Parse()

	names := starterSet
	if *files != "" {
		names = strings.Split(*files, ",")
	}

	oracle, err := ensureOracle(*cacheDir)
	if err != nil {
		fatal(err)
	}
	testDir, err := ensureSourceTests(*cacheDir)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*testdata, 0o755); err != nil {
		fatal(err)
	}

	var totalCases, totalTodo int
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
		todo, err := mergeMetadata(testPath, prevCases, prevMeta.Todo, cases)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", name, err))
		}
		fmt.Printf("%-14s %4d cases (%d todo) — skipped %d substitution, %d form\n",
			name, len(cases), todo, stats.SkippedSubst, stats.SkippedForm)
		totalCases += len(cases)
		totalTodo += todo
		totals.Found += stats.Found
		totals.SkippedSubst += stats.SkippedSubst
		totals.SkippedForm += stats.SkippedForm
	}
	fmt.Printf("\ntotal: %d cases (%d todo) from %d files; %d of %d found blocks skipped (%d substitution, %d malformed/non-literal)\n",
		totalCases, totalTodo, len(names),
		totals.SkippedSubst+totals.SkippedForm, totals.Found,
		totals.SkippedSubst, totals.SkippedForm)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "regenerate-parse:", err)
	os.Exit(1)
}

func regenerateFile(oracle, testDir, outDir, name string, jobs int) ([]testfile.Case, tclextract.Stats, error) {
	src, err := os.ReadFile(filepath.Join(testDir, name+".test"))
	if err != nil {
		return nil, tclextract.Stats{}, err
	}
	extracted, stats := tclextract.File(name, src)

	cases := make([]testfile.Case, len(extracted))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i, ex := range extracted {
		wg.Add(1)
		go func(i int, ex tclextract.Case) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results, err := runOracle(oracle, ex.SQL)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("case %s: %w", ex.Name, err)
				}
				mu.Unlock()
				return
			}
			cases[i] = testfile.Case{Name: ex.Name, SQL: ex.SQL, Results: results}
		}(i, ex)
	}
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
	if err := testfile.Write(filepath.Join(outDir, name+".test"), kept); err != nil {
		return nil, stats, err
	}
	return kept, stats, nil
}

// runOracle feeds one case's SQL to the oracle binary and parses its output.
func runOracle(oracle, sql string) ([]testfile.StmtResult, error) {
	cmd := exec.Command(oracle)
	cmd.Stdin = strings.NewReader(sql)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("oracle: %w: %s", err, errb.String())
	}
	var results []testfile.StmtResult
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		r, err := parseOracleLine(line)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

func parseOracleLine(line string) (testfile.StmtResult, error) {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) != 3 || fields[0] != "stmt" {
		return testfile.StmtResult{}, fmt.Errorf("malformed oracle line %q", line)
	}
	off, err := strconv.Atoi(fields[1])
	if err != nil {
		return testfile.StmtResult{}, fmt.Errorf("malformed oracle line %q", line)
	}
	if fields[2] == "ok" {
		return testfile.StmtResult{Offset: off, OK: true}, nil
	}
	rest := strings.SplitN(strings.TrimPrefix(fields[2], "err "), " ", 3)
	if !strings.HasPrefix(fields[2], "err ") || len(rest) != 3 {
		return testfile.StmtResult{}, fmt.Errorf("malformed oracle line %q", line)
	}
	rc, err1 := strconv.Atoi(rest[0])
	erroff, err2 := strconv.Atoi(rest[1])
	if err1 != nil || err2 != nil {
		return testfile.StmtResult{}, fmt.Errorf("malformed oracle line %q", line)
	}
	return testfile.StmtResult{Offset: off, RC: rc, ErrOffset: erroff, Message: rest[2]}, nil
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

// ensureOracle downloads the pinned amalgamation and compiles the oracle
// binary into the cache directory, reusing existing artifacts.
func ensureOracle(cacheDir string) (string, error) {
	oracle := filepath.Join(cacheDir, "oracle-"+sqliteVersion)
	if _, err := os.Stat(oracle); err == nil {
		return oracle, nil
	}
	amalgDir := filepath.Join(cacheDir, "sqlite-amalgamation-"+sqliteVersion)
	if _, err := os.Stat(filepath.Join(amalgDir, "sqlite3.c")); err != nil {
		zipPath, err := download(cacheDir, "sqlite-amalgamation-"+sqliteVersion+".zip", amalgamationSHA)
		if err != nil {
			return "", err
		}
		if err := unzip(zipPath, cacheDir, nil); err != nil {
			return "", err
		}
	}
	srcPath := filepath.Join(cacheDir, "oracle.c")
	if err := os.WriteFile(srcPath, oracleSource, 0o644); err != nil {
		return "", err
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	fmt.Printf("compiling oracle (%s, SQLite %s)...\n", cc, sqliteVersion)
	cmd := exec.Command(cc, "-O1", "-o", oracle, srcPath,
		filepath.Join(amalgDir, "sqlite3.c"), "-I", amalgDir,
		"-lpthread", "-ldl", "-lm")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compiling oracle: %w\n%s", err, out)
	}
	return oracle, nil
}

// ensureSourceTests downloads the pinned source zip and extracts test/*.test,
// returning the directory holding them.
func ensureSourceTests(cacheDir string) (string, error) {
	testDir := filepath.Join(cacheDir, "sqlite-src-"+sqliteVersion, "test")
	if _, err := os.Stat(filepath.Join(testDir, "select1.test")); err == nil {
		return testDir, nil
	}
	zipPath, err := download(cacheDir, "sqlite-src-"+sqliteVersion+".zip", srcSHA)
	if err != nil {
		return "", err
	}
	only := func(name string) bool {
		return strings.Contains(name, "/test/") && strings.HasSuffix(name, ".test")
	}
	if err := unzip(zipPath, cacheDir, only); err != nil {
		return "", err
	}
	return testDir, nil
}

// download fetches https://sqlite.org/<year>/<name> into cacheDir (reusing an
// existing file) and verifies its SHA-256.
func download(cacheDir, name, wantSHA string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(cacheDir, name)
	if data, err := os.ReadFile(dest); err == nil {
		if fmt.Sprintf("%x", sha256.Sum256(data)) == wantSHA {
			return dest, nil
		}
		fmt.Printf("cached %s has wrong checksum; re-downloading\n", name)
	}
	url := "https://sqlite.org/" + sqliteYear + "/" + name
	fmt.Printf("downloading %s...\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantSHA {
		return "", fmt.Errorf("%s: checksum mismatch: got %s, want %s", name, got, wantSHA)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func unzip(zipPath, destDir string, keep func(string) bool) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || (keep != nil && !keep(f.Name)) {
			continue
		}
		clean := filepath.Clean(f.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe path in zip: %q", f.Name)
		}
		dest := filepath.Join(destDir, clean)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

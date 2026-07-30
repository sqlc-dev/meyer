// Package sqlitesrc manages the pinned SQLite release that meyer is tested
// against. It downloads the official artifacts, verifies their checksums,
// compiles the oracle program, and unpacks the test scripts.
//
// The pin lives here so that every command shares one version, and the
// artifacts live in a cache directory (.sqlite/ by default, gitignored) that
// is reused across runs.
package sqlitesrc

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "embed"

	"github.com/sqlc-dev/meyer/internal/testfile"
	"github.com/sqlc-dev/meyer/lexer"
	"github.com/sqlc-dev/meyer/token"
)

//go:embed oracle/oracle.c
var oracleSource []byte

// The pinned SQLite release. When advancing the pin, update all four
// constants and parser/testdata/README.md, regenerate the corpus, and
// review the diff.
const (
	Version         = "3530400" // 3.53.4
	year            = "2026"
	amalgamationSHA = "1e71ddf93849c6a6ecf58b827c0692073d2dd7ee40196158068f7b29f422e87d"
	srcSHA          = "d18fa15aec74d8c17e1463f861095adc01b5ad190256acb4f91d22f0368d232b"
)

// Oracle returns the path to the compiled oracle binary, downloading the
// pinned amalgamation and building it on first use.
func Oracle(cacheDir string) (string, error) {
	oracle := filepath.Join(cacheDir, "oracle-"+Version)
	if _, err := os.Stat(oracle); err == nil {
		return oracle, nil
	}
	amalgDir := filepath.Join(cacheDir, "sqlite-amalgamation-"+Version)
	if _, err := os.Stat(filepath.Join(amalgDir, "sqlite3.c")); err != nil {
		zipPath, err := download(cacheDir, "sqlite-amalgamation-"+Version+".zip", amalgamationSHA)
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
	fmt.Printf("compiling oracle (%s, SQLite %s)...\n", cc, Version)
	cmd := exec.Command(cc, "-O1", "-o", oracle, srcPath,
		filepath.Join(amalgDir, "sqlite3.c"), "-I", amalgDir,
		"-lpthread", "-ldl", "-lm")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("compiling oracle: %w\n%s", err, out)
	}
	return oracle, nil
}

// TestScripts returns the directory holding the pinned release's
// test/*.test scripts, downloading and unpacking them on first use.
func TestScripts(cacheDir string) (string, error) {
	testDir := filepath.Join(cacheDir, "sqlite-src-"+Version, "test")
	if _, err := os.Stat(filepath.Join(testDir, "select1.test")); err == nil {
		return testDir, nil
	}
	zipPath, err := download(cacheDir, "sqlite-src-"+Version+".zip", srcSHA)
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
	url := "https://sqlite.org/" + year + "/" + name
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

// Runner drives one long-lived oracle process in batch mode. Starting a
// process per script costs more than classifying one, which matters when a
// caller checks tens of thousands of small inputs; a Runner amortises it
// away. Runners are not safe for concurrent use — start one per worker.
type Runner struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// NewRunner starts an oracle process, building it first if necessary.
func NewRunner(cacheDir string) (*Runner, error) {
	oracle, err := Oracle(cacheDir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(oracle, "-batch")
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Runner{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<16)}, nil
}

// Run classifies every statement of one script.
func (r *Runner) Run(sql string) ([]testfile.StmtResult, error) {
	if _, err := fmt.Fprintf(r.in, "%d\n%s", len(sql), sql); err != nil {
		return nil, fmt.Errorf("oracle: %w", err)
	}
	var results []testfile.StmtResult
	for {
		line, err := r.out.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("oracle: %w", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "==" {
			return results, nil
		}
		// An error message can itself contain a newline, because the
		// offending token can: "SELECT X'0102, 1" reports the rest of the
		// input, newline included, as an unrecognized token. Such a line is
		// a continuation of the message before it.
		if !strings.HasPrefix(line, "stmt ") && len(results) > 0 {
			results[len(results)-1].Message += "\n" + line
			continue
		}
		res, err := testfile.ParseResultLine(line)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
}

// Close shuts the oracle process down.
func (r *Runner) Close() error {
	r.in.Close()
	return r.cmd.Wait()
}

// SplitDiffers reports whether Run would cut a script into statements
// somewhere the tokenizer would not, which makes its verdict incomparable
// with a parser's.
//
// The oracle splits the way the sqlite3 shell does, with sqlite3_complete,
// whose scanner is much cruder than the tokenizer: it understands strings,
// the four quoting styles and comments, but nothing else. So a ";" ends a
// statement for it even inside brackets, and even inside a token the real
// tokenizer would have swallowed whole -- ":v(a;b)" is one bind parameter
// to the tokenizer and two statements to sqlite3_complete. Any caller of
// Run inherits this, which is why it lives here rather than in one of them.
func SplitDiffers(sql string) bool {
	depth := 0
	for _, t := range lexer.Lex(sql) {
		switch t.Kind {
		case token.LP:
			depth++
		case token.RP:
			if depth > 0 {
				depth--
			}
		case token.SEMI:
			if depth > 0 {
				return true
			}
		default:
			if strings.Contains(t.Text, ";") && !completeUnderstands(t) {
				return true
			}
		}
	}
	return false
}

// completeUnderstands reports whether sqlite3_complete skips over a token
// the same way the tokenizer does. The closed quoted forms it does, blob
// literals included, since it reads their body as a single-quoted string.
// An unterminated one it does not: in ":v('(%d)',changes());" the tokenizer
// swallows the first quote into the bind parameter and finds a string
// starting at the second, while sqlite3_complete pairs the two.
func completeUnderstands(t token.Token) bool {
	switch t.Kind {
	case token.STRING, token.BLOB:
		return true
	case token.ID:
		switch t.Text[0] {
		case '"', '`', '[':
			return true
		}
	}
	return false
}

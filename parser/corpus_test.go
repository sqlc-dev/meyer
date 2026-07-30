package parser_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlc-dev/meyer/internal/testfile"
)

// forEachCorpusCase runs fn over every case of every corpus file, one
// parallel subtest per file, skipping cases the metadata still lists as
// todo. Four tests walk the corpus this way and only differ in fn.
func forEachCorpusCase(t *testing.T, fn func(*testing.T, testfile.Case)) {
	t.Helper()
	for _, path := range corpusFiles(t) {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".test"), func(t *testing.T) {
			t.Parallel()
			cases, err := testfile.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := testfile.ReadMetadata(testfile.MetadataPath(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range cases {
				if !meta.Todo[c.Name] {
					fn(t, c)
				}
			}
		})
	}
}

// corpusFiles lists the corpus, failing the test if it is missing rather
// than passing vacuously.
func corpusFiles(t testing.TB) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no corpus files; run go run ./cmd/regenerate-parse")
	}
	return paths
}

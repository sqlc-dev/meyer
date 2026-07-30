// Command next-test picks the next corpus case to implement.
//
// It scans parser/testdata/*.metadata.json for todo cases, chooses the file
// with the fewest remaining todos (ties broken by smaller corpus file, so
// nearly-done and simple files finish first), and prints that file's first
// todo case: its SQL and the expectation derived from the oracle results.
//
// Usage:
//
//	go run ./cmd/next-test [-testdata parser/testdata]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sqlc-dev/meyer/internal/testfile"
)

func main() {
	testdata := flag.String("testdata", "parser/testdata", "corpus directory")
	flag.Parse()

	paths, err := filepath.Glob(filepath.Join(*testdata, "*.test"))
	if err != nil || len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "next-test: no corpus files under %s\n", *testdata)
		os.Exit(1)
	}
	sort.Strings(paths)

	type fileState struct {
		path  string
		size  int64
		todos int
		total int
	}
	var files []fileState
	var totalTodo, totalCases int
	for _, path := range paths {
		meta, err := testfile.ReadMetadata(testfile.MetadataPath(path))
		if err != nil {
			fatal(err)
		}
		cases, err := testfile.Read(path)
		if err != nil {
			fatal(err)
		}
		st, _ := os.Stat(path)
		files = append(files, fileState{path: path, size: st.Size(), todos: len(meta.Todo), total: len(cases)})
		totalTodo += len(meta.Todo)
		totalCases += len(cases)
	}

	fmt.Printf("progress: %d/%d cases passing (%d todo across %d files)\n\n",
		totalCases-totalTodo, totalCases, totalTodo, len(files))
	if totalTodo == 0 {
		fmt.Println("No todo cases found!")
		return
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].todos != files[j].todos {
			return files[i].todos < files[j].todos
		}
		return files[i].size < files[j].size
	})
	var pick fileState
	for _, f := range files {
		if f.todos > 0 {
			pick = f
			break
		}
	}

	meta, err := testfile.ReadMetadata(testfile.MetadataPath(pick.path))
	if err != nil {
		fatal(err)
	}
	cases, err := testfile.Read(pick.path)
	if err != nil {
		fatal(err)
	}
	for _, c := range cases {
		if !meta.Todo[c.Name] {
			continue
		}
		exp := c.Expected()
		fmt.Printf("file: %s (%d todo of %d cases)\ncase: %s\n\nsql:\n%s\n", pick.path, pick.todos, pick.total, c.Name, c.SQL)
		if exp.OK {
			fmt.Println("expect: parse success")
		} else {
			fmt.Printf("expect: parse error %q", exp.Message)
			if exp.Offset >= 0 {
				fmt.Printf(" at offset %d", exp.Offset)
			}
			fmt.Println()
		}
		return
	}
	fmt.Fprintf(os.Stderr, "next-test: %s: metadata lists todos not present in corpus; rerun cmd/regenerate-parse\n", pick.path)
	os.Exit(1)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "next-test:", err)
	os.Exit(1)
}

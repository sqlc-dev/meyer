package parser_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	ruleHead     = regexp.MustCompile(`(?m)^([a-z_][a-zA-Z0-9_]*)\s*(\([A-Z]\))?\s*::=`)
	tokenClass   = regexp.MustCompile(`(?m)^%token_class\s+([a-z_][a-zA-Z0-9_]*)`)
	identifierRE = `\b%s\b`
)

// TestEveryRuleIsNamed enforces the repository's rule that every nontrivial
// parse function carries a comment naming the parse.y production it
// implements — from the other direction, which is the one that catches
// something missing: no nonterminal of the grammar may go unmentioned.
//
// A nonterminal named nowhere in parser/ is either unimplemented or
// implemented without saying which rule it came from, and both are worth a
// failure. Advancing the pinned SQLite release surfaces a new production
// here.
func TestEveryRuleIsNamed(t *testing.T) {
	grammar, err := os.ReadFile(filepath.Join("..", "internal", "reference", "parse.y"))
	if err != nil {
		t.Fatalf("%v (the vendored grammar is missing)", err)
	}
	var rules []string
	for _, m := range ruleHead.FindAllStringSubmatch(string(grammar), -1) {
		rules = append(rules, m[1])
	}
	for _, m := range tokenClass.FindAllStringSubmatch(string(grammar), -1) {
		rules = append(rules, m[1])
	}
	if len(rules) < 100 {
		t.Fatalf("only %d rules parsed out of parse.y; the format changed", len(rules))
	}

	src := parserSource(t)
	seen := map[string]bool{}
	for _, rule := range rules {
		if seen[rule] {
			continue
		}
		seen[rule] = true
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(rule) + `\b`).MatchString(src) {
			t.Errorf("parse.y rule %q is named nowhere in parser/", rule)
		}
	}
}

// parserSource concatenates the parser's non-test sources.
func parserSource(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

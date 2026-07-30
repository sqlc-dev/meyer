package token

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The keyword table and the %fallback set are transcriptions, and a
// transcription is only as good as its last proofread. These tests rebuild
// both from the vendored upstream sources in internal/reference and compare
// them with meyer's, so advancing the pinned SQLite release surfaces a
// keyword change as a failure rather than as a silent divergence.
//
// Both have to resolve the build's feature flags the same way the pinned
// oracle does; the flags are listed once here.

// disabledMasks are the mkkeywordhash.c masks that evaluate to 0 in the
// pinned build, so keywords guarded by them are not compiled in.
var disabledMasks = []string{"ORDERSET"} // SQLITE_ENABLE_ORDERED_SET_AGGREGATES

// disabledSections are the %ifdef/%ifndef guards in parse.y's %fallback
// block whose bodies the pinned build leaves out.
var disabledSections = []string{
	"%ifdef SQLITE_OMIT_COMPOUND_SELECT",
	"%ifdef SQLITE_ENABLE_ORDERED_SET_AGGREGATES",
}

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../internal/reference/" + name)
	if err != nil {
		t.Fatalf("%v (the vendored reference sources are missing)", err)
	}
	return string(data)
}

var keywordEntry = regexp.MustCompile(`\{\s*"([A-Z_0-9]+)",\s*"TK_([A-Z_0-9]+)",\s*([A-Z|]+),`)

func TestKeywordTableMatchesUpstream(t *testing.T) {
	src := read(t, "mkkeywordhash.c")
	table := src[strings.Index(src, "static Keyword aKeywordTable[] = {"):]
	table = table[:strings.Index(table, "\n};")]

	want := map[string]string{}
	for _, m := range keywordEntry.FindAllStringSubmatch(table, -1) {
		name, tk, mask := m[1], m[2], m[3]
		if slices.ContainsFunc(strings.Split(mask, "|"), func(p string) bool {
			return slices.Contains(disabledMasks, p)
		}) {
			continue
		}
		want[name] = tk
	}
	if len(want) < 100 {
		t.Fatalf("only %d keywords parsed out of mkkeywordhash.c; the format changed", len(want))
	}
	for name, tk := range want {
		k, ok := keywords[name]
		if !ok {
			t.Errorf("%s is a keyword upstream but not in meyer", name)
			continue
		}
		// Kind.String reports the SQLite token name, minus the TK_ prefix.
		if got := k.String(); got != tk {
			t.Errorf("%s is TK_%s upstream, but meyer gives %s", name, tk, got)
		}
	}
	for name := range keywords {
		if _, ok := want[name]; !ok {
			t.Errorf("%s is a keyword in meyer but not upstream", name)
		}
	}
}

func TestFallbackSetMatchesUpstream(t *testing.T) {
	src := read(t, "parse.y")
	block := src[strings.Index(src, "%fallback ID"):]
	block = block[:strings.Index(block, "\n  .\n")]

	want := map[string]bool{}
	skip := false
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case slices.Contains(disabledSections, line):
			skip = true
		case strings.HasPrefix(line, "%ifdef"), strings.HasPrefix(line, "%ifndef"),
			strings.HasPrefix(line, "%endif"):
			skip = false
		case skip, strings.HasPrefix(line, "%fallback"), strings.HasPrefix(line, "//"):
		default:
			for _, name := range strings.Fields(line) {
				want[name] = true
			}
		}
	}
	if len(want) < 50 {
		t.Fatalf("only %d fallback tokens parsed out of parse.y; the format changed", len(want))
	}
	// The block names token codes, not spellings: TK_LIKE_KW covers LIKE,
	// GLOB and REGEXP.
	got := map[string]bool{}
	for k := range fallbackSet {
		got[k.String()] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s falls back to ID upstream but not in meyer", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s falls back to ID in meyer but not upstream", name)
		}
	}
}

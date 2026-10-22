package app_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAcceptanceCriteria_EveryNamedTestExists keeps docs/spec.md §12 honest:
// each test the checklist cites (`TestName` or the prefix form `TestPrefix_*`)
// must exist somewhere under internal/ or cmd/, and every ticked criterion
// must cite at least one test. A renamed or deleted test fails here instead of
// leaving the checklist pointing at nothing.
func TestAcceptanceCriteria_EveryNamedTestExists(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	spec, err := os.ReadFile(filepath.Join(root, "docs", "spec.md"))
	require.NoError(t, err)
	section := string(spec)
	start := strings.Index(section, "## 12. Kriteria Selesai")
	end := strings.Index(section, "## 13.")
	require.True(t, start >= 0 && end > start, "spec §12 not found")
	section = section[start:end]

	testNames := declaredTests(t, root)

	cited := regexp.MustCompile("`(Test[A-Za-z0-9_]*\\*?)`")
	criteria := 0
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "- [x]") && !strings.HasPrefix(line, "- [ ]") {
			continue
		}
		criteria++
		matches := cited.FindAllStringSubmatch(line, -1)
		if strings.HasPrefix(line, "- [x]") {
			require.NotEmpty(t, matches, "ticked criterion cites no test: %.80s", line)
		}
		for _, m := range matches {
			name := m[1]
			if prefix, ok := strings.CutSuffix(name, "*"); ok {
				require.True(t, anyHasPrefix(testNames, prefix), "no test matches %s (cited in: %.60s)", name, line)
				continue
			}
			require.Contains(t, testNames, name, "cited test does not exist (cited in: %.60s)", line)
		}
	}
	require.Greater(t, criteria, 20, "spec §12 parsed to too few criteria")
}

var testFuncPattern = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)

// declaredTests collects the names of all top-level Test functions, with or
// without build tags.
func declaredTests(t *testing.T, root string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path) //nolint:gosec // walking the repository's own tests
			if err != nil {
				return err
			}
			for _, m := range testFuncPattern.FindAllSubmatch(data, -1) {
				names[string(m[1])] = true
			}
			return nil
		})
		require.NoError(t, err)
	}
	return names
}

func anyHasPrefix(names map[string]bool, prefix string) bool {
	for name := range names {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

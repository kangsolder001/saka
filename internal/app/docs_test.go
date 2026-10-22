package app_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

// markdownFiles are the documents whose links and citations are checked.
func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{filepath.Join(root, "README.md"), filepath.Join(root, "CLAUDE.md")}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	require.NoError(t, err)
	return append(files, docs...)
}

// TestDocs_RelativeLinksPointAtFilesThatExist keeps the README and docs/ from
// linking to documents that were renamed or never written.
func TestDocs_RelativeLinksPointAtFilesThatExist(t *testing.T) {
	root := repoRoot(t)
	link := regexp.MustCompile(`\]\(([^)\s]+)\)`)
	checked := 0
	for _, file := range markdownFiles(t, root) {
		data, err := os.ReadFile(file) //nolint:gosec // the repository's own documents
		require.NoError(t, err)
		for _, m := range link.FindAllStringSubmatch(string(data), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			resolved := filepath.Join(filepath.Dir(file), target)
			_, err := os.Stat(resolved)
			assert.NoError(t, err, "%s links to %s, which does not exist", filepath.Base(file), m[1])
			checked++
		}
	}
	assert.Positive(t, checked)
}

// TestDocs_IdentifiersCitedInTheModuleGuideExist keeps docs/panduan-modul-baru.md
// honest: every `package.Identifier` it cites from the platform and module
// packages must be declared somewhere in the code.
func TestDocs_IdentifiersCitedInTheModuleGuideExist(t *testing.T) {
	root := repoRoot(t)
	guide, err := os.ReadFile(filepath.Join(root, "docs", "panduan-modul-baru.md")) //nolint:gosec // the repository's own document
	require.NoError(t, err)

	cited := regexp.MustCompile("`?\\b(org|reqctx|entitlement|entitlements|db|pagination|httpx|apperr|mail|redisx|safego|clock|scheduler)\\.([A-Z][A-Za-z0-9]*)").
		FindAllStringSubmatch(string(guide), -1)
	require.Greater(t, len(cited), 15)

	var source strings.Builder
	for _, top := range []string{"internal", "cmd"} {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path) //nolint:gosec // walking the repository's own source
			if err != nil {
				return err
			}
			source.Write(data)
			source.WriteByte('\n')
			return nil
		}))
	}
	code := source.String()

	seen := map[string]bool{}
	for _, m := range cited {
		name := m[2]
		if seen[name] {
			continue
		}
		seen[name] = true
		declared := regexp.MustCompile(`(?m)^(func|type|var|const)\b[^\n]*\b` + name + `\b|^\t+` + name + `\b`)
		assert.True(t, declared.MatchString(code), "docs/panduan-modul-baru.md cites %s.%s, which is not declared in the code", m[1], name)
	}
}

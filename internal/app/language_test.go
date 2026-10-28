package app_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// indonesianWords are common Indonesian words that have no business in an
// English sentence. The list is short on purpose: each word must be unlikely to
// appear in English text, identifiers or SQL, so a hit is almost always real.
var indonesianWords = []string{
	"yang", "tidak", "sudah", "belum", "anda", "untuk", "dengan", "berhasil",
	"silakan", "paket", "langganan", "anggota", "tagihan", "organisasi",
	"atau", "bila", "jika", "harus", "akan", "dari", "kepada", "terima",
	"kasih", "pembayaran", "kata", "sandi", "masuk", "keluar", "undangan",
	"tautan", "bulan", "tahun", "kesalahan", "ditemukan", "terlalu", "coba",
	"lagi", "nanti", "salah", "pengguna", "akun",
}

// languageExemptions lists "<path>:<word>" pairs allowed to contain a word
// above, each with the reason it is not Indonesian text. Keep it empty unless
// a real false positive shows up.
var languageExemptions = map[string]string{}

var indonesianPattern = func() *regexp.Regexp {
	quoted := make([]string, len(indonesianWords))
	for i, w := range indonesianWords {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)
}()

// TestLanguage_CodeTemplatesAndContractAreEnglish keeps everything the backend
// sends to clients and end users in English (spec §2, §14 #8): string literals
// in production Go code (API messages, email data, gateway descriptions),
// email templates, and docs/openapi.yaml. Documents under docs/ stay in
// Indonesian and are not checked.
func TestLanguage_CodeTemplatesAndContractAreEnglish(t *testing.T) {
	root := repoRoot(t)
	var violations []string
	report := func(path string, line int, text string) {
		for _, m := range indonesianPattern.FindAllString(text, -1) {
			word := strings.ToLower(m)
			if _, ok := languageExemptions[path+":"+word]; ok {
				continue
			}
			violations = append(violations, fmt.Sprintf("%s:%d: %q in %q", path, line, word, text))
		}
	}

	// String literals in non-test Go files.
	fset := token.NewFileSet()
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel := relPath(t, root, path)
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					text = lit.Value
				}
				report(rel, fset.Position(lit.Pos()).Line, text)
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}

	// Email templates and the OpenAPI contract, line by line.
	textFiles := []string{filepath.Join(root, "docs", "openapi.yaml")}
	err := filepath.WalkDir(filepath.Join(root, "templates"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasSuffix(path, ".go") {
			textFiles = append(textFiles, path)
		}
		return err
	})
	require.NoError(t, err)
	require.Greater(t, len(textFiles), 1, "no templates found: is the test running from internal/app?")
	for _, path := range textFiles {
		f, err := os.Open(path)
		require.NoError(t, err)
		rel := relPath(t, root, path)
		scanner := bufio.NewScanner(f)
		for line := 1; scanner.Scan(); line++ {
			report(rel, line, scanner.Text())
		}
		require.NoError(t, scanner.Err())
		require.NoError(t, f.Close())
	}

	require.Empty(t, violations, "Indonesian text found where English is required (spec §2); translate it or add an exemption with a reason")
}

func relPath(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	return filepath.ToSlash(rel)
}

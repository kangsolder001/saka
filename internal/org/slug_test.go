package org_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/org"
)

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Acme":                    "acme",
		"  Acme  Corp. ":          "acme-corp",
		"PT Maju Jaya (Persero)":  "pt-maju-jaya-persero",
		"a--b__c":                 "a-b-c",
		"Café Bogor":              "caf-bogor",
		"!!!":                     "org",
		"":                        "org",
		"ab":                      "ab0",
		"日本語":                     "org",
		strings.Repeat("x", 100):  strings.Repeat("x", org.MaxSlugLength-7),
		strings.Repeat("ab-", 30): strings.Repeat("ab-", 13) + "ab",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			got := org.Slugify(in)
			assert.Equal(t, want, got)
			assert.True(t, org.ValidSlug(got), "a derived slug is always valid")
		})
	}
}

func TestValidSlug(t *testing.T) {
	valid := []string{"acme", "a1b", "acme-corp", "a-b-c", strings.Repeat("a", org.MaxSlugLength)}
	invalid := []string{"", "ab", "-acme", "acme-", "ac--me", "Acme", "ac me", "ac_me", "acmé", strings.Repeat("a", org.MaxSlugLength+1)}
	for _, s := range valid {
		assert.True(t, org.ValidSlug(s), s)
	}
	for _, s := range invalid {
		assert.False(t, org.ValidSlug(s), s)
	}
}

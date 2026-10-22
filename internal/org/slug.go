package org

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// Slug limits (the slug is part of URLs, so it stays short and plain).
const (
	MinSlugLength = 3
	MaxSlugLength = 48
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug reports whether s is an acceptable slug: lowercase letters, digits
// and single hyphens between them, MinSlugLength to MaxSlugLength long.
func ValidSlug(s string) bool {
	return len(s) >= MinSlugLength && len(s) <= MaxSlugLength && slugPattern.MatchString(s)
}

// Slugify derives a slug from a display name: ASCII letters and digits are
// kept (lowercased), every other run of characters becomes one hyphen. A name
// with nothing usable (for example only symbols) yields "org".
func Slugify(name string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
			continue
		}
		pendingHyphen = true
	}
	slug := b.String()
	if len(slug) > MaxSlugLength-7 { // leave room for a "-xxxxxx" collision suffix
		slug = strings.TrimRight(slug[:MaxSlugLength-7], "-")
	}
	if len(slug) < MinSlugLength {
		if slug == "" {
			return "org"
		}
		return slug + strings.Repeat("0", MinSlugLength-len(slug))
	}
	return slug
}

// withSuffix appends a short random suffix to a base slug, used when the plain
// slug is taken.
func withSuffix(base string) string {
	var b [3]byte
	_, _ = rand.Read(b[:]) // crypto/rand does not fail on supported platforms
	return base + "-" + hex.EncodeToString(b[:])
}

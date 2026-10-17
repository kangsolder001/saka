// Package pagination implements cursor-based pagination (spec §3):
// ?limit=20&cursor=<opaque>. Repositories fetch limit+1 rows ordered by
// (created_at, id); Paginate trims the extra row and builds next_cursor.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// Cursor is the keyset position of the last item on a page. The ID alone is
// unique, CreatedAt makes ordering by time explicit.
type Cursor struct {
	CreatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
}

// Encode returns the opaque string form sent to clients.
func (c Cursor) Encode() string {
	raw, _ := json.Marshal(c) // fixed struct, cannot fail
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses a client-supplied cursor. Any tampering or garbage is
// reported as a validation error.
func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, errInvalidCursor()
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return Cursor{}, errInvalidCursor()
	}
	return c, nil
}

func errInvalidCursor() error {
	return apperr.Validation(map[string]string{"cursor": "invalid cursor"})
}

// Params are the parsed pagination query parameters.
type Params struct {
	Limit  int
	Cursor *Cursor // nil for the first page
}

// FetchLimit is the number of rows a repository should query: one more than
// the page size, so Paginate can tell whether another page exists.
func (p Params) FetchLimit() int { return p.Limit + 1 }

// ParseQuery reads limit and cursor from the URL query. A missing limit
// defaults to DefaultLimit; values outside 1..MaxLimit are rejected.
func ParseQuery(q url.Values) (Params, error) {
	p := Params{Limit: DefaultLimit}
	fields := map[string]string{}

	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxLimit {
			fields["limit"] = "must be an integer between 1 and " + strconv.Itoa(MaxLimit)
		} else {
			p.Limit = n
		}
	}

	if raw := q.Get("cursor"); raw != "" {
		c, err := DecodeCursor(raw)
		if err != nil {
			fields["cursor"] = "invalid cursor"
		} else {
			p.Cursor = &c
		}
	}

	if len(fields) > 0 {
		return Params{}, apperr.Validation(fields)
	}
	return p, nil
}

// Page is the response envelope for list endpoints.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// Paginate turns the rows fetched with FetchLimit into a Page. cursorOf
// returns the cursor position of an item.
func Paginate[T any](rows []T, limit int, cursorOf func(T) Cursor) Page[T] {
	if len(rows) <= limit {
		return Page[T]{Items: nonNil(rows)}
	}
	items := rows[:limit]
	next := cursorOf(items[limit-1]).Encode()
	return Page[T]{Items: items, NextCursor: &next}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

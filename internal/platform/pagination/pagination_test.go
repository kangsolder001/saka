package pagination_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/pagination"
)

func TestCursor_RoundTrip(t *testing.T) {
	in := pagination.Cursor{CreatedAt: time.Date(2026, 3, 1, 10, 0, 0, 123000, time.UTC), ID: uuid.New()}

	out, err := pagination.DecodeCursor(in.Encode())

	require.NoError(t, err)
	assert.True(t, in.CreatedAt.Equal(out.CreatedAt))
	assert.Equal(t, in.ID, out.ID)
}

func TestCursor_IsURLSafe(t *testing.T) {
	c := pagination.Cursor{CreatedAt: time.Now().UTC(), ID: uuid.New()}

	assert.Regexp(t, `^[A-Za-z0-9_-]+$`, c.Encode())
}

func TestDecodeCursor_RejectsGarbage(t *testing.T) {
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("hello"))
	missingFields := base64.RawURLEncoding.EncodeToString([]byte(`{}`))
	badID, _ := json.Marshal(map[string]string{"t": "2026-01-01T00:00:00Z", "id": "nope"})

	for name, in := range map[string]string{
		"not base64":     "%%%",
		"not json":       notJSON,
		"missing fields": missingFields,
		"bad id":         base64.RawURLEncoding.EncodeToString(badID),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pagination.DecodeCursor(in)

			assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
		})
	}
}

func TestParseQuery_Defaults(t *testing.T) {
	p, err := pagination.ParseQuery(url.Values{})

	require.NoError(t, err)
	assert.Equal(t, pagination.DefaultLimit, p.Limit)
	assert.Nil(t, p.Cursor)
	assert.Equal(t, pagination.DefaultLimit+1, p.FetchLimit())
}

func TestParseQuery_ValidValues(t *testing.T) {
	c := pagination.Cursor{CreatedAt: time.Now().UTC(), ID: uuid.New()}

	p, err := pagination.ParseQuery(url.Values{"limit": {"50"}, "cursor": {c.Encode()}})

	require.NoError(t, err)
	assert.Equal(t, 50, p.Limit)
	require.NotNil(t, p.Cursor)
	assert.Equal(t, c.ID, p.Cursor.ID)
}

func TestParseQuery_RejectsInvalid(t *testing.T) {
	tests := map[string]url.Values{
		"zero limit":     {"limit": {"0"}},
		"negative limit": {"limit": {"-5"}},
		"over max":       {"limit": {"101"}},
		"non numeric":    {"limit": {"abc"}},
		"bad cursor":     {"cursor": {"garbage!!"}},
	}

	for name, q := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := pagination.ParseQuery(q)

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
		})
	}
}

func TestParseQuery_ReportsAllBadFields(t *testing.T) {
	_, err := pagination.ParseQuery(url.Values{"limit": {"0"}, "cursor": {"!!"}})

	fields := apperr.From(err).Details["fields"].(map[string]string)
	assert.Contains(t, fields, "limit")
	assert.Contains(t, fields, "cursor")
}

type row struct {
	ID        uuid.UUID
	CreatedAt time.Time
}

func cursorOf(r row) pagination.Cursor { return pagination.Cursor{CreatedAt: r.CreatedAt, ID: r.ID} }

func makeRows(n int) []row {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]row, n)
	for i := range rows {
		rows[i] = row{ID: uuid.New(), CreatedAt: base.Add(time.Duration(i) * time.Minute)}
	}
	return rows
}

func TestPaginate_MoreRowsThanLimit(t *testing.T) {
	rows := makeRows(4) // limit 3 + the extra probe row

	page := pagination.Paginate(rows, 3, cursorOf)

	require.Len(t, page.Items, 3)
	require.NotNil(t, page.NextCursor)
	next, err := pagination.DecodeCursor(*page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, rows[2].ID, next.ID, "cursor points at the last returned item, not the probe row")
}

func TestPaginate_LastPageHasNoCursor(t *testing.T) {
	for _, n := range []int{3, 2} {
		page := pagination.Paginate(makeRows(n), 3, cursorOf)

		assert.Len(t, page.Items, n)
		assert.Nil(t, page.NextCursor)
	}
}

func TestPaginate_EmptyResultMarshalsAsEmptyArray(t *testing.T) {
	page := pagination.Paginate[row](nil, 3, cursorOf)

	raw, err := json.Marshal(page)

	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"next_cursor":null}`, string(raw))
}

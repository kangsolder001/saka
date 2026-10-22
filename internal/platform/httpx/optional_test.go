package httpx_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/platform/httpx"
)

func TestOptional_TellsAbsentFromNullFromValue(t *testing.T) {
	type payload struct {
		Logo httpx.Optional[string] `json:"logo"`
	}
	tests := map[string]struct {
		json    string
		wantSet bool
		want    *string
	}{
		"absent": {`{}`, false, nil},
		"null":   {`{"logo":null}`, true, nil},
		"value":  {`{"logo":"x"}`, true, ptr("x")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var p payload

			require.NoError(t, json.Unmarshal([]byte(tt.json), &p))

			assert.Equal(t, tt.wantSet, p.Logo.Set)
			assert.Equal(t, tt.want, p.Logo.Value)
		})
	}
}

func TestOptional_RejectsTheWrongType(t *testing.T) {
	var p struct {
		Logo httpx.Optional[string] `json:"logo"`
	}

	assert.Error(t, json.Unmarshal([]byte(`{"logo":5}`), &p))
}

func ptr[T any](v T) *T { return &v }

package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/validate"
)

// MaxBodyBytes caps JSON request bodies.
const MaxBodyBytes = 1 << 20 // 1 MiB

// BindJSON decodes the request body into dst and validates it. On failure it
// writes the standard VALIDATION_ERROR response, aborts the request and
// returns false; the handler must then return immediately.
//
// Unknown fields and trailing data are rejected so typos in client payloads
// fail loudly instead of being silently ignored.
func BindJSON(c *gin.Context, dst any) bool {
	if err := decodeJSON(c, dst); err != nil {
		Error(c, err)
		return false
	}
	if err := validate.Struct(dst); err != nil {
		Error(c, err)
		return false
	}
	return true
}

func decodeJSON(c *gin.Context, dst any) error {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, MaxBodyBytes)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return bodyError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return apperr.Validation(map[string]string{"body": "must contain a single JSON object"})
	}
	return nil
}

func bodyError(err error) error {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		tooLarge  *http.MaxBytesError
	)
	switch {
	case errors.Is(err, io.EOF):
		return apperr.Validation(map[string]string{"body": "is required"})
	case errors.As(err, &tooLarge):
		return apperr.Validation(map[string]string{"body": "is too large"})
	case errors.As(err, &typeErr):
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		return apperr.Validation(map[string]string{field: "has an invalid type"})
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return apperr.Validation(map[string]string{"body": "is not valid JSON"})
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return apperr.Validation(map[string]string{name: "is an unknown field"})
	default:
		return apperr.Validation(map[string]string{"body": "is invalid"})
	}
}

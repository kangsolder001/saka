// Package validate checks request structs with go-playground/validator and
// converts failures into the standard VALIDATION_ERROR format. Field names
// in the response are the JSON names, not the Go field names.
package validate

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

var (
	once sync.Once
	v    *validator.Validate
)

func instance() *validator.Validate {
	once.Do(func() {
		v = validator.New(validator.WithRequiredStructEnabled())
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
			if name == "-" || name == "" {
				return f.Name
			}
			return name
		})
	})
	return v
}

// Struct validates s using its `validate` tags. It returns nil when valid,
// or a VALIDATION_ERROR *apperr.Error listing every invalid field.
func Struct(s any) error {
	err := instance().Struct(s)
	if err == nil {
		return nil
	}

	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return apperr.Internal(err)
	}

	fields := make(map[string]string, len(verrs))
	for _, fe := range verrs {
		fields[fieldPath(fe)] = message(fe)
	}
	return apperr.Validation(fields)
}

// fieldPath drops the root struct name: "CreateReq.items[0].name" -> "items[0].name".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if i := strings.Index(ns, "."); i >= 0 {
		return ns[i+1:]
	}
	return ns
}

func message(fe validator.FieldError) string {
	param := fe.Param()
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		if isText(fe) {
			return fmt.Sprintf("must be at least %s characters", param)
		}
		return fmt.Sprintf("must be at least %s", param)
	case "max":
		if isText(fe) {
			return fmt.Sprintf("must be at most %s characters", param)
		}
		return fmt.Sprintf("must be at most %s", param)
	case "len":
		return fmt.Sprintf("must be exactly %s characters", param)
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(param, " ", ", ")
	case "uuid", "uuid4", "uuid_rfc4122":
		return "must be a valid UUID"
	case "url", "http_url":
		return "must be a valid URL"
	default:
		return "is invalid"
	}
}

func isText(fe validator.FieldError) bool {
	return fe.Kind() == reflect.String
}

// HTTPURLProblem returns why raw is not an acceptable stored URL, or "". Only
// absolute http(s) URLs up to maxLen characters pass, so a stored value can
// never be a javascript: or data: URL when a client renders it (avatars,
// organization logos).
func HTTPURLProblem(raw string, maxLen int) string {
	if len(raw) > maxLen {
		return fmt.Sprintf("must be at most %d characters", maxLen)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "must be a valid http(s) URL"
	}
	return ""
}

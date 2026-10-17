// Package apperr defines the application error type and the JSON error
// format shared by every module (spec §3). Services return *Error values;
// the HTTP layer turns them into responses.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable error code exposed in API responses.
type Code string

const (
	CodeValidation             Code = "VALIDATION_ERROR"
	CodeUnauthenticated        Code = "UNAUTHENTICATED"
	CodeTokenExpired           Code = "TOKEN_EXPIRED"
	CodeForbidden              Code = "FORBIDDEN"
	CodeEmailNotVerified       Code = "EMAIL_NOT_VERIFIED"
	CodePlanFeatureUnavailable Code = "PLAN_FEATURE_UNAVAILABLE"
	CodePlanLimitReached       Code = "PLAN_LIMIT_REACHED"
	CodeSubscriptionInactive   Code = "SUBSCRIPTION_INACTIVE"
	CodeNotFound               Code = "NOT_FOUND"
	CodeConflict               Code = "CONFLICT"
	CodeRateLimited            Code = "RATE_LIMITED"
	CodeInternal               Code = "INTERNAL_ERROR"
)

var statusByCode = map[Code]int{
	CodeValidation:             http.StatusBadRequest,
	CodeUnauthenticated:        http.StatusUnauthorized,
	CodeTokenExpired:           http.StatusUnauthorized,
	CodeForbidden:              http.StatusForbidden,
	CodeEmailNotVerified:       http.StatusForbidden,
	CodePlanFeatureUnavailable: http.StatusForbidden,
	CodePlanLimitReached:       http.StatusPaymentRequired,
	CodeSubscriptionInactive:   http.StatusPaymentRequired,
	CodeNotFound:               http.StatusNotFound,
	CodeConflict:               http.StatusConflict,
	CodeRateLimited:            http.StatusTooManyRequests,
	CodeInternal:               http.StatusInternalServerError,
}

// Error is an application error carrying an API code, a user-facing message
// and optional details. Cause is kept for logging and never sent to clients.
type Error struct {
	Code    Code
	Message string
	Details map[string]any
	Cause   error
}

// New creates an Error with the given code and user-facing message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause to errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Is matches any *Error with the same Code, so errors.Is(err, apperr.NotFound())
// works without comparing pointers.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// HTTPStatus returns the HTTP status mapped to the error code (500 if unknown).
func (e *Error) HTTPStatus() int {
	if s, ok := statusByCode[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// WithDetails returns a copy of the error with the given details attached.
func (e *Error) WithDetails(details map[string]any) *Error {
	c := *e
	c.Details = details
	return &c
}

// WithCause returns a copy of the error wrapping the underlying cause.
func (e *Error) WithCause(cause error) *Error {
	c := *e
	c.Cause = cause
	return &c
}

// From converts any error into an *Error. Unknown errors become
// INTERNAL_ERROR with the original kept as the cause.
func From(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}

// IsCode reports whether err is (or wraps) an *Error with the given code.
func IsCode(err error, code Code) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Code == code
}

// Body is the JSON payload of an error response: {"error":{...}}.
type Body struct {
	Error Payload `json:"error"`
}

// Payload is the inner object of Body.
type Payload struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// Response returns the HTTP status and JSON body for the error. Internal
// errors never leak their cause or message to the client.
func (e *Error) Response() (int, Body) {
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	message := e.Message
	if e.Code == CodeInternal {
		message = defaultInternalMessage
		details = map[string]any{}
	}
	return e.HTTPStatus(), Body{Error: Payload{Code: e.Code, Message: message, Details: details}}
}

const defaultInternalMessage = "Terjadi kesalahan pada server."

// Validation reports invalid input; fields maps field name to a reason.
func Validation(fields map[string]string) *Error {
	e := New(CodeValidation, "Data yang dikirim tidak valid.")
	if len(fields) > 0 {
		e.Details = map[string]any{"fields": fields}
	}
	return e
}

// Unauthenticated reports missing or invalid credentials.
func Unauthenticated() *Error {
	return New(CodeUnauthenticated, "Autentikasi diperlukan.")
}

// TokenExpired reports an expired access token.
func TokenExpired() *Error {
	return New(CodeTokenExpired, "Token sudah kedaluwarsa.")
}

// Forbidden reports an authenticated user lacking permission.
func Forbidden() *Error {
	return New(CodeForbidden, "Anda tidak memiliki akses.")
}

// EmailNotVerified reports an action that requires a verified email.
func EmailNotVerified() *Error {
	return New(CodeEmailNotVerified, "Email belum diverifikasi.")
}

// PlanFeatureUnavailable reports a feature missing from the current plan.
func PlanFeatureUnavailable(feature string) *Error {
	return New(CodePlanFeatureUnavailable, "Fitur ini tidak tersedia di paket Anda.").
		WithDetails(map[string]any{"feature": feature})
}

// PlanLimitReached reports an exceeded plan limit.
func PlanLimitReached(limit string, max int) *Error {
	return New(CodePlanLimitReached, "Batas paket tercapai.").
		WithDetails(map[string]any{"limit": limit, "max": max})
}

// SubscriptionInactive reports a subscription that no longer grants access.
func SubscriptionInactive() *Error {
	return New(CodeSubscriptionInactive, "Langganan tidak aktif.")
}

// NotFound reports a missing resource (also used to hide resources the
// caller may not know about, e.g. other tenants).
func NotFound() *Error {
	return New(CodeNotFound, "Data tidak ditemukan.")
}

// Conflict reports a state conflict such as a duplicate email.
func Conflict(message string) *Error {
	return New(CodeConflict, message)
}

// RateLimited reports too many requests.
func RateLimited() *Error {
	return New(CodeRateLimited, "Terlalu banyak permintaan. Coba lagi nanti.")
}

// Internal wraps an unexpected failure. The cause is for logs only.
func Internal(cause error) *Error {
	return &Error{Code: CodeInternal, Message: defaultInternalMessage, Cause: cause}
}

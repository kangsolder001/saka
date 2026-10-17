package httpx

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kangsolder001/saka/internal/platform/apperr"
)

// Error aborts the request with the standard JSON error body for err. The
// error is also attached to the gin context so AccessLog records it,
// including the internal cause that clients never see.
func Error(c *gin.Context, err error) {
	appErr := apperr.From(err)
	_ = c.Error(appErr)
	if seconds, ok := appErr.Details[apperr.DetailRetryAfterSeconds].(int); ok && appErr.Code == apperr.CodeRateLimited {
		c.Header("Retry-After", strconv.Itoa(seconds))
	}
	status, body := appErr.Response()
	c.AbortWithStatusJSON(status, body)
}

// NotFound is the handler for unknown routes.
func NotFound(c *gin.Context) {
	Error(c, apperr.NotFound())
}

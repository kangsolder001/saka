package auth

import (
	"context"
	"time"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
)

// Rate limit rules per endpoint (spec §5.4).
var (
	// RuleLogin counts FAILED attempts per (email + IP).
	RuleLogin = ratelimit.Rule{Name: "login", Limit: 5, Window: 15 * time.Minute}
	// RuleForgotPassword counts requests per email, whether or not it is registered.
	RuleForgotPassword = ratelimit.Rule{Name: "forgot_password", Limit: 3, Window: time.Hour}
	// RuleResendVerification counts requests per user.
	RuleResendVerification = ratelimit.Rule{Name: "resend_verification", Limit: 3, Window: time.Hour}
	// RulePasswordConfirm counts FAILED "confirm your current password" checks per
	// user (change password, change email, delete account). Without it a stolen
	// access token could be used to guess the password.
	RulePasswordConfirm = ratelimit.Rule{Name: "password_confirm", Limit: 5, Window: 15 * time.Minute}
	// RuleEmailChange counts change-email requests per user: each one sends an
	// email to an address the user chose, so it must not become a spam tool.
	RuleEmailChange = ratelimit.Rule{Name: "email_change", Limit: 3, Window: time.Hour}
	// RuleRegister counts sign-up attempts per IP.
	RuleRegister = ratelimit.Rule{Name: "register", Limit: 10, Window: time.Hour}
)

// RateLimiter is the limiter auth uses; *ratelimit.Limiter implements it.
type RateLimiter interface {
	Take(ctx context.Context, key string, rule ratelimit.Rule) (ratelimit.Decision, error)
	Check(ctx context.Context, key string, rule ratelimit.Rule) (ratelimit.Decision, error)
	Record(ctx context.Context, key string, rule ratelimit.Rule) error
	Reset(ctx context.Context, key string) error
}

var _ RateLimiter = (*ratelimit.Limiter)(nil)

// take consumes one event of rule for key, or fails with RATE_LIMITED.
func (s *Service) take(ctx context.Context, rule ratelimit.Rule, parts ...string) error {
	d, err := s.Limiter.Take(ctx, ratelimit.Key(rule.Name, parts...), rule)
	if err != nil {
		return err
	}
	if !d.Allowed {
		return apperr.RateLimitedAfter(d.RetryAfter)
	}
	return nil
}

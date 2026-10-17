package auth

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
	"github.com/kangsolder001/saka/internal/user"
)

// Account security: what a logged-in user can do to their own credentials and
// sessions (spec §6). The user module serves these endpoints and reaches this
// code through its AccountSecurity interface, because auth owns passwords,
// one-time tokens and sessions.

// confirmPassword checks the caller's current password before a sensitive
// action. Wrong guesses are limited per user (RulePasswordConfirm); a correct
// one clears the count. A wrong password is a VALIDATION_ERROR on
// "current_password", not UNAUTHENTICATED: the access token is fine, and
// clients treat 401 as "log in again".
func (s *Service) confirmPassword(ctx context.Context, u user.User, password string) error {
	limitKey := ratelimit.Key(RulePasswordConfirm.Name, u.ID.String())
	decision, err := s.Limiter.Check(ctx, limitKey, RulePasswordConfirm)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apperr.RateLimitedAfter(decision.RetryAfter)
	}

	ok, _, err := s.Hasher.Verify(password, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("auth: verify password: %w", err)
	}
	if !ok {
		if err := s.Limiter.Record(ctx, limitKey, RulePasswordConfirm); err != nil {
			zl := s.Log.FromContext(ctx)
			zl.Warn().Err(err).Msg("could not record failed password confirmation")
		}
		return apperr.Validation(map[string]string{"current_password": "is incorrect"})
	}
	if err := s.Limiter.Reset(ctx, limitKey); err != nil {
		zl := s.Log.FromContext(ctx)
		zl.Warn().Err(err).Msg("could not reset failed password confirmations")
	}
	return nil
}

// ChangePassword replaces the caller's password after checking the current one.
// In one transaction it stores the new hash, revokes every session except
// currentSession (the caller stays logged in) and queues the "password
// changed" notice (spec §6).
func (s *Service) ChangePassword(ctx context.Context, userID, currentSession uuid.UUID, currentPassword, newPassword string) error {
	// Cheap checks first, before reading the user or spending time hashing.
	if err := ValidatePassword(newPassword, ""); err != nil {
		return passwordFieldAs(err, "new_password")
	}
	u, err := s.Users.GetByID(ctx, s.Pool, userID)
	if err != nil {
		return err
	}
	if err := s.confirmPassword(ctx, u, currentPassword); err != nil {
		return err
	}
	if err := ValidatePassword(newPassword, u.Email); err != nil {
		return passwordFieldAs(err, "new_password")
	}
	if newPassword == currentPassword {
		return apperr.Validation(map[string]string{"new_password": "must be different from the current password"}) //nolint:gosec // G101 false positive: a validation message, not a credential
	}

	hash, err := s.Hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}
	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.Users.SetPasswordHash(ctx, tx, u.ID, hash); err != nil {
			return err
		}
		if err := s.Sessions.RevokeOtherSessions(ctx, tx, u.ID, currentSession); err != nil {
			return err
		}
		return s.Mail.Send(ctx, tx, mail.Email{
			Template: "password_changed",
			To:       u.Email,
			Data:     map[string]any{"name": u.Name},
		})
	})
}

// passwordFieldAs re-keys the "password" field error of ValidatePassword to
// the request's own field name.
func passwordFieldAs(err error, field string) error {
	appErr := apperr.From(err)
	fields, ok := appErr.Details["fields"].(map[string]string)
	if !ok {
		return err
	}
	return apperr.Validation(map[string]string{field: fields["password"]})
}

package auth

import (
	"context"
	"encoding/json"
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

// ConfirmPassword checks userID's current password before a sensitive action
// owned by another module (transferring organization ownership). It has the
// same behaviour as the checks inside this package: a wrong password is a
// VALIDATION_ERROR on "current_password", and wrong guesses share the
// per-user limit.
func (s *Service) ConfirmPassword(ctx context.Context, userID uuid.UUID, password string) error {
	u, err := s.Users.GetByID(ctx, s.Pool, userID)
	if err != nil {
		return err
	}
	return s.confirmPassword(ctx, u, password)
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

// emailChangePayload is stored with the email_change token.
type emailChangePayload struct {
	NewEmail string `json:"new_email"`
}

// RequestEmailChange starts changing the caller's email: after checking the
// current password it emails a confirmation link to the NEW address. Nothing
// changes until that link is used, so a typo or someone else's address can
// never lock the user out. A newer request invalidates the older link.
func (s *Service) RequestEmailChange(ctx context.Context, userID uuid.UUID, currentPassword, newEmail string) error {
	newEmail = user.NormalizeEmail(newEmail)
	u, err := s.Users.GetByID(ctx, s.Pool, userID)
	if err != nil {
		return err
	}
	if err := s.confirmPassword(ctx, u, currentPassword); err != nil {
		return err
	}
	// Counted before the lookups below, which would otherwise let a caller
	// probe which addresses are registered at will.
	if err := s.take(ctx, RuleEmailChange, userID.String()); err != nil {
		return err
	}
	if newEmail == u.Email {
		return apperr.Validation(map[string]string{"new_email": "must be different from the current email"})
	}
	if _, err := s.Users.GetByEmail(ctx, s.Pool, newEmail); err == nil {
		return apperr.Conflict("Email sudah terdaftar.")
	} else if !apperr.IsCode(err, apperr.CodeNotFound) {
		return err
	}

	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		raw, err := s.Tokens.Issue(ctx, tx, u.ID, TokenEmailChange, emailChangePayload{NewEmail: newEmail})
		if err != nil {
			return err
		}
		return s.Mail.Send(ctx, tx, mail.Email{
			Template: "email_change_confirm",
			To:       newEmail,
			Data: map[string]any{
				"name": u.Name,
				"url":  s.link("/confirm-email-change", raw),
			},
		})
	})
}

// ConfirmEmailChange applies a change requested with RequestEmailChange. The
// token is the credential (the link is opened from the new inbox, possibly on
// another device), so no login is needed. In one transaction it consumes the
// token, switches the address (now verified) and queues a notice to the OLD
// address. It returns the new email. A taken address returns CONFLICT and
// leaves the token unused.
func (s *Service) ConfirmEmailChange(ctx context.Context, rawToken string) (string, error) {
	var newEmail string
	err := s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		tok, err := s.Tokens.Consume(ctx, tx, rawToken, TokenEmailChange)
		if err != nil {
			return err
		}
		var payload emailChangePayload
		if err := json.Unmarshal(tok.Payload, &payload); err != nil || payload.NewEmail == "" {
			return ErrInvalidToken()
		}
		u, err := s.Users.GetByID(ctx, tx, tok.UserID)
		if err != nil {
			if apperr.IsCode(err, apperr.CodeNotFound) {
				return ErrInvalidToken()
			}
			return err
		}
		if u.Status != user.StatusActive || u.Email == payload.NewEmail {
			return ErrInvalidToken()
		}
		// ChangeEmail clears the cached summary once this transaction commits.
		if err := s.Users.ChangeEmail(ctx, tx, u.ID, payload.NewEmail); err != nil {
			return err
		}
		newEmail = payload.NewEmail
		// The new address is proven (the link reached it), so invitations sent to
		// it can be claimed, as after email verification.
		if err := s.Provisioner.AcceptPendingInvitations(ctx, tx, u.ID, payload.NewEmail); err != nil {
			return err
		}
		return s.Mail.Send(ctx, tx, mail.Email{
			Template: "email_changed_notice",
			To:       u.Email,
			Data:     map[string]any{"name": u.Name, "new_email": payload.NewEmail},
		})
	})
	if err != nil {
		return "", err
	}
	return newEmail, nil
}

// ListSessions returns the caller's live sessions, most recently used first,
// with the one making this request flagged.
func (s *Service) ListSessions(ctx context.Context, userID, currentSession uuid.UUID) ([]user.Session, error) {
	infos, err := s.Sessions.ListSessions(ctx, s.Pool, userID)
	if err != nil {
		return nil, err
	}
	out := make([]user.Session, len(infos))
	for i, info := range infos {
		out[i] = user.Session{
			ID:         info.ID,
			UserAgent:  info.UserAgent,
			IP:         info.IP,
			StartedAt:  info.StartedAt,
			LastUsedAt: info.LastUsedAt,
			ExpiresAt:  info.ExpiresAt,
			Current:    info.ID == currentSession,
		}
	}
	return out, nil
}

// RevokeSession ends one of the caller's sessions. A session that is not the
// caller's (or does not exist, or is already over) is NOT_FOUND, so IDs cannot
// be probed. Ending the current session is allowed and works like logout: its
// refresh cookie dies with its next use.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	revoked, err := s.Sessions.RevokeUserSession(ctx, s.Pool, userID, sessionID)
	if err != nil {
		return err
	}
	if !revoked {
		return apperr.NotFound()
	}
	return nil
}

// DeleteAccount deletes the caller's account after checking their password.
// In one transaction it lets the organization module refuse (the user still
// owns an organization with other members: CONFLICT) or clean up, soft-deletes
// the user (the email is anonymized) and revokes every session. The user's
// cached summary is dropped once the transaction commits, so the account stops
// working immediately.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID, currentPassword string) error {
	u, err := s.Users.GetByID(ctx, s.Pool, userID)
	if err != nil {
		return err
	}
	if err := s.confirmPassword(ctx, u, currentPassword); err != nil {
		return err
	}
	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		if err := s.Orgs.BeforeUserDeleted(ctx, tx, u.ID); err != nil {
			return err
		}
		if err := s.Users.Delete(ctx, tx, u.ID); err != nil {
			return err
		}
		return s.Sessions.RevokeAllSessions(ctx, tx, u.ID)
	})
}

package auth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/platform/ratelimit"
	"github.com/kangsolder001/saka/internal/user"
)

// Transactor runs a function in a database transaction; *db.DB implements it.
type Transactor interface {
	WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

// UserService is what auth needs from the user module; *user.Service implements it.
type UserService interface {
	Create(ctx context.Context, q db.DBTX, in user.NewUser) (user.User, error)
	GetByID(ctx context.Context, q db.DBTX, id uuid.UUID) (user.User, error)
	GetByEmail(ctx context.Context, q db.DBTX, email string) (user.User, error)
	// GetSummary is the cached, password-free read used on hot paths
	// (authenticating requests, refreshing tokens). Reads inside a transaction
	// use GetByID instead.
	GetSummary(ctx context.Context, q db.DBTX, id uuid.UUID) (user.Summary, error)
	MarkEmailVerified(ctx context.Context, q db.DBTX, id uuid.UUID) error
	SetPasswordHash(ctx context.Context, q db.DBTX, id uuid.UUID, hash string) error
	ChangeEmail(ctx context.Context, q db.DBTX, id uuid.UUID, newEmail string) error
}

// TokenService issues and consumes single-use tokens; *UserTokens implements it.
type TokenService interface {
	Issue(ctx context.Context, q db.DBTX, userID uuid.UUID, typ TokenType, payload any) (string, error)
	Consume(ctx context.Context, q db.DBTX, raw string, typ TokenType) (UserToken, error)
}

// RefreshStore manages refresh token sessions; *RefreshTokens implements it.
type RefreshStore interface {
	StartSession(ctx context.Context, q db.DBTX, userID uuid.UUID, client ClientInfo) (RefreshToken, error)
	SessionActive(ctx context.Context, q db.DBTX, userID, familyID uuid.UUID) (bool, error)
	Rotate(ctx context.Context, q db.DBTX, raw string, client ClientInfo) (RotateResult, error)
	RevokeSession(ctx context.Context, q db.DBTX, familyID uuid.UUID) error
	RevokeAllSessions(ctx context.Context, q db.DBTX, userID uuid.UUID) error
	RevokeOtherSessions(ctx context.Context, q db.DBTX, userID, keepFamilyID uuid.UUID) error
	ListSessions(ctx context.Context, q db.DBTX, userID uuid.UUID) ([]SessionInfo, error)
	RevokeUserSession(ctx context.Context, q db.DBTX, userID, sessionID uuid.UUID) (bool, error)
}

var (
	_ UserService  = (*user.Service)(nil)
	_ TokenService = (*UserTokens)(nil)
	_ RefreshStore = (*RefreshTokens)(nil)
	_ Transactor   = (*db.DB)(nil)
)

// Config holds the settings the Service needs.
type Config struct {
	// FrontendURL is the base of links in emails, e.g. https://app.example.com.
	FrontendURL string
}

// Dependencies are the collaborators of Service, injected by the composition root.
type Dependencies struct {
	Tx     Transactor
	Pool   db.DBTX // used for reads outside a transaction
	Users  UserService
	Tokens TokenService
	// Sessions stores refresh token sessions.
	Sessions RefreshStore
	// JWT signs access tokens.
	JWT    *JWT
	Mail   mail.Sender
	Hasher *PasswordHasher
	// Limiter enforces the per-endpoint rate limits (spec §5.4).
	Limiter RateLimiter
	Clock   clock.Clock
	Log     *logger.Logger
}

// Service implements the auth use cases (spec §5).
type Service struct {
	Dependencies
	cfg Config
}

// NewService creates the auth Service.
func NewService(deps Dependencies, cfg Config) *Service {
	cfg.FrontendURL = strings.TrimRight(cfg.FrontendURL, "/")
	return &Service{Dependencies: deps, cfg: cfg}
}

// RegisterInput is the data needed to sign up.
type RegisterInput struct {
	Email    string
	Password string
	Name     string
	// OrganizationName is validated now but only used once organizations exist (Phase 4).
	OrganizationName string
	// ClientIP is the caller's address, used for the sign-up rate limit.
	ClientIP string
}

// Register creates the user and queues the verification email in a single
// transaction: either everything exists afterwards or nothing does. It does
// not log the user in. A taken email returns CONFLICT.
func (s *Service) Register(ctx context.Context, in RegisterInput) (user.User, error) {
	if err := s.take(ctx, RuleRegister, in.ClientIP); err != nil {
		return user.User{}, err
	}
	email := user.NormalizeEmail(in.Email)
	name := strings.TrimSpace(in.Name)

	fields := map[string]string{}
	if name == "" {
		fields["name"] = "is required"
	}
	if strings.TrimSpace(in.OrganizationName) == "" {
		fields["organization_name"] = "is required"
	}
	if err := ValidatePassword(in.Password, email); err != nil {
		fields["password"] = apperr.From(err).Details["fields"].(map[string]string)["password"]
	}
	if len(fields) > 0 {
		return user.User{}, apperr.Validation(fields)
	}

	// Hash before opening the transaction: it is deliberately slow and must
	// not hold a database connection while it runs.
	hash, err := s.Hasher.Hash(in.Password)
	if err != nil {
		return user.User{}, fmt.Errorf("auth: hash password: %w", err)
	}

	var created user.User
	err = s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = s.Users.Create(ctx, tx, user.NewUser{Email: email, PasswordHash: hash, Name: name})
		if err != nil {
			return err
		}
		return s.sendVerificationEmail(ctx, tx, created.Summary())
	})
	if err != nil {
		return user.User{}, err
	}
	return created, nil
}

// sendVerificationEmail issues a verification token and queues the email,
// using the caller's transaction.
func (s *Service) sendVerificationEmail(ctx context.Context, tx db.DBTX, u user.Summary) error {
	raw, err := s.Tokens.Issue(ctx, tx, u.ID, TokenEmailVerification, nil)
	if err != nil {
		return err
	}
	return s.Mail.Send(ctx, tx, mail.Email{
		Template: "verify_email",
		To:       u.Email,
		Data: map[string]any{
			"name": u.Name,
			"url":  s.link("/verify-email", raw),
		},
	})
}

// link builds a frontend URL carrying a one-time token.
func (s *Service) link(path, token string) string {
	return s.cfg.FrontendURL + path + "?token=" + url.QueryEscape(token)
}

// VerifyEmail consumes the verification token and marks the user's email as
// verified, atomically. An unknown, expired or already used token fails with a
// validation error on "token". Verifying twice is harmless: the second token
// is simply rejected.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		tok, err := s.Tokens.Consume(ctx, tx, rawToken, TokenEmailVerification)
		if err != nil {
			return err
		}
		// MarkEmailVerified clears the user's cached summary once this
		// transaction commits; no cache handling is needed here.
		return s.Users.MarkEmailVerified(ctx, tx, tok.UserID)
	})
}

// ResendVerification issues a fresh verification token (invalidating older
// ones) and queues a new email. A user whose email is already verified gets
// CONFLICT.
func (s *Service) ResendVerification(ctx context.Context, userID uuid.UUID) error {
	if err := s.take(ctx, RuleResendVerification, userID.String()); err != nil {
		return err
	}
	u, err := s.Users.GetSummary(ctx, s.Pool, userID)
	if err != nil {
		return err
	}
	if u.EmailVerified() {
		return apperr.Conflict("Email sudah terverifikasi.")
	}
	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		return s.sendVerificationEmail(ctx, tx, u)
	})
}

// LoginInput is a login attempt.
type LoginInput struct {
	Email    string
	Password string
	Client   ClientInfo
}

// LoginResult is a successful login: the user plus both tokens.
type LoginResult struct {
	User             user.User
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// errInvalidCredentials is returned for every kind of failed login so the
// response never reveals whether the email exists.
func errInvalidCredentials() error {
	return apperr.New(apperr.CodeUnauthenticated, "Email atau password salah.")
}

// Login verifies the credentials and starts a session. Unknown email, wrong
// password and deleted accounts all produce the same error, and unknown
// emails still spend a hash's worth of time, so neither the response nor its
// latency reveals which accounts exist. A user whose email is not yet
// verified may log in (spec §5.2).
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	// Only failed attempts count (spec §5.4), keyed by email AND IP so an
	// attacker cannot lock a victim out from another address.
	limitKey := ratelimit.Key(RuleLogin.Name, user.NormalizeEmail(in.Email), in.Client.IP)
	decision, err := s.Limiter.Check(ctx, limitKey, RuleLogin)
	if err != nil {
		return LoginResult{}, err
	}
	if !decision.Allowed {
		return LoginResult{}, apperr.RateLimitedAfter(decision.RetryAfter)
	}

	u, err := s.Users.GetByEmail(ctx, s.Pool, in.Email)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeNotFound) {
			s.Hasher.VerifyDummy(in.Password)
			s.recordLoginFailure(ctx, limitKey)
			return LoginResult{}, errInvalidCredentials()
		}
		return LoginResult{}, err
	}

	ok, needsRehash, err := s.Hasher.Verify(in.Password, u.PasswordHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: verify password: %w", err)
	}
	if !ok {
		s.recordLoginFailure(ctx, limitKey)
		return LoginResult{}, errInvalidCredentials()
	}
	if u.Status != user.StatusActive {
		return LoginResult{}, apperr.Forbidden().WithDetails(map[string]any{"reason": "account_" + string(u.Status)})
	}
	if needsRehash {
		s.upgradePasswordHash(ctx, u.ID, in.Password)
	}

	var refresh RefreshToken
	if err := s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		refresh, err = s.Sessions.StartSession(ctx, tx, u.ID, in.Client)
		return err
	}); err != nil {
		return LoginResult{}, err
	}

	access, accessExpires, err := s.JWT.Sign(u.ID, refresh.FamilyID)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.Limiter.Reset(ctx, limitKey); err != nil {
		zl := s.Log.FromContext(ctx)
		zl.Warn().Err(err).Msg("could not reset login failures")
	}
	return LoginResult{
		User:             u,
		AccessToken:      access,
		AccessExpiresAt:  accessExpires,
		RefreshToken:     refresh.Raw,
		RefreshExpiresAt: refresh.ExpiresAt,
	}, nil
}

// recordLoginFailure counts a failed login. It writes outside any transaction
// so the count survives, and a failure to record must not turn into a
// different response for the client.
func (s *Service) recordLoginFailure(ctx context.Context, limitKey string) {
	if err := s.Limiter.Record(ctx, limitKey, RuleLogin); err != nil {
		zl := s.Log.FromContext(ctx)
		zl.Warn().Err(err).Msg("could not record failed login")
	}
}

// upgradePasswordHash re-hashes a password with the current Argon2 cost after
// a successful login. It is best effort: failing must not fail the login.
func (s *Service) upgradePasswordHash(ctx context.Context, userID uuid.UUID, password string) {
	zl := s.Log.FromContext(ctx)
	hash, err := s.Hasher.Hash(password)
	if err != nil {
		zl.Warn().Err(err).Msg("password rehash failed")
		return
	}
	if err := s.Users.SetPasswordHash(ctx, s.Pool, userID, hash); err != nil {
		zl.Warn().Err(err).Msg("password rehash could not be saved")
	}
}

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID        uuid.UUID
	SessionID     uuid.UUID
	EmailVerified bool
}

// Authenticate turns an access token into a Principal. Beyond the JWT's
// signature and expiry it checks, on every call, that the user still exists and
// is active (from the user summary cache, so most calls skip the database) and
// that the session (refresh token family) is still alive (always from the
// database). Logout and password reset take effect immediately; a suspension or
// deletion takes effect as soon as the user cache is invalidated, and at the
// latest when the cached entry expires (redis.user_cache_ttl).
func (s *Service) Authenticate(ctx context.Context, accessToken string) (Principal, error) {
	claims, err := s.JWT.Verify(accessToken)
	if err != nil {
		return Principal{}, err
	}

	u, err := s.Users.GetSummary(ctx, s.Pool, claims.UserID)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeNotFound) {
			return Principal{}, apperr.Unauthenticated()
		}
		return Principal{}, err
	}
	if u.Status != user.StatusActive {
		return Principal{}, apperr.Unauthenticated()
	}

	active, err := s.Sessions.SessionActive(ctx, s.Pool, u.ID, claims.SessionID)
	if err != nil {
		return Principal{}, err
	}
	if !active {
		return Principal{}, apperr.Unauthenticated()
	}
	return Principal{UserID: u.ID, SessionID: claims.SessionID, EmailVerified: u.EmailVerified()}, nil
}

// RefreshResult is a rotated token pair.
type RefreshResult struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// Refresh exchanges a refresh token for a new access token and a new refresh
// token (rotation, spec §5.3). An unknown or expired token is rejected. A
// token that was already used is treated as stolen: the whole session is
// revoked and the request rejected. All failures look the same to the client
// (UNAUTHENTICATED).
func (s *Service) Refresh(ctx context.Context, rawToken string, client ClientInfo) (RefreshResult, error) {
	var rotated RotateResult
	err := s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		rotated, err = s.Sessions.Rotate(ctx, tx, rawToken, client)
		// For Reused the family revocation is committed precisely because we
		// return nil here, even though the request is rejected below.
		return err
	})
	if err != nil {
		return RefreshResult{}, err
	}

	switch rotated.Outcome {
	case Rotated:
	case Reused:
		zl := s.Log.FromContext(ctx)
		zl.Warn().Msg("refresh token reuse detected; session revoked")
		return RefreshResult{}, apperr.Unauthenticated()
	default: // Unknown, Expired
		return RefreshResult{}, apperr.Unauthenticated()
	}

	// The account check reads the cached summary (this endpoint is hit often).
	// Rotation is already committed, which is harmless: an inactive user cannot
	// use the new tokens anyway, as Authenticate rejects them.
	u, err := s.Users.GetSummary(ctx, s.Pool, rotated.UserID)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeNotFound) {
			return RefreshResult{}, apperr.Unauthenticated()
		}
		return RefreshResult{}, err
	}
	if u.Status != user.StatusActive {
		return RefreshResult{}, apperr.Unauthenticated()
	}

	access, accessExpires, err := s.JWT.Sign(u.ID, rotated.Token.FamilyID)
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{
		AccessToken:      access,
		AccessExpiresAt:  accessExpires,
		RefreshToken:     rotated.Token.Raw,
		RefreshExpiresAt: rotated.Token.ExpiresAt,
	}, nil
}

// Logout ends the caller's session: every refresh token of that login is
// revoked, so neither the refresh token nor the access token (which Authenticate
// ties to the session) works any more. Other devices stay logged in.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	return s.Sessions.RevokeSession(ctx, s.Pool, p.SessionID)
}

// ForgotPassword emails a password reset link when the address belongs to an
// active account. It returns nil whether or not the account exists, so the
// caller can always answer 200 and the endpoint cannot be used to discover
// which emails are registered. (The token insert is the only extra work for an
// existing account and the email itself is sent asynchronously, so the timing
// difference is negligible; the per-email rate limit covers the rest.)
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	// The limit applies to the address itself, registered or not, so hitting it
	// reveals nothing about the account.
	if err := s.take(ctx, RuleForgotPassword, user.NormalizeEmail(email)); err != nil {
		return err
	}
	u, err := s.Users.GetByEmail(ctx, s.Pool, email)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeNotFound) {
			return nil
		}
		return err
	}
	if u.Status != user.StatusActive {
		return nil
	}

	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		raw, err := s.Tokens.Issue(ctx, tx, u.ID, TokenPasswordReset, nil)
		if err != nil {
			return err
		}
		return s.Mail.Send(ctx, tx, mail.Email{
			Template: "password_reset",
			To:       u.Email,
			Data: map[string]any{
				"name": u.Name,
				"url":  s.link("/reset-password", raw),
			},
		})
	})
}

// ResetPassword sets a new password using a reset token. In one transaction it
// consumes the token, stores the new hash, revokes every session of the user
// and queues the "password changed" notice. If anything fails (for example the
// new password equals the email) the token is not consumed and can be retried.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	// Cheap checks and the slow hash happen before the transaction. The
	// "must differ from the email" rule needs the user, so it runs inside.
	if err := ValidatePassword(newPassword, ""); err != nil {
		return err
	}
	hash, err := s.Hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash password: %w", err)
	}

	return s.Tx.WithTx(ctx, func(tx pgx.Tx) error {
		tok, err := s.Tokens.Consume(ctx, tx, rawToken, TokenPasswordReset)
		if err != nil {
			return err
		}
		u, err := s.Users.GetByID(ctx, tx, tok.UserID)
		if err != nil {
			if apperr.IsCode(err, apperr.CodeNotFound) {
				return ErrInvalidToken()
			}
			return err
		}
		if u.Status != user.StatusActive {
			return ErrInvalidToken()
		}
		if err := ValidatePassword(newPassword, u.Email); err != nil {
			return err
		}
		if err := s.Users.SetPasswordHash(ctx, tx, u.ID, hash); err != nil {
			return err
		}
		if err := s.Sessions.RevokeAllSessions(ctx, tx, u.ID); err != nil {
			return err
		}
		return s.Mail.Send(ctx, tx, mail.Email{
			Template: "password_changed",
			To:       u.Email,
			Data:     map[string]any{"name": u.Name},
		})
	})
}

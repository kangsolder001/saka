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
	MarkEmailVerified(ctx context.Context, q db.DBTX, id uuid.UUID) error
	SetPasswordHash(ctx context.Context, q db.DBTX, id uuid.UUID, hash string) error
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
	// Refresh stores refresh token sessions.
	Refresh RefreshStore
	// JWT signs access tokens.
	JWT    *JWT
	Mail   mail.Sender
	Hasher *PasswordHasher
	Clock  clock.Clock
	Log    *logger.Logger
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
}

// Register creates the user and queues the verification email in a single
// transaction: either everything exists afterwards or nothing does. It does
// not log the user in. A taken email returns CONFLICT.
func (s *Service) Register(ctx context.Context, in RegisterInput) (user.User, error) {
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
		return s.sendVerificationEmail(ctx, tx, created)
	})
	if err != nil {
		return user.User{}, err
	}
	return created, nil
}

// sendVerificationEmail issues a verification token and queues the email,
// using the caller's transaction.
func (s *Service) sendVerificationEmail(ctx context.Context, tx db.DBTX, u user.User) error {
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
		return s.Users.MarkEmailVerified(ctx, tx, tok.UserID)
	})
}

// ResendVerification issues a fresh verification token (invalidating older
// ones) and queues a new email. A user whose email is already verified gets
// CONFLICT.
func (s *Service) ResendVerification(ctx context.Context, userID uuid.UUID) error {
	u, err := s.Users.GetByID(ctx, s.Pool, userID)
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
	u, err := s.Users.GetByEmail(ctx, s.Pool, in.Email)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeNotFound) {
			s.Hasher.VerifyDummy(in.Password)
			return LoginResult{}, errInvalidCredentials()
		}
		return LoginResult{}, err
	}

	ok, needsRehash, err := s.Hasher.Verify(in.Password, u.PasswordHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: verify password: %w", err)
	}
	if !ok {
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
		refresh, err = s.Refresh.StartSession(ctx, tx, u.ID, in.Client)
		return err
	}); err != nil {
		return LoginResult{}, err
	}

	access, accessExpires, err := s.JWT.Sign(u.ID, refresh.FamilyID)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		User:             u,
		AccessToken:      access,
		AccessExpiresAt:  accessExpires,
		RefreshToken:     refresh.Raw,
		RefreshExpiresAt: refresh.ExpiresAt,
	}, nil
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

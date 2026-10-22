// Package user owns the users table. Other modules reach user data only
// through Service (spec §2): they never query the table themselves.
package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/clock"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/platform/logger"
	"github.com/kangsolder001/saka/internal/user/userdb"
)

// Status is the account state stored in users.status.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusDeleted   Status = "deleted"
)

// User is an account.
type User struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	Name            string
	AvatarURL       *string
	EmailVerifiedAt *time.Time
	Status          Status
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// EmailVerified reports whether the user has confirmed their email address.
func (u User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

// NewUser holds the data needed to create an account.
type NewUser struct {
	Email        string
	PasswordHash string
	Name         string
}

// Service is the public API of the user module. Every method takes the
// database handle to use (the pool, or a transaction when the call belongs
// to a larger unit of work such as registration).
type Service struct {
	clk clock.Clock

	// Optional summary cache (see WithCache); nil means every read hits the database.
	cache    Cache
	cacheTTL time.Duration
	log      *logger.Logger
}

// NewService creates a Service.
func NewService(clk clock.Clock, opts ...Option) *Service {
	s := &Service{clk: clk}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NormalizeEmail trims surrounding spaces and lowercases the address. The
// column is case-insensitive already; normalizing keeps stored values tidy.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Create inserts a user. A duplicate email returns apperr CONFLICT.
func (s *Service) Create(ctx context.Context, q db.DBTX, in NewUser) (User, error) {
	row, err := userdb.New(q).CreateUser(ctx, userdb.CreateUserParams{
		Email:        NormalizeEmail(in.Email),
		PasswordHash: in.PasswordHash,
		Name:         strings.TrimSpace(in.Name),
		Now:          s.clk.Now(),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, apperr.Conflict("Email sudah terdaftar.")
		}
		return User{}, fmt.Errorf("user: create: %w", err)
	}
	return fromRow(row), nil
}

// GetByID returns the user, or apperr NOT_FOUND (deleted users count as missing).
func (s *Service) GetByID(ctx context.Context, q db.DBTX, id uuid.UUID) (User, error) {
	row, err := userdb.New(q).GetUserByID(ctx, id)
	return one(row, err, "get by id")
}

// GetByEmail returns the user with this email, or apperr NOT_FOUND.
func (s *Service) GetByEmail(ctx context.Context, q db.DBTX, email string) (User, error) {
	row, err := userdb.New(q).GetUserByEmail(ctx, NormalizeEmail(email))
	return one(row, err, "get by email")
}

// GetSummaries returns the summaries of the given users keyed by ID, read
// straight from the database (listings are not the hot path the cache serves).
// Unknown and deleted users are simply absent from the result.
func (s *Service) GetSummaries(ctx context.Context, q db.DBTX, ids []uuid.UUID) (map[uuid.UUID]Summary, error) {
	out := make(map[uuid.UUID]Summary, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := userdb.New(q).ListUsersByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("user: list by ids: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = fromRow(r).Summary()
	}
	return out, nil
}

// MarkEmailVerified records that the user confirmed their email. It is
// idempotent: verifying twice keeps the first timestamp.
func (s *Service) MarkEmailVerified(ctx context.Context, q db.DBTX, id uuid.UUID) error {
	if _, err := userdb.New(q).MarkEmailVerified(ctx, userdb.MarkEmailVerifiedParams{ID: id, Now: s.clk.Now()}); err != nil {
		return fmt.Errorf("user: mark email verified: %w", err)
	}
	s.invalidateAfterCommit(ctx, q, id)
	return nil
}

// SetPasswordHash replaces the stored password hash. The cached summary never
// contains the hash, so there is nothing to invalidate.
func (s *Service) SetPasswordHash(ctx context.Context, q db.DBTX, id uuid.UUID, hash string) error {
	n, err := userdb.New(q).UpdatePasswordHash(ctx, userdb.UpdatePasswordHashParams{ID: id, PasswordHash: hash, Now: s.clk.Now()})
	if err != nil {
		return fmt.Errorf("user: set password hash: %w", err)
	}
	if n == 0 {
		return apperr.NotFound()
	}
	return nil
}

// ChangeEmail replaces the user's email with one they have just confirmed
// (through a link sent to the new address), which also makes it verified. A
// taken address returns apperr CONFLICT.
func (s *Service) ChangeEmail(ctx context.Context, q db.DBTX, id uuid.UUID, newEmail string) error {
	n, err := userdb.New(q).UpdateEmail(ctx, userdb.UpdateEmailParams{ID: id, Email: NormalizeEmail(newEmail), Now: s.clk.Now()})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.Conflict("Email sudah terdaftar.")
		}
		return fmt.Errorf("user: change email: %w", err)
	}
	if n == 0 {
		return apperr.NotFound()
	}
	s.invalidateAfterCommit(ctx, q, id)
	return nil
}

// Delete soft-deletes the account: status becomes deleted, deleted_at is set
// and the email is replaced by deleted+<id>@invalid so the real address can be
// used to sign up again (spec §6). It does not end sessions or touch
// organizations; the caller (auth) does that in the same transaction. An
// unknown or already deleted user returns apperr NOT_FOUND.
func (s *Service) Delete(ctx context.Context, q db.DBTX, id uuid.UUID) error {
	n, err := userdb.New(q).SoftDeleteUser(ctx, userdb.SoftDeleteUserParams{ID: id, Now: s.clk.Now()})
	if err != nil {
		return fmt.Errorf("user: delete: %w", err)
	}
	if n == 0 {
		return apperr.NotFound()
	}
	s.invalidateAfterCommit(ctx, q, id)
	return nil
}

func one(row userdb.User, err error, op string) (User, error) {
	if err != nil {
		return User{}, notFoundOr(err, op)
	}
	return fromRow(row), nil
}

// notFoundOr maps "no row" to apperr NOT_FOUND and wraps anything else.
func notFoundOr(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return fmt.Errorf("user: %s: %w", op, err)
}

func fromRow(r userdb.User) User {
	return User{
		ID:              r.ID,
		Email:           r.Email,
		PasswordHash:    r.PasswordHash,
		Name:            r.Name,
		AvatarURL:       r.AvatarUrl,
		EmailVerifiedAt: r.EmailVerifiedAt,
		Status:          Status(r.Status),
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

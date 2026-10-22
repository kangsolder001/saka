package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kangsolder001/saka/internal/auth"
	"github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/apperr"
	"github.com/kangsolder001/saka/internal/platform/db"
	"github.com/kangsolder001/saka/internal/user"
)

func validRegistration() auth.RegisterInput {
	return auth.RegisterInput{Email: "  Ann@Example.com ", Password: "correct-horse", Name: " Ann ", OrganizationName: "Acme"}
}

func TestRegister_CreatesUserAndQueuesVerificationEmailInOneTransaction(t *testing.T) {
	f := newFixture(t)
	userID := uuid.New()
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.MatchedBy(func(in user.NewUser) bool {
		ok, _, err := f.hasher.Verify("correct-horse", in.PasswordHash)
		return in.Email == "ann@example.com" && in.Name == "Ann" && err == nil && ok
	})).Return(user.User{ID: userID, Email: "ann@example.com", Name: "Ann"}, nil).Once()
	f.prov.EXPECT().ProvisionForNewUser(mock.Anything, mock.Anything, mock.MatchedBy(func(u user.User) bool { return u.ID == userID }), "Acme").
		Return(nil).Once()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, userID, auth.TokenEmailVerification, nil).
		Return("raw+token/1", nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mail.Email{
		Template: "verify_email",
		To:       "ann@example.com",
		Data:     map[string]any{"name": "Ann", "url": "https://app.example.com/verify-email?token=raw%2Btoken%2F1"},
	}).Return(nil).Once()

	got, err := f.svc.Register(context.Background(), validRegistration())

	require.NoError(t, err)
	assert.Equal(t, userID, got.ID)
}

func TestRegister_DoesNotStorePlaintextPassword(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	var stored string
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ db.DBTX, in user.NewUser) (user.User, error) {
			stored = in.PasswordHash
			return user.User{ID: uuid.New(), Email: in.Email, Name: in.Name}, nil
		}).Once()
	f.expectProvision()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("t", nil).Once()
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	_, err := f.svc.Register(context.Background(), validRegistration())

	require.NoError(t, err)
	assert.NotContains(t, stored, "correct-horse")
	assert.Contains(t, stored, "$argon2id$")
}

func TestRegister_InvalidInputFailsBeforeTouchingAnything(t *testing.T) {
	tests := map[string]struct {
		mutate func(in *auth.RegisterInput)
		field  string
	}{
		"short password":        {func(in *auth.RegisterInput) { in.Password = "short" }, "password"},
		"password equals email": {func(in *auth.RegisterInput) { in.Email, in.Password = "ann@example.com", "ann@example.com" }, "password"},
		"blank name":            {func(in *auth.RegisterInput) { in.Name = "   " }, "name"},
		"blank organization":    {func(in *auth.RegisterInput) { in.OrganizationName = "" }, "organization_name"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t) // no expectations: any call to a collaborator fails the test
			in := validRegistration()
			tt.mutate(&in)

			_, err := f.svc.Register(context.Background(), in)

			require.Error(t, err)
			assert.True(t, apperr.IsCode(err, apperr.CodeValidation))
			assert.Contains(t, apperr.From(err).Details["fields"], tt.field)
		})
	}
}

func TestRegister_ReportsEveryInvalidFieldAtOnce(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.Register(context.Background(), auth.RegisterInput{Email: "a@b.co", Password: "x"})

	fields := apperr.From(err).Details["fields"].(map[string]string)
	assert.Contains(t, fields, "password")
	assert.Contains(t, fields, "name")
	assert.Contains(t, fields, "organization_name")
}

func TestRegister_DuplicateEmailIsConflictAndSendsNothing(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		Return(user.User{}, apperr.Conflict("Email sudah terdaftar.")).Once()

	_, err := f.svc.Register(context.Background(), validRegistration())

	assert.True(t, apperr.IsCode(err, apperr.CodeConflict))
	// no Issue / Send expectations: neither may be called after a failed Create
}

func TestRegister_MailFailureFailsTheWholeRegistration(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		Return(user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann"}, nil).Once()
	f.expectProvision()
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("t", nil).Once()
	boom := errors.New("outbox insert failed")
	f.mail.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything).Return(boom).Once()

	_, err := f.svc.Register(context.Background(), validRegistration())

	assert.ErrorIs(t, err, boom, "the error reaches the transaction so the user insert rolls back")
}

func TestRegister_TokenFailureFailsTheWholeRegistration(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		Return(user.User{ID: uuid.New()}, nil).Once()
	f.expectProvision()
	boom := errors.New("token insert failed")
	f.tokens.EXPECT().Issue(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", boom).Once()

	_, err := f.svc.Register(context.Background(), validRegistration())

	assert.ErrorIs(t, err, boom)
}

func (f *fixture) expectProvision() {
	f.prov.EXPECT().ProvisionForNewUser(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
}

func TestRegister_OrganizationFailureFailsTheWholeRegistration(t *testing.T) {
	f := newFixture(t)
	f.expectTx()
	f.users.EXPECT().Create(mock.Anything, mock.Anything, mock.Anything).
		Return(user.User{ID: uuid.New(), Email: "ann@example.com", Name: "Ann"}, nil).Once()
	boom := errors.New("organization insert failed")
	f.prov.EXPECT().ProvisionForNewUser(mock.Anything, mock.Anything, mock.Anything, "Acme").Return(boom).Once()

	_, err := f.svc.Register(context.Background(), validRegistration())

	assert.ErrorIs(t, err, boom, "the error reaches the transaction so the user insert rolls back")
	// no Issue / Send expectations: no email for a registration that failed
}

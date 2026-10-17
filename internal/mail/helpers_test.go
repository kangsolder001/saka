package mail_test

import (
	"context"
	"net/textproto"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// nilTx satisfies db.DBTX without a database: the Service must only pass it on.
type nilTx struct{}

func (nilTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (nilTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (nilTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

// smtpError builds an SMTP reply error, as returned by net/smtp.
func smtpError(code int) error { return &textproto.Error{Code: code, Msg: "smtp reply"} }

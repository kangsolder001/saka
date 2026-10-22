package billing

import (
	"context"
	"fmt"

	"github.com/kangsolder001/saka/internal/platform/db"
)

// FormatInvoiceNumber renders an invoice number: INV-<year>-<sequence padded to
// six digits>, e.g. INV-2026-000123. The sequence is global and never restarts
// with the year, so the number alone identifies the invoice (spec §14 #5); the
// year is only there for the reader. Past 999999 invoices the digits simply
// grow.
func FormatInvoiceNumber(year int, seq int64) string {
	return fmt.Sprintf("INV-%04d-%06d", year, seq)
}

// NextInvoiceNumber takes the next number from the global sequence. It can run
// in any transaction (the sequence is not tenant data). Numbers are never
// reused: if the caller's transaction rolls back, the number is lost and the
// numbering has a gap, which is harmless (a reused number would not be).
func (s *Service) NextInvoiceNumber(ctx context.Context, q db.DBTX) (string, error) {
	seq, err := queries(q).NextInvoiceSequence(ctx)
	if err != nil {
		return "", fmt.Errorf("billing: next invoice number: %w", err)
	}
	return FormatInvoiceNumber(s.clk.Now().Year(), seq), nil
}

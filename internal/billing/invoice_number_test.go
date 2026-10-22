package billing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/billing"
)

func TestFormatInvoiceNumber(t *testing.T) {
	assert.Equal(t, "INV-2026-000001", billing.FormatInvoiceNumber(2026, 1))
	assert.Equal(t, "INV-2026-000123", billing.FormatInvoiceNumber(2026, 123))
	assert.Equal(t, "INV-2027-999999", billing.FormatInvoiceNumber(2027, 999999))
	assert.Equal(t, "INV-2027-1000000", billing.FormatInvoiceNumber(2027, 1000000), "the digits grow past six rather than wrap")
}

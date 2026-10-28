package billing_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kangsolder001/saka/internal/billing"
)

func TestFormatMoney(t *testing.T) {
	assert.Equal(t, "IDR 0", billing.FormatMoney(0, "IDR"))
	assert.Equal(t, "IDR 999", billing.FormatMoney(999, "IDR"))
	assert.Equal(t, "IDR 1,000", billing.FormatMoney(1000, "IDR"))
	assert.Equal(t, "IDR 99,000", billing.FormatMoney(99000, "IDR"))
	assert.Equal(t, "IDR 299,000", billing.FormatMoney(299000, "IDR"))
	assert.Equal(t, "IDR 1,234,567", billing.FormatMoney(1234567, "IDR"))
	assert.Equal(t, "5000 USD", billing.FormatMoney(5000, "USD"))
}

func TestFormatDate_IsEnglishAndInWIB(t *testing.T) {
	assert.Equal(t, "8 June 2026", billing.FormatDate(time.Date(2026, 6, 8, 3, 0, 0, 0, time.UTC)))
	assert.Equal(t, "2 January 2027", billing.FormatDate(time.Date(2027, 1, 1, 18, 0, 0, 0, time.UTC)), "18:00 UTC is already the next day in Jakarta")
	assert.Equal(t, "31 December 2026", billing.FormatDate(time.Date(2026, 12, 31, 16, 59, 0, 0, time.UTC)))
}

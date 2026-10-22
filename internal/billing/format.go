package billing

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// wib is the time zone dates in emails are written in: the customers are in
// Indonesia, and a date in UTC would be a day off for part of the evening.
var wib = time.FixedZone("WIB", 7*60*60)

var monthNames = [...]string{
	"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

// FormatDate writes a date for people, in Indonesian and in WIB: "8 Juni 2026".
func FormatDate(t time.Time) string {
	t = t.In(wib)
	return fmt.Sprintf("%d %s %d", t.Day(), monthNames[t.Month()-1], t.Year())
}

// FormatMoney writes an amount in the smallest unit of its currency for
// people: IDR as "Rp99.000", anything else as "<amount> <CODE>".
func FormatMoney(amount int64, currency string) string {
	if currency != "IDR" {
		return strconv.FormatInt(amount, 10) + " " + currency
	}
	digits := strconv.FormatInt(amount, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return sign + "Rp" + b.String()
}

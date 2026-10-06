package main

import (
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gnolang/gno/gno.land/pkg/gnoland/ugnot"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// Terminal hygiene: every string from the request or the chain goes through
// esc or quote, so an argument carrying ANSI escapes or bidi controls cannot
// redraw the review the user is reading.

// esc escapes control and non-printable characters.
func esc(s string) string {
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}

// escAll escapes each string and joins them with commas.
func escAll(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = esc(s)
	}
	return strings.Join(parts, ", ")
}

// quote is esc in double quotes.
func quote(s string) string { return strconv.Quote(s) }

func formatTime(unix int64) string {
	return time.Unix(unix, 0).UTC().Format("2006-01-02 15:04 UTC")
}

// formatInt groups digits by thousands: 2000000 → 2,000,000.
func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return "-" + groupDigits(s[1:])
	}
	return groupDigits(s)
}

func groupDigits(digits string) string {
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatAmount shows ugnot in GNOT, and other denoms as they are.
func formatAmount(amount *big.Int, denom string) string {
	if denom != ugnot.Denom {
		return amount.String() + esc(denom)
	}
	s := new(big.Rat).SetFrac(amount, big.NewInt(1_000_000)).FloatString(6)
	whole, frac, _ := strings.Cut(strings.TrimRight(strings.TrimRight(s, "0"), "."), ".")
	if frac != "" {
		frac = "." + frac
	}
	if neg := strings.HasPrefix(whole, "-"); neg {
		return "-" + groupDigits(whole[1:]) + frac + " GNOT"
	}
	return groupDigits(whole) + frac + " GNOT"
}

func formatCoin(c std.Coin) string { return formatAmount(big.NewInt(c.Amount), c.Denom) }

func formatCoins(cs std.Coins) string {
	if len(cs) == 0 {
		return "nothing"
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = formatCoin(c)
	}
	return strings.Join(parts, ", ")
}

var units = []struct {
	secs int64
	name string
}{{86400, "day"}, {3600, "hour"}, {60, "minute"}}

// formatDuration names a duration in seconds with an exact count of days or
// hours, or else rounded in the largest unit that fits: 30 days less a
// minute is "about 30 days", not 43,199 minutes.
func formatDuration(secs int64) string {
	for _, u := range units[:2] {
		if secs >= u.secs && secs%u.secs == 0 {
			return plural(secs/u.secs, u.name)
		}
	}
	for _, u := range units {
		if secs >= u.secs {
			if secs%u.secs == 0 {
				return plural(secs/u.secs, u.name)
			}
			return "about " + plural((secs+u.secs/2)/u.secs, u.name)
		}
	}
	return plural(secs, "second")
}

// formatPeriod is formatDuration after "per": "hour", "3 days".
func formatPeriod(secs int64) string {
	d := formatDuration(secs)
	if n, unit, ok := strings.Cut(d, " "); ok && n == "1" {
		return unit
	}
	return d
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return formatInt(n) + " " + unit + "s"
}

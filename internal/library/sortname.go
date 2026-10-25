package library

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// CompareNames orders sound names as the phone's A–Z sorting does
// (Intl.Collator with numeric: true, sensitivity: "base"): ignoring case
// and accents ("Ähm" as "ahm"), with runs of digits compared as numbers, so
// "Clip 2" comes before "Clip 10". Ties keep their order in a stable sort.
func CompareNames(a, b string) int {
	a, b = foldName(a), foldName(b)
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if isDigit(ra) && isDigit(rb) {
			da, db := digits(a), digits(b)
			if c := compareNumbers(da, db); c != 0 {
				return c
			}
			a, b = a[len(da):], b[len(db):]
			continue
		}
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
		a, b = a[na:], b[nb:]
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	}
	return 1
}

// foldName is s in lower case without accents.
func foldName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, norm.NFD.String(s))
}

func isDigit(r rune) bool { return r < utf8.RuneSelf && unicode.IsDigit(r) }

// digits is the run of ASCII digits s starts with.
func digits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

// compareNumbers compares two runs of digits by their value.
func compareNumbers(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

package main

import (
	"strings"
	"unicode"
)

// goName turns an XSD NCName into an exported Go identifier. Letters,
// digits and underscores are kept as they are so that Go names stay
// recognizable next to the schema; '-' and '.' become '_'.
func goName(n string) string {
	var b strings.Builder
	for i, r := range n {
		switch {
		case r == '-' || r == '.':
			b.WriteByte('_')
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" || !unicode.IsLetter([]rune(s)[0]) {
		s = "X" + s
	}
	return s
}

// enumSuffix turns an enumeration value into the identifier part that
// follows the type name and an underscore in a constant name.
func enumSuffix(v string) string {
	var b strings.Builder
	sep := false
	for _, r := range v {
		var w string
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			w = string(r)
		case r == '%':
			w = "pct"
		case r == '+':
			w = "plus"
		case r == '<':
			w = "lt"
		case r == '>':
			w = "gt"
		case r == '=':
			w = "eq"
		}
		if w == "" {
			sep = true
			continue
		}
		if sep && b.Len() > 0 {
			b.WriteByte('_')
		}
		sep = false
		b.WriteString(w)
	}
	if b.Len() == 0 {
		return "Empty"
	}
	return b.String()
}

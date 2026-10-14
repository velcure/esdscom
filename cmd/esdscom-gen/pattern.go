package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// XSD and RE2 differ in a few places that matter for the patterns of the
// schema: XSD patterns are anchored implicitly, '^' and '$' are ordinary
// characters, '.' excludes carriage return as well as newline, and the
// class escapes \d, \w and \s are defined over Unicode.
const (
	xsdDigit    = `\p{Nd}`
	xsdNonWord  = `\p{P}\p{Z}\p{C}`
	xsdSpace    = ` \t\n\r`
	xsdAnyChar  = `[^\n\r]`
	xsdWordAtom = `[^` + xsdNonWord + `]`
)

// translatePattern converts an XSD regular expression into an anchored Go
// regular expression with the same meaning. Constructs whose meaning
// cannot be reproduced exactly are rejected.
func translatePattern(src string) (string, error) {
	t := &patTranslator{src: []rune(src)}
	body, err := t.run()
	if err != nil {
		return "", fmt.Errorf("pattern %q: %w", src, err)
	}
	out := `^(?:` + body + `)$`
	if _, err := regexp.Compile(out); err != nil {
		return "", fmt.Errorf("pattern %q: translated form does not compile: %w", src, err)
	}
	return out, nil
}

type patTranslator struct {
	src []rune
	i   int
}

func (t *patTranslator) run() (string, error) {
	var b strings.Builder
	for t.i < len(t.src) {
		r := t.src[t.i]
		switch r {
		case '\\':
			s, err := t.escape(false)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		case '[':
			s, err := t.class()
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		case '.':
			t.i++
			b.WriteString(xsdAnyChar)
		case '^', '$':
			t.i++
			b.WriteString(`\` + string(r))
		case '(':
			t.i++
			if t.i < len(t.src) && t.src[t.i] == '?' {
				return "", fmt.Errorf("group extension (? is not XSD syntax")
			}
			b.WriteString("(?:")
		case '{':
			end := strings.IndexRune(string(t.src[t.i:]), '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated quantifier")
			}
			q := string(t.src[t.i : t.i+end+1])
			if !regexp.MustCompile(`^\{\d+(,\d*)?\}$`).MatchString(q) {
				return "", fmt.Errorf("bad quantifier %s", q)
			}
			b.WriteString(q)
			t.i += end + 1
		default:
			t.i++
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}

// escape translates a backslash escape. inClass selects the form used
// inside a character class.
func (t *patTranslator) escape(inClass bool) (string, error) {
	if t.i+1 >= len(t.src) {
		return "", fmt.Errorf("trailing backslash")
	}
	c := t.src[t.i+1]
	t.i += 2
	switch c {
	case 'n':
		return `\n`, nil
	case 'r':
		return `\r`, nil
	case 't':
		return `\t`, nil
	case '\\', '|', '.', '-', '^', '?', '*', '+', '{', '}', '(', ')', '[', ']', '$':
		return `\` + string(c), nil
	case 'd':
		return xsdDigit, nil
	case 's':
		if inClass {
			return xsdSpace, nil
		}
		return `[` + xsdSpace + `]`, nil
	case 'w':
		if inClass {
			return "", errWordInClass
		}
		return xsdWordAtom, nil
	case 'D':
		if inClass {
			return "", fmt.Errorf(`\D inside a character class`)
		}
		return `\P{Nd}`, nil
	case 'S':
		if inClass {
			return "", fmt.Errorf(`\S inside a character class`)
		}
		return `[^` + xsdSpace + `]`, nil
	case 'W':
		if inClass {
			return "", fmt.Errorf(`\W inside a character class`)
		}
		return `[` + xsdNonWord + `]`, nil
	case 'p', 'P':
		if t.i >= len(t.src) || t.src[t.i] != '{' {
			return "", fmt.Errorf(`\%c without {`, c)
		}
		end := strings.IndexRune(string(t.src[t.i:]), '}')
		if end < 0 {
			return "", fmt.Errorf(`unterminated \%c{`, c)
		}
		name := string(t.src[t.i+1 : t.i+end])
		t.i += end + 1
		if strings.HasPrefix(name, "Is") {
			return "", fmt.Errorf(`block escape \%c{%s}`, c, name)
		}
		if !regexp.MustCompile(`^(L[ultmo]?|M[nce]?|N[dlo]?|P[cdseifo]?|Z[slp]?|S[mcko]?|C[cfon]?)$`).MatchString(name) {
			return "", fmt.Errorf(`unknown category \%c{%s}`, c, name)
		}
		return `\` + string(c) + `{` + name + `}`, nil
	}
	return "", fmt.Errorf(`unsupported escape \%c`, c)
}

var errWordInClass = errors.New(`\w inside a character class`)

// class translates a character class. A positive class containing \w is
// expressed as an alternation, because RE2 cannot nest the negated class
// that \w stands for.
func (t *patTranslator) class() (string, error) {
	t.i++ // '['
	neg := false
	if t.i < len(t.src) && t.src[t.i] == '^' {
		neg = true
		t.i++
	}
	var b strings.Builder
	word := false
	first := true
	for {
		if t.i >= len(t.src) {
			return "", fmt.Errorf("unterminated character class")
		}
		r := t.src[t.i]
		switch {
		case r == ']' && !first:
			t.i++
			body := b.String()
			pre := "["
			if neg {
				pre = "[^"
			}
			switch {
			case word && neg:
				return "", fmt.Errorf(`\w inside a negated character class`)
			case word && body == "":
				return xsdWordAtom, nil
			case word:
				return `(?:` + pre + body + `]|` + xsdWordAtom + `)`, nil
			}
			return pre + body + "]", nil
		case r == '[':
			return "", fmt.Errorf("nested character class or class subtraction")
		case r == '-' && t.i+1 < len(t.src) && t.src[t.i+1] == '[':
			return "", fmt.Errorf("character class subtraction")
		case r == '\\':
			s, err := t.escape(true)
			if errors.Is(err, errWordInClass) {
				word = true
				break
			}
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		case r == '-':
			t.i++
			b.WriteString(`\-`)
			if !first && t.i < len(t.src) && t.src[t.i] != ']' {
				// A '-' between two characters is a range operator.
				s := b.String()
				b.Reset()
				b.WriteString(strings.TrimSuffix(s, `\-`) + "-")
			}
		default:
			t.i++
			if r == '^' {
				b.WriteString(`\^`)
			} else {
				b.WriteRune(r)
			}
		}
		first = false
	}
}

package esdscom

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// primitive is the built-in type a simple type is ultimately derived from.
type primitive uint8

const (
	primString primitive = iota + 1
	primBoolean
	primInteger
	primInt
	primFloat
	primDecimal
	primDate
	primDateTime
	primAnyURI
)

var primNames = map[primitive]string{
	primString: "string", primBoolean: "boolean", primInteger: "integer", primInt: "int",
	primFloat: "float", primDecimal: "decimal", primDate: "date", primDateTime: "dateTime",
	primAnyURI: "anyURI",
}

type wsMode uint8

const (
	wsUnset wsMode = iota
	wsPreserve
	wsReplace
	wsCollapse
)

// simpleType is the runtime description of a simple type: a restriction
// of a base type by facets, or a union of member types.
type simpleType struct {
	name    string
	prim    primitive
	base    *simpleType
	members []*simpleType
	facets  []facet
}

type facetKind uint8

const (
	fWhiteSpace facetKind = iota + 1
	fLength
	fMinLength
	fMaxLength
	fMinInclusive
	fMaxInclusive
	fTotalDigits
	fFractionDigits
	fPattern
	fEnumeration
)

type facet struct {
	kind     facetKind
	n        int
	value    string
	ws       wsMode
	patterns []*regexp.Regexp
	sources  []string
	enum     map[string]struct{}
}

func whiteSpace(m wsMode) facet   { return facet{kind: fWhiteSpace, ws: m} }
func length(n int) facet          { return facet{kind: fLength, n: n} }
func minLength(n int) facet       { return facet{kind: fMinLength, n: n} }
func maxLength(n int) facet       { return facet{kind: fMaxLength, n: n} }
func minInclusive(v string) facet { return facet{kind: fMinInclusive, value: v} }
func maxInclusive(v string) facet { return facet{kind: fMaxInclusive, value: v} }
func totalDigits(n int) facet     { return facet{kind: fTotalDigits, n: n} }
func fractionDigits(n int) facet  { return facet{kind: fFractionDigits, n: n} }

// pattern takes pairs of the XSD source of a pattern and its translation
// to an anchored Go regular expression.
func pattern(pairs ...string) facet {
	f := facet{kind: fPattern}
	for i := 0; i+1 < len(pairs); i += 2 {
		f.sources = append(f.sources, pairs[i])
		f.patterns = append(f.patterns, regexp.MustCompile(pairs[i+1]))
	}
	return f
}

func enumeration(values []string) facet {
	f := facet{kind: fEnumeration, enum: make(map[string]struct{}, len(values))}
	for _, v := range values {
		f.enum[v] = struct{}{}
	}
	return f
}

// Built-in types.
var (
	xsString   = &simpleType{name: "string", prim: primString}
	xsBoolean  = &simpleType{name: "boolean", prim: primBoolean}
	xsInteger  = &simpleType{name: "integer", prim: primInteger}
	xsInt      = &simpleType{name: "int", prim: primInt}
	xsFloat    = &simpleType{name: "float", prim: primFloat}
	xsDecimal  = &simpleType{name: "decimal", prim: primDecimal}
	xsDate     = &simpleType{name: "date", prim: primDate}
	xsDateTime = &simpleType{name: "dateTime", prim: primDateTime}
	xsAnyURI   = &simpleType{name: "anyURI", prim: primAnyURI}
)

// violation is a failed constraint of a simple value.
type violation struct {
	rule string
	msg  string
}

func (t *simpleType) valid(v string) bool { return t.check(v) == nil }

// whitespace returns the whitespace handling of t: its own facet, or that
// of its base; built-in types other than string and anyURI collapse.
func (t *simpleType) whitespace() wsMode {
	for s := t; s != nil; s = s.base {
		for _, f := range s.facets {
			if f.kind == fWhiteSpace {
				return f.ws
			}
		}
		if s.base == nil && len(s.members) == 0 {
			if s.prim == primString {
				return wsPreserve
			}
			return wsCollapse
		}
	}
	return wsCollapse
}

func normalize(v string, m wsMode) string {
	switch m {
	case wsReplace:
		return strings.Map(func(r rune) rune {
			if r == '\t' || r == '\n' || r == '\r' {
				return ' '
			}
			return r
		}, v)
	case wsCollapse:
		return collapse(v)
	}
	return v
}

func collapse(v string) string {
	if !strings.ContainsAny(v, " \t\n\r") {
		return v
	}
	return strings.Join(strings.FieldsFunc(v, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}), " ")
}

// check returns the first violated constraint of v, or nil.
func (t *simpleType) check(v string) *violation {
	if len(t.members) > 0 {
		return t.checkUnion(v)
	}
	return t.checkNormalized(normalize(v, t.whitespace()))
}

func (t *simpleType) checkUnion(v string) *violation {
	allEnum := true
	for _, m := range t.members {
		x := m.check(v)
		if x == nil {
			return nil
		}
		if x.rule != "enum" {
			allEnum = false
		}
	}
	if allEnum {
		return &violation{"enum", fmt.Sprintf("%q is not an allowed value of %s", v, t.name)}
	}
	return &violation{"union", fmt.Sprintf("%q matches no member type of %s", v, t.name)}
}

func (t *simpleType) checkNormalized(v string) *violation {
	switch {
	case len(t.members) > 0:
		if x := t.checkUnion(v); x != nil {
			return x
		}
	case t.base != nil:
		if x := t.base.checkNormalized(v); x != nil {
			return x
		}
	default:
		if !lexicalOK(t.prim, v) {
			return &violation{"type", fmt.Sprintf("%q is not a valid xs:%s", v, primNames[t.prim])}
		}
	}
	for i := range t.facets {
		if x := t.facets[i].check(t, v); x != nil {
			return x
		}
	}
	return nil
}

func lexicalOK(p primitive, v string) bool {
	var err error
	switch p {
	case primBoolean:
		_, err = parseBool(v)
	case primInteger:
		return reInteger.MatchString(v)
	case primInt:
		_, err = parseInt(v)
		if err == nil {
			n, _ := parseInt(v)
			return n >= -1<<31 && n < 1<<31
		}
	case primFloat:
		return reFloat.MatchString(v)
	case primDecimal:
		return reDecimal.MatchString(v)
	case primDate:
		_, err = parseDate(v)
	case primDateTime:
		_, err = parseDateTime(v)
	}
	return err == nil
}

func (f *facet) check(t *simpleType, v string) *violation {
	switch f.kind {
	case fLength, fMinLength, fMaxLength:
		n := utf8.RuneCountInString(v)
		switch {
		case f.kind == fLength && n != f.n:
			return &violation{"length", fmt.Sprintf("length %d of %s must be %d", n, t.name, f.n)}
		case f.kind == fMinLength && n < f.n:
			return &violation{"minLength", fmt.Sprintf("length %d of %s is below %d", n, t.name, f.n)}
		case f.kind == fMaxLength && n > f.n:
			return &violation{"maxLength", fmt.Sprintf("length %d of %s exceeds %d", n, t.name, f.n)}
		}
	case fMinInclusive, fMaxInclusive:
		c, ok := compareNumber(t.prim, v, f.value)
		if f.kind == fMinInclusive && (!ok || c < 0) {
			return &violation{"minInclusive", fmt.Sprintf("%s of %s is below %s", v, t.name, f.value)}
		}
		if f.kind == fMaxInclusive && (!ok || c > 0) {
			return &violation{"maxInclusive", fmt.Sprintf("%s of %s exceeds %s", v, t.name, f.value)}
		}
	case fTotalDigits, fFractionDigits:
		total, frac := digits(v)
		if f.kind == fTotalDigits && total > f.n {
			return &violation{"totalDigits", fmt.Sprintf("%s of %s has more than %d digits", v, t.name, f.n)}
		}
		if f.kind == fFractionDigits && frac > f.n {
			return &violation{"fractionDigits", fmt.Sprintf("%s of %s has more than %d fraction digits", v, t.name, f.n)}
		}
	case fPattern:
		for _, re := range f.patterns {
			if re.MatchString(v) {
				return nil
			}
		}
		return &violation{"pattern", fmt.Sprintf("%q does not match the pattern %s of %s", v, strings.Join(f.sources, " | "), t.name)}
	case fEnumeration:
		if _, ok := f.enum[v]; !ok {
			return &violation{"enum", fmt.Sprintf("%q is not an allowed value of %s", v, t.name)}
		}
	}
	return nil
}

// compareNumber compares two values of a numeric primitive. It reports
// false when a value cannot be compared, such as NaN.
func compareNumber(p primitive, a, b string) (int, bool) {
	if p == primFloat {
		x, err1 := parseFloat(a)
		y, err2 := parseFloat(b)
		if err1 != nil || err2 != nil || x != x || y != y {
			return 0, false
		}
		switch {
		case x < y:
			return -1, true
		case x > y:
			return 1, true
		}
		return 0, true
	}
	x, ok1 := parseRat(a)
	y, ok2 := parseRat(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	return x.Cmp(y), true
}

// digits counts the significant digits and the fraction digits of a
// decimal lexical value.
func digits(v string) (total, frac int) {
	v = strings.TrimLeft(v, "+-")
	intPart, fracPart, _ := strings.Cut(v, ".")
	intPart = strings.TrimLeft(intPart, "0")
	fracPart = strings.TrimRight(fracPart, "0")
	return len(intPart) + len(fracPart), len(fracPart)
}

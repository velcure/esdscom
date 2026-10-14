package esdscom

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Boolean is a value of xs:boolean in its lexical form.
type Boolean string

// Integer is a value of xs:integer in its lexical form.
type Integer string

// Int is a value of xs:int in its lexical form.
type Int string

// Float is a value of xs:float in its lexical form.
type Float string

// Decimal is a value of xs:decimal in its lexical form.
type Decimal string

// Date is a value of xs:date in its lexical form.
type Date string

// DateTime is a value of xs:dateTime in its lexical form.
type DateTime string

// AnyURI is a value of xs:anyURI.
type AnyURI string

// Simple values keep their lexical form, so that decoding never fails on
// a malformed value and Validate can report it with its path. The methods
// below convert to Go values.

// Bool returns the value of b.
func (b Boolean) Bool() (bool, error) { return parseBool(string(b)) }

// Valid reports whether b is a valid xs:boolean.
func (b Boolean) Valid() bool { return xsBoolean.valid(string(b)) }

// Int64 returns the value of i.
func (i Integer) Int64() (int64, error) { return parseInt(string(i)) }

// Valid reports whether i is a valid xs:integer.
func (i Integer) Valid() bool { return xsInteger.valid(string(i)) }

// Int64 returns the value of i.
func (i Int) Int64() (int64, error) { return parseInt(string(i)) }

// Valid reports whether i is a valid xs:int.
func (i Int) Valid() bool { return xsInt.valid(string(i)) }

// Float64 returns the value of f.
func (f Float) Float64() (float64, error) { return parseFloat(string(f)) }

// Valid reports whether f is a valid xs:float.
func (f Float) Valid() bool { return xsFloat.valid(string(f)) }

// Float64 returns the value of d, rounded to the nearest float64.
func (d Decimal) Float64() (float64, error) { return parseDecimalFloat(string(d)) }

// Valid reports whether d is a valid xs:decimal.
func (d Decimal) Valid() bool { return xsDecimal.valid(string(d)) }

// Time returns the date as midnight of that day, in the time zone given in
// the value or UTC if it has none.
func (d Date) Time() (time.Time, error) { return parseDate(string(d)) }

// Valid reports whether d is a valid xs:date.
func (d Date) Valid() bool { return xsDate.valid(string(d)) }

// Time returns the value of d, in UTC if it has no time zone.
func (d DateTime) Time() (time.Time, error) { return parseDateTime(string(d)) }

// Valid reports whether d is a valid xs:dateTime.
func (d DateTime) Valid() bool { return xsDateTime.valid(string(d)) }

// Valid reports whether u is a valid xs:anyURI.
func (u AnyURI) Valid() bool { return xsAnyURI.valid(string(u)) }

// BooleanOf returns the lexical form of b.
func BooleanOf(b bool) Boolean { return Boolean(strconv.FormatBool(b)) }

// IntegerOf returns the lexical form of i.
func IntegerOf(i int64) Integer { return Integer(strconv.FormatInt(i, 10)) }

// FloatOf returns the lexical form of f.
func FloatOf(f float64) Float {
	switch {
	case math.IsInf(f, 1):
		return "INF"
	case math.IsInf(f, -1):
		return "-INF"
	case math.IsNaN(f):
		return "NaN"
	}
	return Float(strconv.FormatFloat(f, 'g', -1, 32))
}

// DateOf returns the date of t without a time zone.
func DateOf(t time.Time) Date { return Date(t.Format("2006-01-02")) }

// DateTimeOf returns the lexical form of t with its time zone.
func DateTimeOf(t time.Time) DateTime { return DateTime(t.Format(time.RFC3339Nano)) }

// Ptr returns a pointer to v. The generated types use pointers for
// elements that occur at most once, so that an absent element can be told
// apart from an empty one.
func Ptr[T any](v T) *T { return &v }

var (
	reInteger  = regexp.MustCompile(`^[+-]?[0-9]+$`)
	reDecimal  = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)$`)
	reFloat    = regexp.MustCompile(`^([+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?|[+-]?INF|NaN)$`)
	reDate     = regexp.MustCompile(`^(-?[0-9]{4,})-([0-9]{2})-([0-9]{2})(Z|[+-][0-9]{2}:[0-9]{2})?$`)
	reDateTime = regexp.MustCompile(`^(-?[0-9]{4,})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})?$`)
)

var errLexical = errors.New("invalid lexical form")

func parseBool(s string) (bool, error) {
	switch collapse(s) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	}
	return false, errLexical
}

func parseInt(s string) (int64, error) {
	s = collapse(s)
	if !reInteger.MatchString(s) {
		return 0, errLexical
	}
	return strconv.ParseInt(s, 10, 64)
}

func parseFloat(s string) (float64, error) {
	s = collapse(s)
	switch s {
	case "INF", "+INF":
		return math.Inf(1), nil
	case "-INF":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}
	if !reFloat.MatchString(s) {
		return 0, errLexical
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, err
	}
	return f, nil
}

func parseDecimalFloat(s string) (float64, error) {
	s = collapse(s)
	if !reDecimal.MatchString(s) {
		return 0, errLexical
	}
	return strconv.ParseFloat(s, 64)
}

func parseRat(s string) (*big.Rat, bool) {
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}

func parseDate(s string) (time.Time, error) {
	s = collapse(s)
	m := reDate.FindStringSubmatch(s)
	if m == nil || !validDate(m[1], m[2], m[3]) || !validZone(m[4]) {
		return time.Time{}, errLexical
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, zone(m[4])), nil
}

func parseDateTime(s string) (time.Time, error) {
	s = collapse(s)
	m := reDateTime.FindStringSubmatch(s)
	if m == nil || !validDate(m[1], m[2], m[3]) || !validZone(m[8]) {
		return time.Time{}, errLexical
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	h, _ := strconv.Atoi(m[4])
	mi, _ := strconv.Atoi(m[5])
	sec, _ := strconv.Atoi(m[6])
	if mi > 59 || sec > 59 || h > 24 || h == 24 && (mi != 0 || sec != 0 || strings.Trim(m[7], ".0") != "") {
		return time.Time{}, errLexical
	}
	var ns int
	if m[7] != "" {
		frac := (m[7][1:] + "000000000")[:9]
		ns, _ = strconv.Atoi(frac)
	}
	return time.Date(y, time.Month(mo), d, h, mi, sec, ns, zone(m[8])), nil
}

func validDate(ys, ms, ds string) bool {
	y, err := strconv.Atoi(ys)
	if err != nil || y == 0 || len(strings.TrimPrefix(ys, "-")) > 4 && strings.HasPrefix(strings.TrimPrefix(ys, "-"), "0") {
		return false
	}
	m, _ := strconv.Atoi(ms)
	d, _ := strconv.Atoi(ds)
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[m-1]
	if m == 2 && (y%4 == 0 && y%100 != 0 || y%400 == 0) {
		days = 29
	}
	return d <= days
}

func validZone(z string) bool {
	if z == "" || z == "Z" {
		return true
	}
	h, _ := strconv.Atoi(z[1:3])
	m, _ := strconv.Atoi(z[4:6])
	return m <= 59 && (h < 14 || h == 14 && m == 0)
}

func zone(z string) *time.Location {
	if z == "" || z == "Z" {
		return time.UTC
	}
	h, _ := strconv.Atoi(z[1:3])
	m, _ := strconv.Atoi(z[4:6])
	off := h*3600 + m*60
	if z[0] == '-' {
		off = -off
	}
	return time.FixedZone(z, off)
}

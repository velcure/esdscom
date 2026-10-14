package esdscom

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestSimpleTypes(t *testing.T) {
	tests := []struct {
		typ  *simpleType
		v    string
		rule string // empty when the value is valid
	}{
		{xsString, " any text ", ""},
		{xsBoolean, "true", ""},
		{xsBoolean, " 0 ", ""},
		{xsBoolean, "yes", "type"},
		{xsInteger, "+12", ""},
		{xsInteger, "1.0", "type"},
		{xsInt, "2147483647", ""},
		{xsInt, "-2147483648", ""},
		{xsInt, "2147483648", "type"},
		{xsInt, "x", "type"},
		{xsFloat, "1e3", ""},
		{xsFloat, ".5", ""},
		{xsFloat, "1.", ""},
		{xsFloat, "-INF", ""},
		{xsFloat, "NaN", ""},
		{xsFloat, "1,5", "type"},
		{xsFloat, "inf", "type"},
		{xsFloat, "", "type"},
		{xsDecimal, "-1.50", ""},
		{xsDecimal, "1e3", "type"},
		{xsDate, "2024-02-29", ""},
		{xsDate, "2023-02-29", "type"},
		{xsDate, "2024-13-01", "type"},
		{xsDate, "0000-01-01", "type"},
		{xsDate, "2024-01-01Z", ""},
		{xsDate, "2024-01-01+14:00", ""},
		{xsDate, "2024-01-01+15:00", "type"},
		{xsDateTime, "2024-01-01T10:00:00", ""},
		{xsDateTime, "2024-01-01T10:00:00.125-05:30", ""},
		{xsDateTime, "2024-01-01T24:00:00", ""},
		{xsDateTime, "2024-01-01T24:00:01", "type"},
		{xsDateTime, "2024-01-01T10:60:00", "type"},
		{xsDateTime, "2024-01-01", "type"},
		{xsAnyURI, "https://example.org/a b", ""},
		{stCasNo, "67-63-0", ""},
		{stCasNo, "67-63-X", "pattern"},
		{stCasNo, "67-63-0 ", "pattern"},
		{stString64, strings.Repeat("ä", 64), ""},
		{stString64, strings.Repeat("ä", 65), "maxLength"},
		{stUUID, strings.Repeat("a", 31), "minLength"},
		{stPercentage, "100", ""},
		{stPercentage, "1e2", ""},
		{stPercentage, "100.5", "maxInclusive"},
		{stPercentage, "-1", "minInclusive"},
		{stPercentage, "NaN", "minInclusive"},
		{stPercentage, "abc", "type"},
		{stEffectivenessValue, "99.125", ""},
		{stEffectivenessValue, "99.1250", ""},
		{stEffectivenessValue, "1.2345", "fractionDigits"},
		{stEffectivenessValue, "1000.5", "maxInclusive"},
		{stUfi, " ABCD-1234-EFGH-5678\n", ""},
		{stUfi, "ABCD-1234-EFGH", "pattern"},
		{stEmailAddress, "first.last@example.org", ""},
		{stEmailAddress, "aä.öz@example.org", ""},
		{stEmailAddress, "no-at-sign", "pattern"},
		{stGhs07HazardEnum, "225", ""},
		{stGhs07HazardEnum, "999", "enum"},
		{stCountryCodeEnum, "DE", ""},
		{stCountryCodeEnum, " DE", "enum"},
	}
	for _, tt := range tests {
		x := tt.typ.check(tt.v)
		switch {
		case tt.rule == "" && x != nil:
			t.Errorf("%s %q: unexpected %s: %s", tt.typ.name, tt.v, x.rule, x.msg)
		case tt.rule != "" && x == nil:
			t.Errorf("%s %q: valid, want %s", tt.typ.name, tt.v, tt.rule)
		case tt.rule != "" && x.rule != tt.rule:
			t.Errorf("%s %q: rule %s (%s), want %s", tt.typ.name, tt.v, x.rule, x.msg, tt.rule)
		}
	}
}

func TestTotalDigits(t *testing.T) {
	st := &simpleType{name: "d", prim: primDecimal, base: xsDecimal, facets: []facet{totalDigits(3)}}
	if x := st.check("12.34"); x == nil || x.rule != "totalDigits" {
		t.Fatalf("12.34: %v", x)
	}
	if x := st.check("-001.20"); x != nil {
		t.Fatalf("-001.20: %v", x)
	}
	fixed := &simpleType{name: "f", prim: primString, base: xsString, facets: []facet{length(2), whiteSpace(wsReplace)}}
	if x := fixed.check("a\tb"); x == nil || x.rule != "length" {
		t.Fatalf("length: %v", x)
	}
	if x := fixed.check("a\t"); x != nil {
		t.Fatalf("a<TAB>: %v", x)
	}
}

func TestAccessors(t *testing.T) {
	if b, err := Boolean(" 1").Bool(); err != nil || !b {
		t.Errorf("Bool: %v %v", b, err)
	}
	if _, err := Boolean("yes").Bool(); err == nil {
		t.Error("Bool accepted yes")
	}
	if i, err := Integer("-42").Int64(); err != nil || i != -42 {
		t.Errorf("Integer: %v %v", i, err)
	}
	if _, err := Int("4x").Int64(); err == nil {
		t.Error("Int accepted 4x")
	}
	if f, err := Percentage("12.5").Float64(); err != nil || f != 12.5 {
		t.Errorf("Percentage: %v %v", f, err)
	}
	if f, err := Float("-INF").Float64(); err != nil || !math.IsInf(f, -1) {
		t.Errorf("Float -INF: %v %v", f, err)
	}
	if f, err := Float("NaN").Float64(); err != nil || !math.IsNaN(f) {
		t.Errorf("Float NaN: %v %v", f, err)
	}
	if f, err := Float("1e999").Float64(); err != nil || !math.IsInf(f, 1) {
		t.Errorf("Float overflow: %v %v", f, err)
	}
	if _, err := Float("x").Float64(); err == nil {
		t.Error("Float accepted x")
	}
	if d, err := Decimal("2.50").Float64(); err != nil || d != 2.5 {
		t.Errorf("Decimal: %v %v", d, err)
	}
	if _, err := Decimal("2e5").Float64(); err == nil {
		t.Error("Decimal accepted 2e5")
	}
	d, err := Date("2026-10-14+02:00").Time()
	if err != nil || d.Year() != 2026 || d.Month() != time.October || d.Day() != 14 {
		t.Errorf("Date: %v %v", d, err)
	}
	if _, offset := d.Zone(); offset != 7200 {
		t.Errorf("Date zone offset %d", offset)
	}
	dt, err := DateTime("2026-10-14T08:30:15.5Z").Time()
	if err != nil || dt.Hour() != 8 || dt.Nanosecond() != 500000000 || dt.Location() != time.UTC {
		t.Errorf("DateTime: %v %v", dt, err)
	}
	if dt, err := DateTime("2026-10-14T08:30:15-01:00").Time(); err != nil || dt.UTC().Hour() != 9 {
		t.Errorf("DateTime zone: %v %v", dt, err)
	}
	if _, err := Date("2026-10-32").Time(); err == nil {
		t.Error("Date accepted day 32")
	}
	if _, err := DateTime("2026-10-14").Time(); err == nil {
		t.Error("DateTime accepted a date")
	}
	ts := time.Date(2026, 10, 14, 8, 30, 0, 0, time.UTC)
	if got := DateOf(ts); got != "2026-10-14" || !got.Valid() {
		t.Errorf("DateOf = %q", got)
	}
	if got := DateTimeOf(ts); got != "2026-10-14T08:30:00Z" || !got.Valid() {
		t.Errorf("DateTimeOf = %q", got)
	}
	for f, want := range map[float64]Float{1.5: "1.5", math.Inf(1): "INF", math.Inf(-1): "-INF", math.NaN(): "NaN"} {
		if got := FloatOf(f); got != want || !got.Valid() {
			t.Errorf("FloatOf(%v) = %q", f, got)
		}
	}
	if got := BooleanOf(true); got != "true" || !got.Valid() {
		t.Errorf("BooleanOf = %q", got)
	}
	if got := IntegerOf(-7); got != "-7" || !got.Valid() || !Int(got).Valid() {
		t.Errorf("IntegerOf = %q", got)
	}
	if !Decimal("1.0").Valid() || !AnyURI("x").Valid() {
		t.Error("Valid of built-in types")
	}
	if !CountryCodeEnum_DE.Valid() || CountryCodeEnum("XX").Valid() {
		t.Error("Valid of an enumeration")
	}
	if !Ghs07HazardEnum("225").Valid() {
		t.Error("Valid of a union")
	}
}

func TestErrorStrings(t *testing.T) {
	v := ValidationErrors{
		{Path: "a", Rule: "required", Msg: "a is required"},
		{Path: "b", Rule: "unknown", Msg: "b is unknown", Severity: SeverityHint},
	}
	if got := v.Error(); got != "error: a: a is required (required)\nhint: b: b is unknown (unknown)" {
		t.Errorf("Error() = %q", got)
	}
	if len(v.Errors()) != 1 || len(v.Hints()) != 1 || v.Valid() {
		t.Error("filters")
	}
	if !v.Hints().Valid() {
		t.Error("hints only must be valid")
	}
	if got := (&UnknownError{Path: "x/y", What: "element"}).Error(); got != "esdscom: unknown element at x/y" {
		t.Errorf("UnknownError = %q", got)
	}
}

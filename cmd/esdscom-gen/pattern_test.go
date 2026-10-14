package main

import (
	"regexp"
	"testing"
)

func TestTranslatePattern(t *testing.T) {
	tests := []struct {
		xsd     string
		match   []string
		nomatch []string
	}{
		{`\d{2,7}-\d{2}-\d{1}`, []string{"67-63-0", "٦٧-٦٣-٠"}, []string{"67-63-0x", "a67-63-0", "6-63-0"}},
		{`[0-9]{3}-[0-9]{3}-[0-9]{2}-[0-9,X]`, []string{"123-456-78-X", "123-456-78-,"}, []string{"123-456-78-Y"}},
		{`F-\p{Lu}, S-\p{Lu}`, []string{"F-A, S-Ä"}, []string{"F-a, S-B"}},
		{`([0-9a-zA-Z]([-.\w]*[0-9a-zA-Z])*@([0-9a-zA-Z][-\w]*[0-9a-zA-Z]\.)+[a-zA-Z]{2,9})`, []string{"a.b-cd@ex-ample.org", "aäz@x1.de"}, []string{"a@b", "a b@c.de", "a_b@c.de"}},
		{`P0[0-3][0-9]|AISE[0-1][0-9]`, []string{"P012", "AISE19"}, []string{"P012AISE19", "P042"}},
		{`10{0,8}`, []string{"1", "100000000"}, []string{"1000000000"}},
		{`a.c`, []string{"abc", "a c"}, []string{"a\nc", "a\rc"}},
		{`^a$`, []string{"^a$"}, []string{"a"}},
		{`\s\S\w\W\D`, []string{" xa-x"}, []string{"xxa-x"}},
		{`[\s\d]+`, []string{" 1\t2"}, []string{"a"}},
		{`[^a-c]`, []string{"d"}, []string{"a"}},
		{`[\w]`, []string{"x"}, []string{"-"}},
		{`[a\-\^]+`, []string{"a-^"}, []string{"b"}},
		{`\n\r\t\\\|\.\?\*\+\{\}\(\)\[\]`, []string{"\n\r\t\\|.?*+{}()[]"}, nil},
		{`\P{L}`, []string{"1"}, []string{"a"}},
		{`[-a]`, []string{"-"}, []string{"b"}},
		{`[a-]`, []string{"-"}, []string{"b"}},
	}
	for _, tt := range tests {
		re, err := translatePattern(tt.xsd)
		if err != nil {
			t.Errorf("%s: %v", tt.xsd, err)
			continue
		}
		r := regexp.MustCompile(re)
		for _, s := range tt.match {
			if !r.MatchString(s) {
				t.Errorf("%s (%s) does not match %q", tt.xsd, re, s)
			}
		}
		for _, s := range tt.nomatch {
			if r.MatchString(s) {
				t.Errorf("%s (%s) matches %q", tt.xsd, re, s)
			}
		}
	}
}

func TestTranslatePatternRejects(t *testing.T) {
	for _, src := range []string{
		`\p{IsBasicLatin}`, `\p{Xx}`, `\p`, `\p{L`, `\i`, `\c`, `a\`, `[a-z-[aeiou]]`, `[[a]]`, `[a`,
		`(?:a)`, `a{2`, `a{x}`, `[^\w]`, `[\D]`, `[\S]`, `[\W]`,
	} {
		if re, err := translatePattern(src); err == nil {
			t.Errorf("%s accepted as %s", src, re)
		}
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string]string{"string128": "String128", "a-b.c": "A_b_c", "_x": "X_x", "ClpClassification_2023_707": "ClpClassification_2023_707", "1a": "X1a"} {
		if got := goName(in); got != want {
			t.Errorf("goName(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Weight-%": "Weight_pct", "T 1": "T_1", "": "Empty", "a<b>=c+": "altbgteqcplus", "Hautätzend": "Hautätzend", " x ": "x"} {
		if got := enumSuffix(in); got != want {
			t.Errorf("enumSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

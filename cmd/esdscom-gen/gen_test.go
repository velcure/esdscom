package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/velcure/esdscom/internal/xsd"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// render generates the files for schema and joins them into one text with
// a "-- name --" line before each file.
func render(t *testing.T, schema string, exts ...string) []byte {
	t.Helper()
	files, _, err := generate(schema, Config{
		Package:    "golden",
		Root:       "Root",
		Extensions: exts,
		Source:     filepath.ToSlash(schema),
	})
	if err != nil {
		t.Fatalf("generate %s: %v", schema, err)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	for _, n := range names {
		b.WriteString("-- " + n + " --\n")
		b.Write(files[n])
	}
	return b.Bytes()
}

// TestGolden generates one small schema per supported construct and
// compares the result with the golden file next to it. Run with -update
// to accept a change.
func TestGolden(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		exts   []string
	}{
		{"sequence", "sequence.xsd", nil},
		{"choice", "choice.xsd", nil},
		{"group", "group.xsd", nil},
		{"extension", "extension.xsd", nil},
		{"restriction", "restriction.xsd", nil},
		{"union", "union.xsd", nil},
		{"attribute", "attribute.xsd", nil},
		{"include", "include/main.xsd", []string{"BB"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := render(t, filepath.Join("testdata", "golden", c.schema), c.exts...)
			golden := filepath.Join("testdata", "golden", c.name+".golden")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output differs from %s (run with -update to accept):\n%s", golden, got)
			}
		})
	}
}

// TestDeterministic generates the real schema twice and compares the
// result with the committed files of the package.
func TestDeterministic(t *testing.T) {
	cfg := Config{Package: "esdscom", Root: "DatasheetFeed", Extensions: []string{"EU", "DE"}, Source: "schema/v5.5/SDSComXML.xsd"}
	schema := filepath.Join("..", "..", "schema", "v5.5", "SDSComXML.xsd")
	first, _, err := generate(schema, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := generate(schema, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("file sets differ: %d and %d", len(first), len(second))
	}
	for name, src := range first {
		if !bytes.Equal(src, second[name]) {
			t.Fatalf("%s differs between two runs", name)
		}
		committed, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(src, committed) {
			t.Fatalf("%s is not up to date; run go generate ./...", name)
		}
	}
	for _, want := range []string{"types_gen.go", "types_eu_gen.go", "types_de_gen.go", "validate_gen.go"} {
		if first[want] == nil {
			t.Fatalf("%s not generated", want)
		}
	}
}

const head = `<?xml version="1.0"?>
<xs:schema targetNamespace="urn:test" xmlns="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema">`

// TestUnsupported checks that every construct outside the supported
// subset stops the generator.
func TestUnsupported(t *testing.T) {
	cases := map[string]string{
		"any":               `<xs:element name="Root"><xs:complexType><xs:sequence><xs:any/></xs:sequence></xs:complexType></xs:element>`,
		"substitutionGroup": `<xs:element name="Root" type="string"/><xs:element name="Sub" type="string" substitutionGroup="t:Root"/>`,
		"abstract":          `<xs:element name="Root" type="t:A"/><xs:complexType name="A" abstract="true"/>`,
		"key":               `<xs:element name="Root"><xs:complexType/><xs:key name="k"><xs:selector xpath="."/><xs:field xpath="@a"/></xs:key></xs:element>`,
		"unique":            `<xs:element name="Root"><xs:complexType/><xs:unique name="k"><xs:selector xpath="."/><xs:field xpath="@a"/></xs:unique></xs:element>`,
		"redefine":          `<xs:redefine schemaLocation="x.xsd"/>`,
		"override":          `<xs:override schemaLocation="x.xsd"/>`,
		"import":            `<xs:import namespace="urn:other"/>`,
		"assert":            `<xs:complexType name="A"><xs:assert test="true()"/></xs:complexType>`,
		"mixed":             `<xs:complexType name="A" mixed="true"/>`,
		"element ref":       `<xs:complexType name="A"><xs:sequence><xs:element ref="t:Root"/></xs:sequence></xs:complexType>`,
		"all":               `<xs:complexType name="A"><xs:all><xs:element name="B" type="string"/></xs:all></xs:complexType>`,
		"list":              `<xs:simpleType name="A"><xs:list itemType="string"/></xs:simpleType>`,
		"content restrict":  `<xs:complexType name="A"><xs:complexContent><xs:restriction base="t:B"/></xs:complexContent></xs:complexType>`,
		"attribute use":     `<xs:complexType name="A"><xs:attribute name="a" type="string" use="required"/></xs:complexType>`,
		"attribute fixed":   `<xs:complexType name="A"><xs:attribute name="a" type="string" fixed="x"/></xs:complexType>`,
		"nillable":          `<xs:element name="Root" type="string" nillable="true"/>`,
		"element default":   `<xs:element name="Root" type="string" default="x"/>`,
		"anyAttribute":      `<xs:complexType name="A"><xs:anyAttribute/></xs:complexType>`,
		"maxExclusive":      `<xs:simpleType name="A"><xs:restriction base="xs:integer"><xs:maxExclusive value="1"/></xs:restriction></xs:simpleType>`,
		"facet fixed":       `<xs:simpleType name="A"><xs:restriction base="string"><xs:maxLength value="1" fixed="true"/></xs:restriction></xs:simpleType>`,
		"builtin type":      `<xs:element name="Root" type="xs:token"/>`,
		"foreign element":   `<xs:element name="Root" type="string"><x:a xmlns:x="urn:x"/></xs:element>`,
		"text":              `<xs:element name="Root" type="string">text</xs:element>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.xsd")
			if err := os.WriteFile(path, []byte(head+body+"</xs:schema>"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := xsd.ParseFile(path)
			var unsupported *xsd.UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v, want an UnsupportedError", err)
			}
		})
	}
}

// TestGeneratorErrors covers schemas that parse but cannot be generated.
func TestGeneratorErrors(t *testing.T) {
	cases := map[string]string{
		"enum on float":     `<xs:element name="Root" type="t:A"/><xs:simpleType name="A"><xs:restriction base="xs:float"><xs:enumeration value="1"/></xs:restriction></xs:simpleType>`,
		"length on int":     `<xs:element name="Root" type="t:A"/><xs:simpleType name="A"><xs:restriction base="xs:int"><xs:maxLength value="1"/></xs:restriction></xs:simpleType>`,
		"range on string":   `<xs:element name="Root" type="t:A"/><xs:simpleType name="A"><xs:restriction base="string"><xs:minInclusive value="1"/></xs:restriction></xs:simpleType>`,
		"digits on float":   `<xs:element name="Root" type="t:A"/><xs:simpleType name="A"><xs:restriction base="xs:float"><xs:totalDigits value="1"/></xs:restriction></xs:simpleType>`,
		"bad pattern":       `<xs:element name="Root" type="t:A"/><xs:simpleType name="A"><xs:restriction base="string"><xs:pattern value="\p{IsBasicLatin}"/></xs:restriction></xs:simpleType>`,
		"field collision":   `<xs:element name="Root"><xs:complexType><xs:sequence><xs:element name="A" type="string"/><xs:element name="A" type="string"/></xs:sequence></xs:complexType></xs:element>`,
		"type collision":    `<xs:element name="Root"><xs:complexType><xs:sequence><xs:element name="A" type="t:a"/><xs:element name="B" type="t:A"/></xs:sequence></xs:complexType></xs:element><xs:complexType name="a"/><xs:simpleType name="A"><xs:restriction base="string"/></xs:simpleType>`,
		"reserved name":     `<xs:element name="Root"><xs:complexType><xs:sequence><xs:element name="A" type="t:Extra"/></xs:sequence></xs:complexType></xs:element><xs:complexType name="Extra"/>`,
		"no root":           `<xs:element name="Other" type="string"/>`,
		"named root type":   `<xs:element name="Root" type="string"/>`,
		"unresolved":        `<xs:element name="Root"><xs:complexType><xs:sequence><xs:element name="A" type="t:Missing"/></xs:sequence></xs:complexType></xs:element>`,
		"unknown prefix":    `<xs:element name="Root"><xs:complexType><xs:sequence><xs:element name="A" type="q:A"/></xs:sequence></xs:complexType></xs:element>`,
		"cyclic group":      `<xs:element name="Root"><xs:complexType><xs:group ref="t:G"/></xs:complexType></xs:element><xs:group name="G"><xs:sequence><xs:group ref="t:G"/></xs:sequence></xs:group>`,
		"cyclic simple":     `<xs:element name="Root" type="t:A"/><xs:simpleType name="A"><xs:restriction base="t:B"/></xs:simpleType><xs:simpleType name="B"><xs:restriction base="t:A"/></xs:simpleType>`,
		"cyclic extension":  `<xs:element name="Root" type="t:A"/><xs:complexType name="A"><xs:complexContent><xs:extension base="t:B"/></xs:complexContent></xs:complexType><xs:complexType name="B"><xs:complexContent><xs:extension base="t:A"/></xs:complexContent></xs:complexType>`,
		"duplicate type":    `<xs:element name="Root" type="string"/><xs:complexType name="A"/><xs:complexType name="A"/>`,
		"extension of text": `<xs:element name="Root" type="t:A"/><xs:complexType name="A"><xs:complexContent><xs:extension base="string"/></xs:complexContent></xs:complexType>`,
		"Extra element":     `<xs:element name="Root"><xs:complexType><xs:sequence><xs:element name="Extra" type="string"/></xs:sequence></xs:complexType></xs:element>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.xsd")
			if err := os.WriteFile(path, []byte(head+body+"</xs:schema>"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := generate(path, Config{Package: "x", Root: "Root"}); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestRun(t *testing.T) {
	out := t.TempDir()
	stale := filepath.Join(out, "types_zz_gen.go")
	if err := os.WriteFile(stale, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"-schema", filepath.Join("testdata", "golden", "include", "main.xsd"), "-root", "Root", "-ext", "aa, bb", "-pkg", "x", "-out", out, "-v"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale generated file was not removed")
	}
	for _, f := range []string{"types_gen.go", "types_aa_gen.go", "types_bb_gen.go", "validate_gen.go"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{}); err == nil || !strings.Contains(err.Error(), "-schema") {
		t.Fatalf("missing -schema: %v", err)
	}
	if err := run([]string{"-bogus"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

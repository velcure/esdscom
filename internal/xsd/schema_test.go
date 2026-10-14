package xsd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRealSchema(t *testing.T) {
	s, err := ParseFile("../../schema/v5.5/SDSComXML.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "5.5" || s.TargetNamespace != "http://www.esdscom.eu/eSDScom" {
		t.Fatalf("version %q, namespace %q", s.Version, s.TargetNamespace)
	}
	if len(s.Files) != 19 {
		t.Fatalf("loaded %d files, want 19", len(s.Files))
	}
	root := s.Element("DatasheetFeed")
	if root == nil || root.ComplexType == nil {
		t.Fatal("root element DatasheetFeed missing")
	}
	phrase := s.ComplexType(QName{s.TargetNamespace, "Phrase"})
	if phrase == nil || phrase.Particle.Kind != ChoiceParticle || len(phrase.Attributes) != 1 || phrase.Attributes[0].GroupRef.Local != "PhraseAttributes" {
		t.Fatalf("Phrase parsed wrongly: %+v", phrase)
	}
	if s.ComplexType(QName{"urn:other", "Phrase"}) != nil || s.SimpleType(QName{"urn:other", "x"}) != nil ||
		s.Group(QName{"urn:other", "x"}) != nil || s.AttributeGroup(QName{"urn:other", "x"}) != nil {
		t.Fatal("lookup in a foreign namespace succeeded")
	}
	newLine := phrase.Particle.Children[1].Element
	if newLine.Name != "NewLine" || !newLine.IsAnyType() {
		t.Fatalf("NewLine: %+v", newLine)
	}
	pct := s.SimpleType(QName{s.TargetNamespace, "Percentage"})
	if pct.Base != (QName{Namespace, "float"}) || *pct.Facets.MinInclusive != "0" || *pct.Facets.MaxInclusive != "100" {
		t.Fatalf("Percentage: %+v", pct)
	}
}

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.xsd")
	doc := `<?xml version="1.0"?>
<xs:schema targetNamespace="urn:test" xmlns="http://www.w3.org/2001/XMLSchema" xmlns:t="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema" version="2">` + body + `</xs:schema>`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseModel(t *testing.T) {
	s, err := ParseFile(write(t, `
	<xs:annotation><xs:documentation>Schema doc.</xs:documentation></xs:annotation>
	<xs:element name="Root">
		<xs:annotation>
			<xs:documentation xml:lang="en">
				First   line.

				Second line.
			</xs:documentation>
			<xs:appinfo>ignored</xs:appinfo>
			<xs:documentation>More.</xs:documentation>
		</xs:annotation>
		<xs:complexType>
			<xs:sequence minOccurs="0" maxOccurs="unbounded">
				<xs:element name="A" type="t:S" minOccurs="2" maxOccurs="7"/>
				<xs:group ref="t:G" minOccurs="0"/>
				<xs:choice><xs:element name="B" type="string"/><xs:element name="C"/></xs:choice>
			</xs:sequence>
			<xs:attribute name="x" type="xs:int" default="1"/>
			<xs:attributeGroup ref="t:AG"/>
		</xs:complexType>
	</xs:element>
	<xs:group name="G"><xs:sequence><xs:element name="D" type="string"/></xs:sequence></xs:group>
	<xs:attributeGroup name="AG"><xs:attribute name="y"><xs:simpleType><xs:restriction base="string"/></xs:simpleType></xs:attribute></xs:attributeGroup>
	<xs:simpleType name="S">
		<xs:restriction base="t:U">
			<xs:enumeration value="a"><xs:annotation><xs:documentation>The a.</xs:documentation></xs:annotation></xs:enumeration>
			<xs:pattern value="[a-z]"/>
			<xs:length value="1"/><xs:minLength value="1"/><xs:maxLength value="1"/>
			<xs:whiteSpace value="collapse"/>
		</xs:restriction>
	</xs:simpleType>
	<xs:simpleType name="U"><xs:union memberTypes="xs:string t:N"/></xs:simpleType>
	<xs:simpleType name="N"><xs:restriction base="xs:decimal"><xs:totalDigits value="3"/><xs:fractionDigits value="1"/></xs:restriction></xs:simpleType>
	<xs:complexType name="Ext"><xs:simpleContent><xs:extension base="t:S"><xs:attribute name="z" type="string"/></xs:extension></xs:simpleContent></xs:complexType>
	<xs:complexType name="Ext2"><xs:complexContent><xs:extension base="t:Base"><xs:sequence><xs:element name="E" type="t:Ext"/></xs:sequence></xs:extension></xs:complexContent></xs:complexType>
	<xs:complexType name="Base"/>
	`))
	if err != nil {
		t.Fatal(err)
	}
	root := s.Element("Root")
	if root.Doc != "First line.\n\nSecond line.\n\nMore." {
		t.Fatalf("doc = %q", root.Doc)
	}
	seq := root.ComplexType.Particle
	if seq.Kind != SequenceParticle || seq.Min != 0 || seq.Max != Unbounded || len(seq.Children) != 3 {
		t.Fatalf("sequence: %+v", seq)
	}
	if a := seq.Children[0]; a.Min != 2 || a.Max != 7 || a.Element.Type != (QName{"urn:test", "S"}) {
		t.Fatalf("A: %+v", a)
	}
	if g := seq.Children[1]; g.Kind != GroupRefParticle || g.Ref.Local != "G" || g.Min != 0 {
		t.Fatalf("group ref: %+v", g)
	}
	if c := seq.Children[2]; c.Kind != ChoiceParticle || !c.Children[1].Element.IsAnyType() {
		t.Fatalf("choice: %+v", c)
	}
	attrs := root.ComplexType.Attributes
	if len(attrs) != 2 || *attrs[0].Attribute.Default != "1" || attrs[1].GroupRef.Local != "AG" {
		t.Fatalf("attributes: %+v", attrs)
	}
	st := s.SimpleType(QName{"urn:test", "S"})
	f := st.Facets
	if len(f.Enumerations) != 1 || f.Enumerations[0].Doc != "The a." || f.Patterns[0] != "[a-z]" ||
		*f.Length != 1 || *f.MinLength != 1 || *f.MaxLength != 1 || f.WhiteSpace != "collapse" {
		t.Fatalf("facets: %+v", f)
	}
	if u := s.SimpleType(QName{"urn:test", "U"}); u.Kind != Union || len(u.Members) != 2 || !u.Members[0].IsBuiltin() {
		t.Fatalf("union: %+v", u)
	}
	if n := s.SimpleType(QName{"urn:test", "N"}); *n.Facets.TotalDigits != 3 || *n.Facets.FractionDigits != 1 {
		t.Fatalf("digits: %+v", n.Facets)
	}
	if e := s.ComplexType(QName{"urn:test", "Ext"}); e.Content != SimpleExtension || e.Base.Local != "S" || len(e.Attributes) != 1 {
		t.Fatalf("simple content: %+v", e)
	}
	if e := s.ComplexType(QName{"urn:test", "Ext2"}); e.Content != ComplexExtension || e.Particle == nil {
		t.Fatalf("complex content: %+v", e)
	}
	if s.Group(QName{"urn:test", "G"}) == nil || s.AttributeGroup(QName{"urn:test", "AG"}) == nil {
		t.Fatal("group lookups")
	}
	if (QName{Local: "x"}).String() != "x" || (QName{"u", "x"}).String() != "{u}x" || !(QName{}).IsZero() {
		t.Fatal("QName helpers")
	}
	for k, want := range map[ParticleKind]string{ElementParticle: "element", SequenceParticle: "sequence", ChoiceParticle: "choice", GroupRefParticle: "group", 0: "invalid"} {
		if k.String() != want {
			t.Fatalf("%d.String() = %q", k, k.String())
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"bad minOccurs":       `<xs:complexType name="A"><xs:sequence minOccurs="x"/></xs:complexType>`,
		"bad maxOccurs":       `<xs:complexType name="A"><xs:sequence maxOccurs="0"/></xs:complexType>`,
		"min above max":       `<xs:complexType name="A"><xs:sequence minOccurs="3" maxOccurs="2"/></xs:complexType>`,
		"bad length":          `<xs:simpleType name="A"><xs:restriction base="string"><xs:length value="-1"/></xs:restriction></xs:simpleType>`,
		"repeated facet":      `<xs:simpleType name="A"><xs:restriction base="string"><xs:length value="1"/><xs:length value="2"/></xs:restriction></xs:simpleType>`,
		"repeated range":      `<xs:simpleType name="A"><xs:restriction base="xs:int"><xs:minInclusive value="1"/><xs:minInclusive value="2"/></xs:restriction></xs:simpleType>`,
		"bad whiteSpace":      `<xs:simpleType name="A"><xs:restriction base="string"><xs:whiteSpace value="x"/></xs:restriction></xs:simpleType>`,
		"unresolved group":    `<xs:complexType name="A"><xs:group ref="t:G"/></xs:complexType>`,
		"unresolved attrgrp":  `<xs:complexType name="A"><xs:attributeGroup ref="t:G"/></xs:complexType>`,
		"unresolved base":     `<xs:complexType name="A"><xs:complexContent><xs:extension base="t:B"/></xs:complexContent></xs:complexType>`,
		"unresolved member":   `<xs:simpleType name="A"><xs:union memberTypes="t:B"/></xs:simpleType>`,
		"anyType as simple":   `<xs:simpleType name="A"><xs:restriction base="xs:anyType"/></xs:simpleType>`,
		"cyclic attrgrp":      `<xs:attributeGroup name="G"><xs:attributeGroup ref="t:G"/></xs:attributeGroup>`,
		"missing include":     `<xs:include schemaLocation="missing.xsd"/>`,
		"duplicate element":   `<xs:element name="A" type="string"/><xs:element name="A" type="string"/>`,
		"duplicate group":     `<xs:group name="A"><xs:sequence/></xs:group><xs:group name="A"><xs:sequence/></xs:group>`,
		"duplicate attrgrp":   `<xs:attributeGroup name="A"/><xs:attributeGroup name="A"/>`,
		"duplicate simple":    `<xs:simpleType name="A"><xs:restriction base="string"/></xs:simpleType><xs:complexType name="A"/>`,
		"simple ext complex":  `<xs:complexType name="B"/><xs:complexType name="A"><xs:simpleContent><xs:extension base="t:B"/></xs:simpleContent></xs:complexType>`,
		"empty choice":        `<xs:complexType name="A"><xs:choice/></xs:complexType>`,
		"group with occurs":   `<xs:group name="G"><xs:sequence minOccurs="0"/></xs:group>`,
		"group without model": `<xs:group name="G"/>`,
		"element no name":     `<xs:element type="string"/>`,
		"type and inline":     `<xs:element name="A" type="string"><xs:simpleType><xs:restriction base="string"/></xs:simpleType></xs:element>`,
		"late annotation":     `<xs:complexType name="A"><xs:sequence/><xs:annotation/></xs:complexType>`,
		"union no members":    `<xs:simpleType name="A"><xs:union/></xs:simpleType>`,
		"attribute no type":   `<xs:attributeGroup name="G"><xs:attribute name="a"/></xs:attributeGroup>`,
		"facet no value":      `<xs:simpleType name="A"><xs:restriction base="string"><xs:pattern/></xs:restriction></xs:simpleType>`,
		"restriction no base": `<xs:simpleType name="A"><xs:restriction/></xs:simpleType>`,
		"attrs after model":   `<xs:complexType name="A"><xs:attribute name="a" type="string"/><xs:sequence/></xs:complexType>`,
		"two inline types":    `<xs:element name="A"><xs:simpleType><xs:restriction base="string"/></xs:simpleType><xs:complexType/></xs:element>`,
		"empty content":       `<xs:complexType name="A"><xs:complexContent/></xs:complexType>`,
		"extension no base":   `<xs:complexType name="A"><xs:complexContent><xs:extension/></xs:complexContent></xs:complexType>`,
		"simple content seq":  `<xs:complexType name="A"><xs:simpleContent><xs:extension base="string"><xs:sequence/></xs:extension></xs:simpleContent></xs:complexType>`,
		"anonymous top type":  `<xs:complexType/>`,
		"local group def":     `<xs:complexType name="A"><xs:sequence><xs:group name="x"/></xs:sequence></xs:complexType>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFile(write(t, body)); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestParseDocumentErrors(t *testing.T) {
	dir := t.TempDir()
	for name, doc := range map[string]string{
		"malformed":  `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><x>`,
		"not schema": `<a/>`,
		"empty":      ``,
		"no ns":      `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`,
		"qualified":  `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="u" elementFormDefault="qualified"/>`,
		"foreign":    `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="u" xmlns:f="urn:f" f:x="1"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".xsd")
			if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := ParseFile(path); err == nil {
				t.Fatal("no error")
			}
		})
	}
	if _, err := ParseFile(filepath.Join(dir, "missing.xsd")); err == nil {
		t.Fatal("missing file accepted")
	}
	var u *UnsupportedError
	_, err := ParseFile(write(t, `<xs:notation name="n" public="p"/>`))
	if !errors.As(err, &u) || !strings.Contains(u.Error(), "xs:notation") {
		t.Fatalf("err = %v", err)
	}
}

func TestIncludeNamespaceMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.xsd")
	other := filepath.Join(dir, "other.xsd")
	if err := os.WriteFile(main, []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="u"><xs:include schemaLocation="other.xsd"/><xs:include schemaLocation="other.xsd"/></xs:schema>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="v"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(main); err == nil {
		t.Fatal("namespace mismatch accepted")
	}
}

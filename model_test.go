package esdscom

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

// The eSDScom types of the EU and DE extensions use no repeated sequences
// or choices; other national extensions do. These tests run the runtime
// on a hand-written description that has them.

type tHolder struct {
	Choice  tHolderChoice
	Choice2 []tHolderChoice2
	Pairs   []tPair
	Opt     *tOpt
	Extra   Extra
}

type tHolderChoice struct {
	A      *string
	B      []string
	CGroup *tCGroup
	Choice *tInner
}

type tCGroup struct {
	C *string
	D *string
}

type tInner struct {
	E *string
	F *string
}

type tHolderChoice2 struct {
	G *string
	H *string
}

type tPair struct {
	Key   *string
	Value *string
}

type tOpt struct {
	Nested *string
}

var ctHolder = func() *complexType {
	c := &complexType{name: "Holder"}
	c.define(reflect.TypeFor[tHolder](), nil, nil,
		sequence("", 1, 1,
			choice("Choice", 1, 1,
				simpleElem("A", "A", 1, 1, xsString),
				simpleElem("B", "B", 1, unbounded, xsString),
				sequence("CGroup", 1, 1,
					simpleElem("C", "C", 1, 1, xsString),
					simpleElem("D", "D", 0, 1, xsString)),
				choice("Choice", 1, 1,
					simpleElem("E", "E", 1, 1, xsString),
					simpleElem("F", "F", 1, 1, xsString))),
			choice("Choice2", 0, unbounded,
				simpleElem("G", "G", 1, 1, xsString),
				simpleElem("H", "H", 1, 1, xsString)),
			sequence("Pairs", 0, 2,
				simpleElem("Key", "Key", 1, 1, xsString),
				simpleElem("Value", "Value", 0, 1, xsString)),
			sequence("Opt", 0, 1,
				simpleElem("Nested", "Nested", 1, 1, xsString))))
	return c
}()

func decodeHolder(t *testing.T, doc string) *tHolder {
	t.Helper()
	d := &decoder{x: xml.NewDecoder(strings.NewReader(doc))}
	tok, err := d.token()
	if err != nil {
		t.Fatal(err)
	}
	h := new(tHolder)
	if err := d.complex(ctHolder, reflect.ValueOf(h).Elem(), tok.(xml.StartElement), ""); err != nil {
		t.Fatal(err)
	}
	return h
}

func encodeHolder(t *testing.T, h *tHolder) string {
	t.Helper()
	var buf bytes.Buffer
	e := &encoder{w: bufio.NewWriter(&buf)}
	e.complex(ctHolder, reflect.ValueOf(h).Elem(), xml.Name{Local: "T"})
	if err := e.w.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func validateHolder(h *tHolder) ValidationErrors {
	vd := &validator{}
	vd.complex(ctHolder, reflect.ValueOf(h).Elem(), "")
	return vd.out
}

func rules(v ValidationErrors) []string {
	var out []string
	for _, e := range v {
		out = append(out, e.Rule+"@"+e.Path)
	}
	return out
}

func TestRepeatedContainers(t *testing.T) {
	doc := `<T><C>c</C><D>d</D><G>1</G><H>2</H><G>3</G><Key>k1</Key><Key>k2</Key><Value>v2</Value><Nested>n</Nested></T>`
	h := decodeHolder(t, doc)
	s := func(v string) *string { return &v }
	want := &tHolder{
		Choice:  tHolderChoice{CGroup: &tCGroup{C: s("c"), D: s("d")}},
		Choice2: []tHolderChoice2{{G: s("1")}, {H: s("2")}, {G: s("3")}},
		Pairs:   []tPair{{Key: s("k1")}, {Key: s("k2"), Value: s("v2")}},
		Opt:     &tOpt{Nested: s("n")},
	}
	if !reflect.DeepEqual(h, want) {
		t.Fatalf("decoded %+v", h)
	}
	if v := validateHolder(h); v != nil {
		t.Fatalf("findings: %v", v)
	}
	if got := encodeHolder(t, h); got != doc {
		t.Fatalf("encoded %s", got)
	}
}

func TestContainerFindings(t *testing.T) {
	tests := []struct {
		doc  string
		want []string
	}{
		{`<T><A>a</A><E>e</E></T>`, []string{"choice@"}},
		{`<T></T>`, []string{"choice@"}},
		{`<T><Choice/></T>`, []string{"choice@", "unknown@Choice"}},
		{`<T><A>a</A><Value>v</Value></T>`, []string{"required@Key"}},
		{`<T><A>a</A><Key>1</Key><Key>2</Key><Key>3</Key></T>`, []string{"maxOccurs@Pairs"}},
		{`<T><A>a</A><A>b</A></T>`, []string{"unexpected@A"}},
		{`<T><E>e</E><F>f</F></T>`, []string{"choice@"}},
		{`<T><B>1</B><B>2</B></T>`, nil},
		{`<T><D>d</D></T>`, []string{"required@C"}},
	}
	for _, tt := range tests {
		h := decodeHolder(t, tt.doc)
		if got := rules(validateHolder(h)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: findings %v, want %v", tt.doc, got, tt.want)
		}
		again := decodeHolder(t, encodeHolder(t, h))
		if !reflect.DeepEqual(h, again) {
			t.Errorf("%s: round trip changed the tree", tt.doc)
		}
	}
}

func TestOutOfOrderIsNormalized(t *testing.T) {
	h := decodeHolder(t, `<T><Nested>n</Nested><A>a</A></T>`)
	if got := encodeHolder(t, h); got != `<T><A>a</A><Nested>n</Nested></T>` {
		t.Fatalf("encoded %s", got)
	}
}

func TestProgrammaticContent(t *testing.T) {
	h := &tHolder{Opt: &tOpt{}}
	h.Choice.Choice = &tInner{}
	h.Extra.Nodes = append(h.Extra.Nodes, RawNode{Element: &RawElement{Name: xml.Name{Space: "urn:x", Local: "Note"}, Attrs: []xml.Attr{{Name: xml.Name{Space: "urn:x", Local: "a"}, Value: "1"}}}})
	if h.Extra.Empty() || (&Extra{}).Empty() == false {
		t.Fatal("Extra.Empty")
	}
	got := rules(validateHolder(h))
	want := []string{"choice@", "required@Nested", "unknown@Note"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v, want %v", got, want)
	}
	if out := encodeHolder(t, h); out != `<T><ns1:Note xmlns:ns1="urn:x" ns1:a="1"/></T>` {
		t.Fatalf("encoded %s", out)
	}
}

func TestDescribe(t *testing.T) {
	c := ctHolder.model.children[0]
	if got := labels(c.children); got != "A, B, (C, D), (E | F)" {
		t.Fatalf("labels = %q", got)
	}
	if got := sequence("Empty", 0, 1).describe(); got != "Empty" {
		t.Fatalf("describe = %q", got)
	}
}

func TestDefinePanics(t *testing.T) {
	type noExtra struct{ A *string }
	type badField struct {
		A     string
		Extra Extra
	}
	type badSlice struct {
		A     *string
		Extra Extra
	}
	for name, f := range map[string]func(){
		"no Extra": func() { (&complexType{}).define(reflect.TypeFor[noExtra](), nil, nil, nil) },
		"no field": func() {
			(&complexType{}).define(reflect.TypeFor[badField](), nil, nil, simpleElem("X", "X", 1, 1, xsString))
		},
		"not ptr": func() {
			(&complexType{}).define(reflect.TypeFor[badField](), nil, nil, simpleElem("A", "A", 1, 1, xsString))
		},
		"not slice": func() {
			(&complexType{}).define(reflect.TypeFor[badSlice](), nil, nil, simpleElem("A", "A", 1, 2, xsString))
		},
		"wrong kind": func() { (&complexType{}).define(reflect.TypeFor[badSlice](), nil, nil, anyElem("A", "A", 1, 1)) },
		"attr kind": func() {
			(&complexType{}).define(reflect.TypeFor[badField](), []attribute{{name: "a", field: "A"}}, nil, nil)
		},
		"attr field": func() {
			(&complexType{}).define(reflect.TypeFor[badField](), []attribute{{name: "a", field: "Z"}}, nil, nil)
		},
		"container": func() { (&complexType{}).define(reflect.TypeFor[badSlice](), nil, nil, sequence("A", 0, 1)) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			f()
		})
	}
}

// TestGeneratedSimpleTypes walks all types reachable from the root and
// checks that the Valid method of each simple Go type agrees with its
// description, for a few probe values.
func TestGeneratedSimpleTypes(t *testing.T) {
	seen := map[*complexType]bool{}
	checked := map[reflect.Type]bool{}
	probes := []string{"", "1", "true", "2026-10-14", "2026-10-14T00:00:00", "x"}
	check := func(st *simpleType, gt reflect.Type) {
		if checked[gt] {
			return
		}
		checked[gt] = true
		valid := reflect.New(gt).Elem().MethodByName("Valid")
		for _, p := range probes {
			v := reflect.New(gt).Elem()
			v.SetString(p)
			if got := v.MethodByName("Valid").Call(nil)[0].Bool(); got != st.valid(p) {
				t.Errorf("%s(%q).Valid() = %v, description says %v", gt, p, got, st.valid(p))
			}
			for _, m := range []string{"Bool", "Int64", "Float64", "Time"} {
				if fn := v.MethodByName(m); fn.IsValid() {
					fn.Call(nil)
				}
			}
		}
		if gt.Kind() == reflect.String && gt != reflect.TypeFor[string]() && !valid.IsValid() {
			t.Errorf("%s has no Valid method", gt)
		}
	}
	elemType := func(t reflect.Type) reflect.Type {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
			t = t.Elem()
		}
		return t
	}
	var walk func(c *complexType)
	var visit func(p *particle, st reflect.Type)
	visit = func(p *particle, st reflect.Type) {
		var ft reflect.Type
		if p.index >= 0 {
			ft = elemType(st.Field(p.index).Type)
		} else {
			ft = st
		}
		switch {
		case p.kind != pElement:
			for _, c := range p.children {
				visit(c, ft)
			}
		case p.complex != nil:
			walk(p.complex)
		case p.simple != nil && ft != reflect.TypeFor[string]():
			check(p.simple, ft)
		}
	}
	walk = func(c *complexType) {
		if seen[c] {
			return
		}
		seen[c] = true
		for _, a := range c.attrs {
			check(a.typ, elemType(c.goType.Field(a.index).Type))
		}
		if c.text != nil && c.goType.Field(c.textIndex).Type != reflect.TypeFor[string]() {
			check(c.text, c.goType.Field(c.textIndex).Type)
		}
		if c.model != nil {
			visit(c.model, c.goType)
		}
	}
	walk(rootType)
	if len(seen) < 300 || len(checked) < 100 {
		t.Fatalf("walked %d complex and %d simple types", len(seen), len(checked))
	}
}

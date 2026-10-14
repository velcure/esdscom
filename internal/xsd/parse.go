package xsd

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UnsupportedError reports an XSD construct outside the supported subset.
type UnsupportedError struct {
	Pos  Pos
	What string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s:%d: unsupported XSD construct: %s", e.Pos.File, e.Pos.Line, e.What)
}

// Builtins lists the XML Schema built-in types the parser accepts as type
// or base references.
var Builtins = map[string]bool{
	"anyType":  true,
	"string":   true,
	"boolean":  true,
	"integer":  true,
	"int":      true,
	"float":    true,
	"decimal":  true,
	"date":     true,
	"dateTime": true,
	"anyURI":   true,
}

// node is a parsed XSD element with its in-scope namespace bindings.
type node struct {
	name     xml.Name
	attrs    map[string]string
	ns       map[string]string
	children []*node
	text     strings.Builder
	pos      Pos
}

// ParseFile loads the schema document at path together with every document
// it includes, transitively.
func ParseFile(path string) (*Schema, error) {
	p := &parser{
		s: &Schema{
			complexByName: map[string]*ComplexType{},
			simpleByName:  map[string]*SimpleType{},
			groupByName:   map[string]*Group{},
			attrGrpByName: map[string]*AttributeGroup{},
			elemByName:    map[string]*Element{},
		},
		loaded: map[string]bool{},
	}
	if err := p.load(path, ""); err != nil {
		return nil, err
	}
	if err := p.resolve(); err != nil {
		return nil, err
	}
	return p.s, nil
}

type parser struct {
	s      *Schema
	loaded map[string]bool
}

func (p *parser) load(path, wantNS string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if p.loaded[abs] {
		return nil
	}
	p.loaded[abs] = true
	f, err := os.Open(abs) //nolint:gosec // The generator reads the schema files its caller names.
	if err != nil {
		return err
	}
	defer f.Close()
	root, err := readTree(f, filepath.Base(abs))
	if err != nil {
		return err
	}
	if root.name != (xml.Name{Space: Namespace, Local: "schema"}) {
		return fmt.Errorf("%s: root element is not xs:schema", filepath.Base(abs))
	}
	if err := allowAttrs(root, "targetNamespace", "version", "elementFormDefault", "attributeFormDefault"); err != nil {
		return err
	}
	for _, a := range []string{"elementFormDefault", "attributeFormDefault"} {
		if v, ok := root.attrs[a]; ok && v != "unqualified" {
			return unsupported(root, a+`="`+v+`"`)
		}
	}
	tns, ok := root.attrs["targetNamespace"]
	if !ok {
		return unsupported(root, "schema without targetNamespace")
	}
	if wantNS == "" {
		p.s.TargetNamespace = tns
		p.s.Version = root.attrs["version"]
	} else if tns != wantNS {
		return unsupported(root, "included schema with different targetNamespace "+tns)
	}
	p.s.Files = append(p.s.Files, filepath.Base(abs))

	var includes []string
	for _, c := range root.children {
		switch c.name.Local {
		case "annotation":
			if _, err := p.doc(c); err != nil {
				return err
			}
		case "include":
			if err := allowAttrs(c, "schemaLocation"); err != nil {
				return err
			}
			if err := p.onlyAnnotation(c); err != nil {
				return err
			}
			includes = append(includes, filepath.Join(filepath.Dir(abs), c.attrs["schemaLocation"]))
		case "element":
			e, err := p.element(c, true)
			if err != nil {
				return err
			}
			if p.s.elemByName[e.Name] != nil {
				return dup(c, "element", e.Name)
			}
			p.s.elemByName[e.Name] = e
			p.s.Elements = append(p.s.Elements, e)
		case "complexType":
			t, err := p.complexType(c, true)
			if err != nil {
				return err
			}
			if p.s.complexByName[t.Name] != nil || p.s.simpleByName[t.Name] != nil {
				return dup(c, "type", t.Name)
			}
			p.s.complexByName[t.Name] = t
			p.s.ComplexTypes = append(p.s.ComplexTypes, t)
		case "simpleType":
			t, err := p.simpleType(c, true)
			if err != nil {
				return err
			}
			if p.s.complexByName[t.Name] != nil || p.s.simpleByName[t.Name] != nil {
				return dup(c, "type", t.Name)
			}
			p.s.simpleByName[t.Name] = t
			p.s.SimpleTypes = append(p.s.SimpleTypes, t)
		case "group":
			g, err := p.groupDef(c)
			if err != nil {
				return err
			}
			if p.s.groupByName[g.Name] != nil {
				return dup(c, "group", g.Name)
			}
			p.s.groupByName[g.Name] = g
			p.s.Groups = append(p.s.Groups, g)
		case "attributeGroup":
			g, err := p.attributeGroupDef(c)
			if err != nil {
				return err
			}
			if p.s.attrGrpByName[g.Name] != nil {
				return dup(c, "attributeGroup", g.Name)
			}
			p.s.attrGrpByName[g.Name] = g
			p.s.AttributeGroups = append(p.s.AttributeGroups, g)
		default:
			return unsupported(c, "xs:"+c.name.Local+" in xs:schema")
		}
	}
	for _, inc := range includes {
		if err := p.load(inc, p.s.TargetNamespace); err != nil {
			return err
		}
	}
	return nil
}

// readTree parses an XSD document into nodes. Only elements of the XML
// Schema namespace are accepted, except inside xs:documentation and
// xs:appinfo, whose content is free.
func readTree(r io.Reader, file string) (*node, error) {
	d := xml.NewDecoder(r)
	var stack []*node
	var root *node
	freeDepth := 0
	for {
		line, _ := d.InputPos()
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if freeDepth > 0 {
				freeDepth++
				continue
			}
			n := &node{name: t.Name, attrs: map[string]string{}, ns: map[string]string{}, pos: Pos{file, line}}
			if len(stack) > 0 {
				for k, v := range stack[len(stack)-1].ns {
					n.ns[k] = v
				}
			}
			for _, a := range t.Attr {
				switch {
				case a.Name.Space == "xmlns":
					n.ns[a.Name.Local] = a.Value
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					n.ns[""] = a.Value
				case a.Name.Space == "":
					n.attrs[a.Name.Local] = a.Value
				case a.Name.Space == "http://www.w3.org/XML/1998/namespace" && a.Name.Local == "lang":
				default:
					return nil, &UnsupportedError{n.pos, "foreign attribute " + a.Name.Space + ":" + a.Name.Local}
				}
			}
			if t.Name.Space != Namespace {
				return nil, &UnsupportedError{n.pos, "foreign element {" + t.Name.Space + "}" + t.Name.Local}
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
			if t.Name.Local == "documentation" || t.Name.Local == "appinfo" {
				freeDepth = 1
			}
		case xml.EndElement:
			if freeDepth > 1 {
				freeDepth--
				continue
			}
			freeDepth = 0
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if freeDepth > 0 {
				top.text.Write(t)
				continue
			}
			if strings.TrimSpace(string(t)) != "" {
				return nil, &UnsupportedError{top.pos, "text content in xs:" + top.name.Local}
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%s: empty document", file)
	}
	return root, nil
}

func unsupported(n *node, what string) error {
	return &UnsupportedError{n.pos, what}
}

func dup(n *node, kind, name string) error {
	return fmt.Errorf("%s:%d: duplicate %s %q", n.pos.File, n.pos.Line, kind, name)
}

func allowAttrs(n *node, names ...string) error {
	for k := range n.attrs {
		ok := false
		for _, a := range names {
			if k == a {
				ok = true
				break
			}
		}
		if !ok {
			return unsupported(n, "attribute "+k+" on xs:"+n.name.Local)
		}
	}
	return nil
}

func (p *parser) qname(n *node, v string) (QName, error) {
	prefix, local, ok := strings.Cut(v, ":")
	if !ok {
		prefix, local = "", v
	}
	ns, found := n.ns[prefix]
	if !found {
		if prefix == "" {
			return QName{Local: local}, nil
		}
		return QName{}, fmt.Errorf("%s:%d: unknown namespace prefix %q", n.pos.File, n.pos.Line, prefix)
	}
	return QName{ns, local}, nil
}

func occurs(n *node) (minOcc, maxOcc int, err error) {
	minOcc, maxOcc = 1, 1
	if v, ok := n.attrs["minOccurs"]; ok {
		minOcc, err = strconv.Atoi(v)
		if err != nil || minOcc < 0 {
			return 0, 0, fmt.Errorf("%s:%d: bad minOccurs %q", n.pos.File, n.pos.Line, v)
		}
	}
	if v, ok := n.attrs["maxOccurs"]; ok {
		if v == "unbounded" {
			maxOcc = Unbounded
		} else {
			maxOcc, err = strconv.Atoi(v)
			if err != nil || maxOcc < 1 {
				return 0, 0, fmt.Errorf("%s:%d: bad maxOccurs %q", n.pos.File, n.pos.Line, v)
			}
		}
	}
	if maxOcc != Unbounded && minOcc > maxOcc {
		return 0, 0, fmt.Errorf("%s:%d: minOccurs > maxOccurs", n.pos.File, n.pos.Line)
	}
	return minOcc, maxOcc, nil
}

// doc returns the normalized text of an xs:annotation.
func (p *parser) doc(n *node) (string, error) {
	if err := allowAttrs(n, "id"); err != nil {
		return "", err
	}
	var parts []string
	for _, c := range n.children {
		switch c.name.Local {
		case "documentation":
			if err := allowAttrs(c, "source"); err != nil {
				return "", err
			}
			if t := normalizeDoc(c.text.String()); t != "" {
				parts = append(parts, t)
			}
		case "appinfo":
			if err := allowAttrs(c, "source"); err != nil {
				return "", err
			}
		default:
			return "", unsupported(c, "xs:"+c.name.Local+" in xs:annotation")
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func normalizeDoc(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// splitAnnotation separates a leading xs:annotation from the remaining
// children; an annotation anywhere else is an error.
func (p *parser) splitAnnotation(n *node) (string, []*node, error) {
	var docText string
	var rest []*node
	for i, c := range n.children {
		if c.name.Local == "annotation" {
			if i != 0 {
				return "", nil, unsupported(c, "xs:annotation not in first position")
			}
			d, err := p.doc(c)
			if err != nil {
				return "", nil, err
			}
			docText = d
			continue
		}
		rest = append(rest, c)
	}
	return docText, rest, nil
}

func (p *parser) onlyAnnotation(n *node) error {
	_, rest, err := p.splitAnnotation(n)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return unsupported(rest[0], "xs:"+rest[0].name.Local+" in xs:"+n.name.Local)
	}
	return nil
}

func (p *parser) element(n *node, global bool) (*Element, error) {
	var err error
	if global {
		err = allowAttrs(n, "name", "type")
	} else {
		err = allowAttrs(n, "name", "type", "minOccurs", "maxOccurs", "form")
	}
	if err != nil {
		return nil, err
	}
	if v, ok := n.attrs["form"]; ok && v != "unqualified" {
		return nil, unsupported(n, `form="`+v+`"`)
	}
	name, ok := n.attrs["name"]
	if !ok {
		return nil, unsupported(n, "xs:element without name (ref)")
	}
	e := &Element{Name: name, Pos: n.pos}
	if v, ok := n.attrs["type"]; ok {
		if e.Type, err = p.qname(n, v); err != nil {
			return nil, err
		}
	}
	d, rest, err := p.splitAnnotation(n)
	if err != nil {
		return nil, err
	}
	e.Doc = d
	if len(rest) > 1 {
		return nil, unsupported(rest[1], "xs:"+rest[1].name.Local+" in xs:element")
	}
	if len(rest) == 1 {
		if !e.Type.IsZero() {
			return nil, unsupported(rest[0], "xs:element with both type and inline type")
		}
		switch rest[0].name.Local {
		case "complexType":
			if e.ComplexType, err = p.complexType(rest[0], false); err != nil {
				return nil, err
			}
		case "simpleType":
			if e.SimpleType, err = p.simpleType(rest[0], false); err != nil {
				return nil, err
			}
		default:
			return nil, unsupported(rest[0], "xs:"+rest[0].name.Local+" in xs:element")
		}
	}
	return e, nil
}

// particle parses xs:element, xs:sequence, xs:choice or a group reference
// inside a content model.
func (p *parser) particle(n *node) (*Particle, error) {
	minOcc, maxOcc, err := occurs(n)
	if err != nil {
		return nil, err
	}
	pt := &Particle{Min: minOcc, Max: maxOcc, Pos: n.pos}
	switch n.name.Local {
	case "element":
		pt.Kind = ElementParticle
		if pt.Element, err = p.element(n, false); err != nil {
			return nil, err
		}
		pt.Doc = pt.Element.Doc
	case "sequence", "choice":
		if err := allowAttrs(n, "minOccurs", "maxOccurs"); err != nil {
			return nil, err
		}
		pt.Kind = SequenceParticle
		if n.name.Local == "choice" {
			pt.Kind = ChoiceParticle
		}
		d, rest, err := p.splitAnnotation(n)
		if err != nil {
			return nil, err
		}
		pt.Doc = d
		for _, c := range rest {
			switch c.name.Local {
			case "element", "sequence", "choice", "group":
				cp, err := p.particle(c)
				if err != nil {
					return nil, err
				}
				pt.Children = append(pt.Children, cp)
			default:
				return nil, unsupported(c, "xs:"+c.name.Local+" in xs:"+n.name.Local)
			}
		}
		if pt.Kind == ChoiceParticle && len(pt.Children) == 0 {
			return nil, unsupported(n, "empty xs:choice")
		}
	case "group":
		if err := allowAttrs(n, "ref", "minOccurs", "maxOccurs"); err != nil {
			return nil, err
		}
		ref, ok := n.attrs["ref"]
		if !ok {
			return nil, unsupported(n, "local xs:group without ref")
		}
		pt.Kind = GroupRefParticle
		if pt.Ref, err = p.qname(n, ref); err != nil {
			return nil, err
		}
		d, rest, err := p.splitAnnotation(n)
		if err != nil {
			return nil, err
		}
		if len(rest) > 0 {
			return nil, unsupported(rest[0], "xs:"+rest[0].name.Local+" in xs:group reference")
		}
		pt.Doc = d
	default:
		return nil, unsupported(n, "xs:"+n.name.Local+" as particle")
	}
	return pt, nil
}

func (p *parser) complexType(n *node, global bool) (*ComplexType, error) {
	var err error
	if global {
		err = allowAttrs(n, "name")
	} else {
		err = allowAttrs(n)
	}
	if err != nil {
		return nil, err
	}
	t := &ComplexType{Name: n.attrs["name"], Pos: n.pos, Content: ElementContent}
	if global && t.Name == "" {
		return nil, unsupported(n, "top-level xs:complexType without name")
	}
	d, rest, err := p.splitAnnotation(n)
	if err != nil {
		return nil, err
	}
	t.Doc = d
	if len(rest) > 0 {
		switch rest[0].name.Local {
		case "complexContent", "simpleContent":
			if len(rest) > 1 {
				return nil, unsupported(rest[1], "xs:"+rest[1].name.Local+" after xs:"+rest[0].name.Local)
			}
			return t, p.content(rest[0], t)
		}
	}
	return t, p.modelAndAttrs(n, rest, t)
}

// modelAndAttrs parses an optional model group followed by attribute uses.
func (p *parser) modelAndAttrs(parent *node, nodes []*node, t *ComplexType) error {
	for i, c := range nodes {
		switch c.name.Local {
		case "sequence", "choice", "group":
			if i != 0 {
				return unsupported(c, "model group after attributes in xs:"+parent.name.Local)
			}
			pt, err := p.particle(c)
			if err != nil {
				return err
			}
			t.Particle = pt
		case "attribute", "attributeGroup":
			u, err := p.attributeUse(c)
			if err != nil {
				return err
			}
			t.Attributes = append(t.Attributes, u)
		default:
			return unsupported(c, "xs:"+c.name.Local+" in xs:"+parent.name.Local)
		}
	}
	return nil
}

func (p *parser) content(n *node, t *ComplexType) error {
	if err := allowAttrs(n); err != nil {
		return err
	}
	_, rest, err := p.splitAnnotation(n)
	if err != nil {
		return err
	}
	if len(rest) != 1 || rest[0].name.Local != "extension" {
		if len(rest) > 0 {
			return unsupported(rest[0], "xs:"+rest[0].name.Local+" in xs:"+n.name.Local)
		}
		return unsupported(n, "empty xs:"+n.name.Local)
	}
	ext := rest[0]
	if err := allowAttrs(ext, "base"); err != nil {
		return err
	}
	base, ok := ext.attrs["base"]
	if !ok {
		return unsupported(ext, "xs:extension without base")
	}
	if t.Base, err = p.qname(ext, base); err != nil {
		return err
	}
	_, items, err := p.splitAnnotation(ext)
	if err != nil {
		return err
	}
	if n.name.Local == "simpleContent" {
		t.Content = SimpleExtension
		for _, c := range items {
			if c.name.Local != "attribute" && c.name.Local != "attributeGroup" {
				return unsupported(c, "xs:"+c.name.Local+" in simple content extension")
			}
		}
	} else {
		t.Content = ComplexExtension
	}
	return p.modelAndAttrs(ext, items, t)
}

func (p *parser) attributeUse(n *node) (*AttributeUse, error) {
	if n.name.Local == "attributeGroup" {
		if err := allowAttrs(n, "ref"); err != nil {
			return nil, err
		}
		ref, ok := n.attrs["ref"]
		if !ok {
			return nil, unsupported(n, "local xs:attributeGroup without ref")
		}
		if err := p.onlyAnnotation(n); err != nil {
			return nil, err
		}
		q, err := p.qname(n, ref)
		if err != nil {
			return nil, err
		}
		return &AttributeUse{GroupRef: q}, nil
	}
	if err := allowAttrs(n, "name", "type", "default"); err != nil {
		return nil, err
	}
	name, ok := n.attrs["name"]
	if !ok {
		return nil, unsupported(n, "xs:attribute without name (ref)")
	}
	a := &Attribute{Name: name, Pos: n.pos}
	if v, ok := n.attrs["default"]; ok {
		a.Default = &v
	}
	var err error
	if v, ok := n.attrs["type"]; ok {
		if a.Type, err = p.qname(n, v); err != nil {
			return nil, err
		}
	}
	d, rest, err := p.splitAnnotation(n)
	if err != nil {
		return nil, err
	}
	a.Doc = d
	if len(rest) > 1 || len(rest) == 1 && rest[0].name.Local != "simpleType" {
		return nil, unsupported(rest[len(rest)-1], "xs:"+rest[len(rest)-1].name.Local+" in xs:attribute")
	}
	if len(rest) == 1 {
		if !a.Type.IsZero() {
			return nil, unsupported(rest[0], "xs:attribute with both type and inline type")
		}
		if a.SimpleType, err = p.simpleType(rest[0], false); err != nil {
			return nil, err
		}
	}
	if a.Type.IsZero() && a.SimpleType == nil {
		return nil, unsupported(n, "xs:attribute without type")
	}
	return &AttributeUse{Attribute: a}, nil
}

func (p *parser) groupDef(n *node) (*Group, error) {
	if err := allowAttrs(n, "name"); err != nil {
		return nil, err
	}
	g := &Group{Name: n.attrs["name"], Pos: n.pos}
	if g.Name == "" {
		return nil, unsupported(n, "top-level xs:group without name")
	}
	d, rest, err := p.splitAnnotation(n)
	if err != nil {
		return nil, err
	}
	g.Doc = d
	if len(rest) != 1 || (rest[0].name.Local != "sequence" && rest[0].name.Local != "choice") {
		return nil, unsupported(n, "xs:group definition must hold exactly one xs:sequence or xs:choice")
	}
	if _, ok := rest[0].attrs["minOccurs"]; ok {
		return nil, unsupported(rest[0], "minOccurs on the model group of a group definition")
	}
	if _, ok := rest[0].attrs["maxOccurs"]; ok {
		return nil, unsupported(rest[0], "maxOccurs on the model group of a group definition")
	}
	if g.Particle, err = p.particle(rest[0]); err != nil {
		return nil, err
	}
	return g, nil
}

func (p *parser) attributeGroupDef(n *node) (*AttributeGroup, error) {
	if err := allowAttrs(n, "name"); err != nil {
		return nil, err
	}
	g := &AttributeGroup{Name: n.attrs["name"], Pos: n.pos}
	if g.Name == "" {
		return nil, unsupported(n, "top-level xs:attributeGroup without name")
	}
	_, rest, err := p.splitAnnotation(n)
	if err != nil {
		return nil, err
	}
	for _, c := range rest {
		if c.name.Local != "attribute" && c.name.Local != "attributeGroup" {
			return nil, unsupported(c, "xs:"+c.name.Local+" in xs:attributeGroup")
		}
		u, err := p.attributeUse(c)
		if err != nil {
			return nil, err
		}
		g.Attributes = append(g.Attributes, u)
	}
	return g, nil
}

func (p *parser) simpleType(n *node, global bool) (*SimpleType, error) {
	var err error
	if global {
		err = allowAttrs(n, "name")
	} else {
		err = allowAttrs(n)
	}
	if err != nil {
		return nil, err
	}
	t := &SimpleType{Name: n.attrs["name"], Pos: n.pos}
	if global && t.Name == "" {
		return nil, unsupported(n, "top-level xs:simpleType without name")
	}
	d, rest, err := p.splitAnnotation(n)
	if err != nil {
		return nil, err
	}
	t.Doc = d
	if len(rest) != 1 {
		return nil, unsupported(n, "xs:simpleType must hold exactly one xs:restriction or xs:union")
	}
	c := rest[0]
	switch c.name.Local {
	case "restriction":
		t.Kind = Restriction
		return t, p.restriction(c, t)
	case "union":
		t.Kind = Union
		if err := allowAttrs(c, "memberTypes"); err != nil {
			return nil, err
		}
		if err := p.onlyAnnotation(c); err != nil {
			return nil, err
		}
		for _, m := range strings.Fields(c.attrs["memberTypes"]) {
			q, err := p.qname(c, m)
			if err != nil {
				return nil, err
			}
			t.Members = append(t.Members, q)
		}
		if len(t.Members) == 0 {
			return nil, unsupported(c, "xs:union without memberTypes")
		}
		return t, nil
	default:
		return nil, unsupported(c, "xs:"+c.name.Local+" in xs:simpleType")
	}
}

func (p *parser) restriction(n *node, t *SimpleType) error {
	if err := allowAttrs(n, "base"); err != nil {
		return err
	}
	_, rest, err := p.splitAnnotation(n)
	if err != nil {
		return err
	}
	if base, ok := n.attrs["base"]; ok {
		if t.Base, err = p.qname(n, base); err != nil {
			return err
		}
	} else {
		if len(rest) == 0 || rest[0].name.Local != "simpleType" {
			return unsupported(n, "xs:restriction without base")
		}
		if t.BaseType, err = p.simpleType(rest[0], false); err != nil {
			return err
		}
		rest = rest[1:]
	}
	f := &t.Facets
	for _, c := range rest {
		if err := allowAttrs(c, "value"); err != nil {
			return err
		}
		v, ok := c.attrs["value"]
		if !ok {
			return unsupported(c, "facet without value")
		}
		d, extra, err := p.splitAnnotation(c)
		if err != nil {
			return err
		}
		if len(extra) > 0 {
			return unsupported(extra[0], "xs:"+extra[0].name.Local+" in facet")
		}
		switch c.name.Local {
		case "enumeration":
			f.Enumerations = append(f.Enumerations, Enumeration{Value: v, Doc: d})
		case "pattern":
			f.Patterns = append(f.Patterns, v)
		case "length", "minLength", "maxLength", "totalDigits", "fractionDigits":
			i, err := strconv.Atoi(v)
			if err != nil || i < 0 {
				return fmt.Errorf("%s:%d: bad %s %q", c.pos.File, c.pos.Line, c.name.Local, v)
			}
			ptr := map[string]**int{
				"length": &f.Length, "minLength": &f.MinLength, "maxLength": &f.MaxLength,
				"totalDigits": &f.TotalDigits, "fractionDigits": &f.FractionDigits,
			}[c.name.Local]
			if *ptr != nil {
				return unsupported(c, "repeated facet "+c.name.Local)
			}
			*ptr = &i
		case "minInclusive", "maxInclusive":
			ptr := &f.MinInclusive
			if c.name.Local == "maxInclusive" {
				ptr = &f.MaxInclusive
			}
			if *ptr != nil {
				return unsupported(c, "repeated facet "+c.name.Local)
			}
			*ptr = &v
		case "whiteSpace":
			if v != "preserve" && v != "replace" && v != "collapse" {
				return fmt.Errorf("%s:%d: bad whiteSpace %q", c.pos.File, c.pos.Line, v)
			}
			f.WhiteSpace = v
		default:
			return unsupported(c, "facet xs:"+c.name.Local)
		}
	}
	return nil
}

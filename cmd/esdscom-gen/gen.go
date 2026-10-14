package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/velcure/esdscom/internal/xsd"
)

// Config selects what the generator produces.
type Config struct {
	// Package is the Go package name of the output.
	Package string
	// Root is the name of the global element that is the document root.
	Root string
	// Extensions lists the national extensions (the XX of
	// SDSComXMLNE_XX.xsd) whose types are generated. Elements typed by an
	// extension that is not listed are left out of the model; the runtime
	// keeps them as unknown content.
	Extensions []string
	// Source names the schema in the header of generated files.
	Source string
}

var extFile = regexp.MustCompile(`NE_([A-Za-z]+)\.xsd$`)

// fileGroup returns the extension a schema file belongs to, or "" for the
// core schema.
func fileGroup(file string) string {
	if m := extFile.FindStringSubmatch(file); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// model is the Go view of the schema that the emitter renders.
type model struct {
	cfg     Config
	schema  *xsd.Schema
	root    *structDef
	structs []*structDef
	simples []*simpleDef

	byGoName   map[string]string // Go type name -> origin, for collisions
	ctStructs  map[*xsd.ComplexType]*structDef
	simpleDefs map[*xsd.SimpleType]*simpleDef
	groups     map[*xsd.Group]*groupBinding
	subs       map[*xsd.Particle]*groupBinding
	pending    []func() error
	// Dropped lists elements left out because their type belongs to an
	// extension that is not generated.
	Dropped []string
}

// structDef is one generated Go struct.
type structDef struct {
	Name       string
	Doc        string
	Group      string // extension, "" for core
	Complex    bool   // the struct of an XSD complex type, with Extra and a descriptor
	XSDName    string // name used in messages
	Attrs      []*attrDef
	Text       *simpleRef // simple content
	Fields     []*fieldDef
	Model      *particleDef
	fieldNames map[string]string
}

type fieldDef struct {
	Name   string
	GoType string
	Doc    string
}

type attrDef struct {
	XMLName string
	Field   string
	Ref     simpleRef
	Default *string
	Doc     string
}

// simpleRef points at a simple type: its Go type and its descriptor
// variable.
type simpleRef struct {
	GoType string
	Desc   string
}

// particleDef is a node of the descriptor tree of a complex type.
type particleDef struct {
	Kind     xsd.ParticleKind // element, sequence or choice
	Min, Max int
	Field    string // empty for a sequence flattened into its parent
	Name     string // element name
	Simple   *simpleRef
	Complex  *structDef
	Any      bool
	Children []*particleDef
}

// simpleDef is one generated simple type.
type simpleDef struct {
	Name     string
	Desc     string
	XSDName  string
	Doc      string
	Group    string
	Prim     string // runtime primitive constant
	Base     string // descriptor of the base, empty for a union
	Members  []string
	Facets   []string // runtime facet constructor calls
	Enums    []enumDef
	Accessor string // primitive accessor method, if any
}

type enumDef struct {
	Const string
	Value string
	Doc   string
}

type groupBinding struct {
	def      *structDef
	children []*particleDef
	kind     xsd.ParticleKind
}

// primitives maps built-in types to runtime primitive constants, Go types
// and descriptor variables.
var primitives = map[string]struct{ prim, goType, desc string }{
	"string":   {"primString", "string", "xsString"},
	"boolean":  {"primBoolean", "Boolean", "xsBoolean"},
	"integer":  {"primInteger", "Integer", "xsInteger"},
	"int":      {"primInt", "Int", "xsInt"},
	"float":    {"primFloat", "Float", "xsFloat"},
	"decimal":  {"primDecimal", "Decimal", "xsDecimal"},
	"date":     {"primDate", "Date", "xsDate"},
	"dateTime": {"primDateTime", "DateTime", "xsDateTime"},
	"anyURI":   {"primAnyURI", "AnyURI", "xsAnyURI"},
}

// reserved holds names the runtime package defines itself.
var reserved = map[string]bool{
	"Boolean": true, "Integer": true, "Int": true, "Float": true, "Decimal": true,
	"Date": true, "DateTime": true, "AnyURI": true, "AnyType": true, "Extra": true,
	"RawElement": true, "RawNode": true, "ValidationError": true, "ValidationErrors": true,
	"Severity": true, "DecodeOption": true, "EncodeOption": true, "Decode": true,
	"Encode": true, "SchemaVersion": true, "Namespace": true,
}

func build(s *xsd.Schema, cfg Config) (*model, error) {
	m := &model{
		cfg:        cfg,
		schema:     s,
		byGoName:   map[string]string{},
		ctStructs:  map[*xsd.ComplexType]*structDef{},
		simpleDefs: map[*xsd.SimpleType]*simpleDef{},
		groups:     map[*xsd.Group]*groupBinding{},
		subs:       map[*xsd.Particle]*groupBinding{},
	}
	root := s.Element(cfg.Root)
	if root == nil {
		return nil, fmt.Errorf("root element %q not found", cfg.Root)
	}
	var def *structDef
	var err error
	switch {
	case root.ComplexType != nil:
		def, err = m.complexStruct(goName(root.Name), root.Name, root.ComplexType, fileGroup(root.Pos.File), root.Doc)
	case s.ComplexType(root.Type) != nil:
		def, err = m.namedComplex(s.ComplexType(root.Type))
	default:
		return nil, fmt.Errorf("root element %q must have a complex type", cfg.Root)
	}
	if err != nil {
		return nil, err
	}
	m.root = def
	for len(m.pending) > 0 {
		next := m.pending[0]
		m.pending = m.pending[1:]
		if err := next(); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(m.structs, func(i, j int) bool { return m.structs[i].Name < m.structs[j].Name })
	sort.SliceStable(m.simples, func(i, j int) bool { return m.simples[i].Name < m.simples[j].Name })
	return m, nil
}

func (m *model) included(group string) bool {
	if group == "" {
		return true
	}
	for _, e := range m.cfg.Extensions {
		if strings.EqualFold(e, group) {
			return true
		}
	}
	return false
}

func (m *model) claim(name, origin string) error {
	if reserved[name] {
		return fmt.Errorf("generated name %s (%s) collides with a runtime name", name, origin)
	}
	if prev, ok := m.byGoName[name]; ok {
		return fmt.Errorf("generated name %s used by %s and %s", name, prev, origin)
	}
	m.byGoName[name] = origin
	return nil
}

func (m *model) newStruct(name, xsdName, doc, group string, complex bool) (*structDef, error) {
	if err := m.claim(name, xsdName); err != nil {
		return nil, err
	}
	d := &structDef{Name: name, XSDName: xsdName, Doc: doc, Group: group, Complex: complex, fieldNames: map[string]string{}}
	m.structs = append(m.structs, d)
	return d, nil
}

func (d *structDef) addField(name, goType, doc, origin string) error {
	if prev, ok := d.fieldNames[name]; ok {
		return fmt.Errorf("struct %s: field %s used by %s and %s", d.Name, name, prev, origin)
	}
	d.fieldNames[name] = origin
	d.Fields = append(d.Fields, &fieldDef{Name: name, GoType: goType, Doc: doc})
	return nil
}

// complexStruct creates the struct of a complex type and binds its
// content.
func (m *model) complexStruct(name, xsdName string, ct *xsd.ComplexType, group, doc string) (*structDef, error) {
	if doc == "" {
		doc = ct.Doc
	}
	d, err := m.newStruct(name, xsdName, doc, group, true)
	if err != nil {
		return nil, err
	}
	return d, m.fillComplex(d, ct, group)
}

// fillComplex binds the attributes and content of ct onto d. The content
// of base types comes first, as XSD defines extension as a sequence of the
// base content followed by the extension's own particle.
func (m *model) fillComplex(d *structDef, ct *xsd.ComplexType, group string) error {
	var chain []*xsd.ComplexType
	for t := ct; ; {
		chain = append([]*xsd.ComplexType{t}, chain...)
		if t.Content != xsd.ComplexExtension {
			break
		}
		base := m.schema.ComplexType(t.Base)
		if !m.included(fileGroup(base.Pos.File)) {
			return fmt.Errorf("%s: base type %s belongs to extension %s, which is not generated",
				d.XSDName, base.Name, fileGroup(base.Pos.File))
		}
		t = base
	}
	if chain[0].Content == xsd.SimpleExtension {
		if len(chain) > 1 {
			return &xsd.UnsupportedError{Pos: ct.Pos, What: "complex content extension of a type with simple content"}
		}
		ref, err := m.simpleRef(ct.Base, nil, d.Name+"Value", group)
		if err != nil {
			return err
		}
		d.Text = &ref
		d.fieldNames["Value"] = "simple content"
	}
	var parts []*xsd.Particle
	for _, t := range chain {
		if err := m.attributes(d, t.Attributes, group); err != nil {
			return err
		}
		if t.Particle != nil {
			parts = append(parts, t.Particle)
		}
	}
	switch len(parts) {
	case 0:
	case 1:
		pd, err := m.bind(d, parts[0], false, group)
		if err != nil {
			return err
		}
		d.Model = pd
	default:
		pd := &particleDef{Kind: xsd.SequenceParticle, Min: 1, Max: 1}
		for _, p := range parts {
			c, err := m.bind(d, p, false, group)
			if err != nil {
				return err
			}
			if c != nil {
				pd.Children = append(pd.Children, c)
			}
		}
		d.Model = pd
	}
	if _, ok := d.fieldNames["Extra"]; ok {
		return fmt.Errorf("struct %s: an element named Extra collides with the Extra field", d.Name)
	}
	return d.finishAttrs()
}

func (m *model) attributes(d *structDef, uses []*xsd.AttributeUse, group string) error {
	for _, u := range uses {
		if u.Attribute == nil {
			if err := m.attributes(d, m.schema.AttributeGroup(u.GroupRef).Attributes, group); err != nil {
				return err
			}
			continue
		}
		a := u.Attribute
		for _, prev := range d.Attrs {
			if prev.XMLName == a.Name {
				return fmt.Errorf("struct %s: duplicate attribute %s", d.Name, a.Name)
			}
		}
		ref, err := m.simpleRef(a.Type, a.SimpleType, d.Name+goName(a.Name), group)
		if err != nil {
			return err
		}
		d.Attrs = append(d.Attrs, &attrDef{XMLName: a.Name, Ref: ref, Default: a.Default, Doc: a.Doc})
	}
	return nil
}

// finishAttrs names attribute fields once all element fields are known,
// so that an attribute sharing its name with an element gets the suffix
// Attr instead of colliding.
func (d *structDef) finishAttrs() error {
	for _, a := range d.Attrs {
		a.Field = goName(a.XMLName)
		if _, ok := d.fieldNames[a.Field]; ok {
			a.Field += "Attr"
		}
		if _, ok := d.fieldNames[a.Field]; ok {
			return fmt.Errorf("struct %s: attribute field %s collides", d.Name, a.Field)
		}
		d.fieldNames[a.Field] = "attribute " + a.XMLName
	}
	return nil
}

// bind maps a particle onto fields of d and returns its descriptor node.
// It returns nil for content left out because it belongs to an extension
// that is not generated.
func (m *model) bind(d *structDef, p *xsd.Particle, inChoice bool, group string) (*particleDef, error) {
	switch p.Kind {
	case xsd.ElementParticle:
		return m.bindElement(d, p, group)
	case xsd.GroupRefParticle:
		return m.bindGroupRef(d, p, inChoice)
	case xsd.SequenceParticle:
		if p.Min == 1 && p.Max == 1 && !inChoice {
			pd := &particleDef{Kind: xsd.SequenceParticle, Min: 1, Max: 1}
			for _, c := range p.Children {
				cd, err := m.bind(d, c, false, group)
				if err != nil {
					return nil, err
				}
				if cd != nil {
					pd.Children = append(pd.Children, cd)
				}
			}
			return pd, nil
		}
		field := m.seqFieldName(p)
		sb, err := m.subStruct(d, p, field, group)
		if err != nil {
			return nil, err
		}
		pd := &particleDef{Kind: xsd.SequenceParticle, Min: p.Min, Max: p.Max, Field: field, Children: sb.children}
		doc := "holds a sequence"
		if p.Max != 1 {
			doc = "holds the repetitions of a sequence"
		}
		if err := d.addField(field, occursType(p.Max, true, sb.def.Name), joinDoc(p.Doc, field+" "+doc+"."), "sequence"); err != nil {
			return nil, err
		}
		return pd, nil
	case xsd.ChoiceParticle:
		field := "Choice"
		for i := 2; ; i++ {
			if _, ok := d.fieldNames[field]; !ok {
				break
			}
			field = fmt.Sprintf("Choice%d", i)
		}
		sb, err := m.subStruct(d, p, field, group)
		if err != nil {
			return nil, err
		}
		pd := &particleDef{Kind: xsd.ChoiceParticle, Min: p.Min, Max: p.Max, Field: field, Children: sb.children}
		if len(sb.children) < len(p.Children) {
			// A left-out alternative may be the one present in a document;
			// it is then kept as unknown content, so no alternative of the
			// generated model is required.
			pd.Min = 0
		}
		doc := field + " holds the alternatives of a choice; exactly one of its fields is set."
		if p.Max != 1 {
			doc = field + " holds the repetitions of a choice; in each, exactly one field is set."
		}
		if err := d.addField(field, occursType(p.Max, inChoice, sb.def.Name), joinDoc(p.Doc, doc), "choice"); err != nil {
			return nil, err
		}
		return pd, nil
	}
	return nil, fmt.Errorf("unexpected particle kind %v", p.Kind)
}

// subStruct returns the struct that holds the content of a nested
// sequence or choice. A particle inherited by several derived types maps
// to one shared struct.
func (m *model) subStruct(d *structDef, p *xsd.Particle, field, group string) (*groupBinding, error) {
	if sb, ok := m.subs[p]; ok {
		return sb, nil
	}
	sub, err := m.newStruct(d.Name+field, d.XSDName+"/"+field, p.Doc, group, false)
	if err != nil {
		return nil, err
	}
	sb := &groupBinding{def: sub, kind: p.Kind}
	m.subs[p] = sb
	for _, c := range p.Children {
		cd, err := m.bind(sub, c, p.Kind == xsd.ChoiceParticle, group)
		if err != nil {
			return nil, err
		}
		if cd != nil {
			sb.children = append(sb.children, cd)
		}
	}
	return sb, nil
}

// occursType returns the Go type of a field by its occurrence: a slice
// for repeated content, a pointer when absence must be representable,
// otherwise the plain type.
func occursType(maxOcc int, pointer bool, t string) string {
	switch {
	case maxOcc != 1:
		return "[]" + t
	case pointer:
		return "*" + t
	}
	return t
}

func joinDoc(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n\n" + b
}

// seqFieldName names a nested sequence after its first element, or after
// the group it starts with.
func (m *model) seqFieldName(p *xsd.Particle) string {
	var first func(p *xsd.Particle) string
	first = func(p *xsd.Particle) string {
		switch p.Kind {
		case xsd.ElementParticle:
			return goName(p.Element.Name) + "Group"
		case xsd.GroupRefParticle:
			return goName(p.Ref.Local)
		}
		for _, c := range p.Children {
			if n := first(c); n != "" {
				return n
			}
		}
		return ""
	}
	if n := first(p); n != "" {
		return n
	}
	return "Sequence"
}

func (m *model) bindElement(d *structDef, p *xsd.Particle, group string) (*particleDef, error) {
	e := p.Element
	pd := &particleDef{Kind: xsd.ElementParticle, Min: p.Min, Max: p.Max, Name: e.Name, Field: goName(e.Name)}
	var goType string
	switch {
	case e.IsAnyType():
		pd.Any = true
		goType = "AnyType"
	case e.ComplexType != nil && m.ctStructs[e.ComplexType] != nil:
		pd.Complex = m.ctStructs[e.ComplexType]
		goType = pd.Complex.Name
	case e.ComplexType != nil:
		sub, err := m.complexStruct(d.Name+goName(e.Name), d.XSDName+"/"+e.Name, e.ComplexType, group, e.Doc)
		if err != nil {
			return nil, err
		}
		m.ctStructs[e.ComplexType] = sub
		pd.Complex = sub
		goType = sub.Name
	case e.SimpleType != nil:
		ref, err := m.simpleRef(xsd.QName{}, e.SimpleType, d.Name+goName(e.Name), group)
		if err != nil {
			return nil, err
		}
		pd.Simple = &ref
		goType = ref.GoType
	default:
		if ct := m.schema.ComplexType(e.Type); ct != nil {
			if !m.included(fileGroup(ct.Pos.File)) {
				m.drop(d, p)
				return nil, nil
			}
			sub, err := m.namedComplex(ct)
			if err != nil {
				return nil, err
			}
			pd.Complex = sub
			goType = sub.Name
		} else {
			if st := m.schema.SimpleType(e.Type); st != nil && !m.included(fileGroup(st.Pos.File)) {
				m.drop(d, p)
				return nil, nil
			}
			ref, err := m.simpleRef(e.Type, nil, "", group)
			if err != nil {
				return nil, err
			}
			pd.Simple = &ref
			goType = ref.GoType
		}
	}
	if err := d.addField(pd.Field, occursType(p.Max, true, goType), e.Doc, "element "+e.Name); err != nil {
		return nil, err
	}
	return pd, nil
}

func (m *model) drop(d *structDef, p *xsd.Particle) {
	m.Dropped = append(m.Dropped, fmt.Sprintf("%s/%s (minOccurs %d, type %s)", d.XSDName, p.Element.Name, p.Min, p.Element.Type.Local))
}

// namedComplex returns the struct of a named complex type, creating it on
// first use. Its content is bound later, which allows recursive types.
func (m *model) namedComplex(ct *xsd.ComplexType) (*structDef, error) {
	if d, ok := m.ctStructs[ct]; ok {
		return d, nil
	}
	group := fileGroup(ct.Pos.File)
	d, err := m.newStruct(goName(ct.Name), ct.Name, ct.Doc, group, true)
	if err != nil {
		return nil, err
	}
	m.ctStructs[ct] = d
	m.pending = append(m.pending, func() error { return m.fillComplex(d, ct, group) })
	return d, nil
}

func (m *model) bindGroupRef(d *structDef, p *xsd.Particle, inChoice bool) (*particleDef, error) {
	g := m.schema.Group(p.Ref)
	group := fileGroup(g.Pos.File)
	if !m.included(group) {
		m.Dropped = append(m.Dropped, fmt.Sprintf("%s/group %s (minOccurs %d)", d.XSDName, g.Name, p.Min))
		return nil, nil
	}
	gp := g.Particle
	if p.Min == 1 && p.Max == 1 && !inChoice && gp.Kind == xsd.SequenceParticle {
		inline := *gp
		return m.bind(d, &inline, false, group)
	}
	gb, err := m.groupStruct(g)
	if err != nil {
		return nil, err
	}
	field := gb.def.Name
	pd := &particleDef{Kind: gb.kind, Min: p.Min, Max: p.Max, Field: field, Children: gb.children}
	pointer := inChoice || p.Min == 0 || gb.kind == xsd.SequenceParticle
	if err := d.addField(field, occursType(p.Max, pointer, gb.def.Name), joinDoc(p.Doc, g.Doc), "group "+g.Name); err != nil {
		return nil, err
	}
	return pd, nil
}

// groupStruct returns the struct shared by all non-inlined references to
// a named model group.
func (m *model) groupStruct(g *xsd.Group) (*groupBinding, error) {
	if gb, ok := m.groups[g]; ok {
		return gb, nil
	}
	group := fileGroup(g.Pos.File)
	def, err := m.newStruct(goName(g.Name), "group "+g.Name, g.Doc, group, false)
	if err != nil {
		return nil, err
	}
	gb := &groupBinding{def: def, kind: g.Particle.Kind}
	m.groups[g] = gb
	for _, c := range g.Particle.Children {
		cd, err := m.bind(def, c, g.Particle.Kind == xsd.ChoiceParticle, group)
		if err != nil {
			return nil, err
		}
		if cd != nil {
			gb.children = append(gb.children, cd)
		}
	}
	return gb, nil
}

// simpleRef resolves a simple type reference, either by name or as an
// anonymous type that gets the name anonName.
func (m *model) simpleRef(q xsd.QName, anon *xsd.SimpleType, anonName, group string) (simpleRef, error) {
	if anon != nil {
		sd, err := m.simple(anon, anonName, group)
		if err != nil {
			return simpleRef{}, err
		}
		return simpleRef{GoType: sd.Name, Desc: sd.Desc}, nil
	}
	if q.IsBuiltin() {
		p, ok := primitives[q.Local]
		if !ok {
			return simpleRef{}, fmt.Errorf("built-in type xs:%s is not a simple type supported here", q.Local)
		}
		return simpleRef{GoType: p.goType, Desc: p.desc}, nil
	}
	st := m.schema.SimpleType(q)
	if st == nil {
		return simpleRef{}, fmt.Errorf("simple type %s not found", q)
	}
	sd, err := m.simple(st, goName(st.Name), fileGroup(st.Pos.File))
	if err != nil {
		return simpleRef{}, err
	}
	return simpleRef{GoType: sd.Name, Desc: sd.Desc}, nil
}

func (m *model) simple(st *xsd.SimpleType, name, group string) (*simpleDef, error) {
	if sd, ok := m.simpleDefs[st]; ok {
		return sd, nil
	}
	if st.Name != "" {
		group = fileGroup(st.Pos.File)
		if !m.included(group) {
			return nil, fmt.Errorf("simple type %s of extension %s is needed but that extension is not generated", st.Name, group)
		}
	}
	xsdName := st.Name
	if xsdName == "" {
		xsdName = "anonymous type of " + name
	}
	if err := m.claim(name, xsdName); err != nil {
		return nil, err
	}
	sd := &simpleDef{Name: name, Desc: "st" + name, XSDName: st.Name, Doc: st.Doc, Group: group}
	m.simpleDefs[st] = sd
	m.simples = append(m.simples, sd)
	if st.Kind == xsd.Union {
		sd.Prim = "primString"
		for _, q := range st.Members {
			ref, err := m.simpleRef(q, nil, "", group)
			if err != nil {
				return nil, err
			}
			sd.Members = append(sd.Members, ref.Desc)
		}
		return sd, nil
	}
	var baseRef simpleRef
	var err error
	if st.BaseType != nil {
		baseRef, err = m.simpleRef(xsd.QName{}, st.BaseType, name+"Base", group)
	} else {
		baseRef, err = m.simpleRef(st.Base, nil, "", group)
	}
	if err != nil {
		return nil, err
	}
	sd.Base = baseRef.Desc
	prim := m.primOf(st)
	sd.Prim = prim
	if err := m.facets(sd, st, prim); err != nil {
		return nil, err
	}
	sd.Accessor = accessorFor(prim)
	return sd, nil
}

// primOf returns the runtime primitive a restriction is ultimately based
// on; unions count as strings.
func (m *model) primOf(st *xsd.SimpleType) string {
	for {
		if st.Kind == xsd.Union {
			return "primString"
		}
		if st.BaseType != nil {
			st = st.BaseType
			continue
		}
		if st.Base.IsBuiltin() {
			return primitives[st.Base.Local].prim
		}
		st = m.schema.SimpleType(st.Base)
	}
}

func accessorFor(prim string) string {
	switch prim {
	case "primBoolean":
		return "Bool() (bool, error)"
	case "primInteger", "primInt":
		return "Int64() (int64, error)"
	case "primFloat", "primDecimal":
		return "Float64() (float64, error)"
	case "primDate", "primDateTime":
		return "Time() (time.Time, error)"
	}
	return ""
}

func (m *model) facets(sd *simpleDef, st *xsd.SimpleType, prim string) error {
	f := st.Facets
	stringy := prim == "primString" || prim == "primAnyURI"
	numeric := prim == "primFloat" || prim == "primDecimal" || prim == "primInteger" || prim == "primInt"
	decimal := prim == "primDecimal" || prim == "primInteger" || prim == "primInt"
	bad := func(facet string) error {
		return &xsd.UnsupportedError{Pos: st.Pos, What: fmt.Sprintf("facet %s on a type derived from %s", facet, strings.TrimPrefix(prim, "prim"))}
	}
	if f.WhiteSpace != "" {
		if !stringy {
			return bad("whiteSpace")
		}
		sd.Facets = append(sd.Facets, fmt.Sprintf("whiteSpace(%s)", map[string]string{"preserve": "wsPreserve", "replace": "wsReplace", "collapse": "wsCollapse"}[f.WhiteSpace]))
	}
	for _, pair := range []struct {
		name string
		v    *int
	}{{"length", f.Length}, {"minLength", f.MinLength}, {"maxLength", f.MaxLength}} {
		if pair.v == nil {
			continue
		}
		if !stringy {
			return bad(pair.name)
		}
		sd.Facets = append(sd.Facets, fmt.Sprintf("%s(%d)", pair.name, *pair.v))
	}
	for _, pair := range []struct {
		name string
		v    *string
	}{{"minInclusive", f.MinInclusive}, {"maxInclusive", f.MaxInclusive}} {
		if pair.v == nil {
			continue
		}
		if !numeric {
			return bad(pair.name)
		}
		sd.Facets = append(sd.Facets, fmt.Sprintf("%s(%q)", pair.name, *pair.v))
	}
	for _, pair := range []struct {
		name string
		v    *int
	}{{"totalDigits", f.TotalDigits}, {"fractionDigits", f.FractionDigits}} {
		if pair.v == nil {
			continue
		}
		if !decimal {
			return bad(pair.name)
		}
		sd.Facets = append(sd.Facets, fmt.Sprintf("%s(%d)", pair.name, *pair.v))
	}
	if len(f.Patterns) > 0 {
		var args []string
		for _, src := range f.Patterns {
			re, err := translatePattern(src)
			if err != nil {
				return fmt.Errorf("%s:%d: %w", st.Pos.File, st.Pos.Line, err)
			}
			args = append(args, fmt.Sprintf("%q, %q", src, re))
		}
		sd.Facets = append(sd.Facets, "pattern("+strings.Join(args, ", ")+")")
	}
	if len(f.Enumerations) > 0 {
		if !stringy {
			return bad("enumeration")
		}
		used := map[string]bool{}
		for _, e := range f.Enumerations {
			c := sd.Name + "_" + enumSuffix(e.Value)
			base := c
			for i := 2; used[c] || m.byGoName[c] != ""; i++ {
				c = fmt.Sprintf("%s%d", base, i)
			}
			used[c] = true
			if err := m.claim(c, "enumeration value of "+sd.Name); err != nil {
				return err
			}
			sd.Enums = append(sd.Enums, enumDef{Const: c, Value: e.Value, Doc: e.Doc})
		}
		sd.Facets = append(sd.Facets, "enumeration(enum"+sd.Name+")")
	}
	return nil
}

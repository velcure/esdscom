package esdscom

import (
	"fmt"
	"reflect"
	"strings"
)

type validator struct {
	out ValidationErrors
}

func (vd *validator) add(sev Severity, path, rule, msg string) {
	vd.out = append(vd.out, ValidationError{Path: path, Rule: rule, Msg: msg, Severity: sev})
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "/" + name
}

func (vd *validator) simple(t *simpleType, v, path string) {
	if x := t.check(v); x != nil {
		vd.add(SeverityError, path, x.rule, x.msg)
	}
}

// complex validates the struct v described by c. path is the path of the
// element, empty for the root.
func (vd *validator) complex(c *complexType, v reflect.Value, path string) {
	for i := range c.attrs {
		a := &c.attrs[i]
		if f := v.Field(a.index); !f.IsNil() {
			vd.simple(a.typ, f.Elem().String(), joinPath(path, "@"+a.name))
		}
	}
	if c.text != nil {
		vd.simple(c.text, v.Field(c.textIndex).String(), path)
	}
	if c.model != nil {
		vd.particle(c.model, v, path)
	}
	extra := v.Field(c.extraIndex).Addr().Interface().(*Extra)
	for _, a := range extra.Attrs {
		if a.Name.Space == xsiNamespace {
			continue
		}
		vd.add(SeverityHint, joinPath(path, "@"+a.Name.Local), "unknown",
			fmt.Sprintf("attribute %s is not described by schema %s and was kept unchanged", a.Name.Local, SchemaVersion))
	}
	for _, n := range extra.Nodes {
		switch {
		case n.Element == nil:
			if strings.TrimSpace(n.Text) != "" {
				vd.add(SeverityError, path, "text", "text is not allowed in element content")
			}
		case n.Element.Name.Space == "" && len(c.byName[n.Element.Name.Local]) > 0:
			vd.add(SeverityError, joinPath(path, n.Element.Name.Local), "unexpected",
				fmt.Sprintf("element %s occurs more often than allowed or at a place where it is not allowed", n.Element.Name.Local))
		default:
			vd.add(SeverityHint, joinPath(path, n.Element.Name.Local), "unknown",
				fmt.Sprintf("element %s is not described by the generated types for schema %s and was kept unchanged", qualifiedName(n.Element.Name.Space, n.Element.Name.Local), SchemaVersion))
		}
	}
}

func qualifiedName(space, local string) string {
	if space == "" {
		return local
	}
	return "{" + space + "}" + local
}

// particle validates the content held for p by the struct v.
func (vd *validator) particle(p *particle, v reflect.Value, path string) {
	if p.kind == pSequence && p.field == "" {
		for _, c := range p.children {
			vd.particle(c, v, path)
		}
		return
	}
	f := v.Field(p.index)
	label := p.label()
	switch f.Kind() {
	case reflect.Slice:
		n := f.Len()
		switch {
		case n < p.min && p.min == 1:
			vd.add(SeverityError, joinPath(path, label), "required", label+" is required")
		case n < p.min:
			vd.add(SeverityError, joinPath(path, label), "minOccurs", fmt.Sprintf("%s occurs %d times, at least %d required", label, n, p.min))
		case p.max != unbounded && n > p.max:
			vd.add(SeverityError, joinPath(path, label), "maxOccurs", fmt.Sprintf("%s occurs %d times, at most %d allowed", label, n, p.max))
		}
		for i := 0; i < n; i++ {
			vd.content(p, f.Index(i), path, i)
		}
	case reflect.Pointer:
		if f.IsNil() {
			if p.min > 0 && p.kind == pElement {
				vd.add(SeverityError, joinPath(path, label), "required", label+" is required")
			}
			return
		}
		vd.content(p, f.Elem(), path, -1)
	default:
		vd.content(p, f, path, -1)
	}
}

// content validates one occurrence of p. i is its index for repeated
// particles, -1 otherwise.
func (vd *validator) content(p *particle, v reflect.Value, path string, i int) {
	switch p.kind {
	case pElement:
		ep := joinPath(path, p.name)
		if i >= 0 {
			ep = fmt.Sprintf("%s[%d]", ep, i)
		}
		switch {
		case p.any:
		case p.complex != nil:
			vd.complex(p.complex, v, ep)
		default:
			vd.simple(p.simple, v.String(), ep)
		}
	case pSequence:
		for _, c := range p.children {
			vd.particle(c, v, path)
		}
	case pChoice:
		var set []*particle
		for _, c := range p.children {
			if isSet(c, v) {
				set = append(set, c)
			}
		}
		switch {
		case len(set) > 1:
			vd.add(SeverityError, path, "choice", fmt.Sprintf("only one of %s may be present, found %s", labels(p.children), labels(set)))
		case len(set) == 0 && (p.min > 0 || i >= 0) && !p.emptiable:
			vd.add(SeverityError, path, "choice", fmt.Sprintf("one of %s is required", labels(p.children)))
		}
		for _, c := range set {
			vd.particle(c, v, path)
		}
	}
}

// isSet reports whether content is present for p in the struct v.
func isSet(p *particle, v reflect.Value) bool {
	if p.kind == pSequence && p.field == "" {
		for _, c := range p.children {
			if isSet(c, v) {
				return true
			}
		}
		return false
	}
	f := v.Field(p.index)
	switch f.Kind() {
	case reflect.Slice:
		return f.Len() > 0
	case reflect.Pointer:
		return !f.IsNil()
	}
	for _, c := range p.children {
		if isSet(c, f) {
			return true
		}
	}
	return false
}

// label names a particle in messages: an element by its name, a
// container by its field.
func (p *particle) label() string {
	if p.kind == pElement {
		return p.name
	}
	return p.field
}

func labels(ps []*particle) string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.describe()
	}
	return strings.Join(names, ", ")
}

// describe names a particle by the elements it starts with.
func (p *particle) describe() string {
	switch {
	case p.kind == pElement:
		return p.name
	case p.kind == pSequence && len(p.children) > 0:
		return "(" + labels(p.children) + ")"
	case p.kind == pChoice:
		return "(" + strings.ReplaceAll(labels(p.children), ", ", " | ") + ")"
	}
	return p.field
}

func reflectValue(f *DatasheetFeed) reflect.Value { return reflect.ValueOf(f).Elem() }

package esdscom

import (
	"fmt"
	"reflect"
)

const unbounded = -1

// complexType is the runtime description of a complex type: which struct
// represents it, its attributes, its simple content and its content model.
// The generated code defines one per complex type.
type complexType struct {
	name   string
	goType reflect.Type
	attrs  []attribute
	text   *simpleType
	model  *particle

	textIndex  int
	extraIndex int
	attrByName map[string]*attribute
	// slots lists the element particles of the content model in schema
	// order; byName indexes them by element name.
	slots  []*slot
	byName map[string][]*slot
}

type attribute struct {
	name  string
	field string
	typ   *simpleType
	index int
}

type particleKind uint8

const (
	pElement particleKind = iota + 1
	pSequence
	pChoice
)

// particle is a node of a content model, bound to the Go field that holds
// its content. A sequence with an empty field is flattened into the
// enclosing struct.
type particle struct {
	kind     particleKind
	name     string
	field    string
	min, max int
	simple   *simpleType
	complex  *complexType
	any      bool
	children []*particle

	index     int
	emptiable bool
	// containers lists the descendant sequences and choices that own a
	// field; their decoding state is reset when a new repetition starts.
	containers []*particle
	// firstSlot is the slot index of the first element below the
	// particle, used to tell choice branches apart.
	firstSlot, lastSlot int
}

// slot is one element particle together with the containers above it.
type slot struct {
	idx  int
	elem *particle
	path []*particle
}

func simpleElem(name, field string, minOcc, maxOcc int, t *simpleType) *particle {
	return &particle{kind: pElement, name: name, field: field, min: minOcc, max: maxOcc, simple: t}
}

func complexElem(name, field string, minOcc, maxOcc int, t *complexType) *particle {
	return &particle{kind: pElement, name: name, field: field, min: minOcc, max: maxOcc, complex: t}
}

func anyElem(name, field string, minOcc, maxOcc int) *particle {
	return &particle{kind: pElement, name: name, field: field, min: minOcc, max: maxOcc, any: true}
}

func sequence(field string, minOcc, maxOcc int, children ...*particle) *particle {
	return &particle{kind: pSequence, field: field, min: minOcc, max: maxOcc, children: children}
}

func choice(field string, minOcc, maxOcc int, children ...*particle) *particle {
	return &particle{kind: pChoice, field: field, min: minOcc, max: maxOcc, children: children}
}

var (
	extraType = reflect.TypeFor[Extra]()
	anyType   = reflect.TypeFor[AnyType]()
)

// define binds the description to its Go struct. It panics when the
// generated description and the generated struct disagree, which the
// tests of this package catch.
func (c *complexType) define(t reflect.Type, attrs []attribute, text *simpleType, model *particle) {
	c.goType = t
	c.attrs = attrs
	c.text = text
	c.model = model
	c.attrByName = map[string]*attribute{}
	c.byName = map[string][]*slot{}
	f, ok := t.FieldByName("Extra")
	if !ok || f.Type != extraType || len(f.Index) != 1 {
		panic(fmt.Sprintf("esdscom: %s has no Extra field", t))
	}
	c.extraIndex = f.Index[0]
	c.textIndex = -1
	if text != nil {
		c.textIndex = fieldIndex(t, "Value", reflect.String)
	}
	for i := range c.attrs {
		a := &c.attrs[i]
		a.index = fieldIndex(t, a.field, reflect.Pointer)
		c.attrByName[a.name] = a
	}
	if model != nil {
		c.bind(model, t, nil)
		c.prepare(model)
	}
}

func fieldIndex(t reflect.Type, name string, kind reflect.Kind) int {
	f, ok := t.FieldByName(name)
	if !ok || len(f.Index) != 1 {
		panic(fmt.Sprintf("esdscom: %s has no field %s", t, name))
	}
	if f.Type.Kind() != kind {
		panic(fmt.Sprintf("esdscom: field %s.%s is %s, want %s", t, name, f.Type.Kind(), kind))
	}
	return f.Index[0]
}

// bind resolves the field of p in the struct type t and records the slots
// of all elements below p.
func (c *complexType) bind(p *particle, t reflect.Type, path []*particle) {
	if p.kind == pSequence && p.field == "" {
		p.index = -1
		p.firstSlot = len(c.slots)
		for _, ch := range p.children {
			c.bind(ch, t, path)
		}
		p.lastSlot = len(c.slots) - 1
		return
	}
	f, ok := t.FieldByName(p.field)
	if !ok || len(f.Index) != 1 {
		panic(fmt.Sprintf("esdscom: %s has no field %s", t, p.field))
	}
	p.index = f.Index[0]
	ft := f.Type
	switch {
	case p.max != 1:
		if ft.Kind() != reflect.Slice {
			panic(fmt.Sprintf("esdscom: field %s.%s must be a slice", t, p.field))
		}
		ft = ft.Elem()
	case ft.Kind() == reflect.Pointer:
		ft = ft.Elem()
	case p.kind != pChoice:
		panic(fmt.Sprintf("esdscom: field %s.%s must be a pointer", t, p.field))
	}
	if p.kind == pElement {
		switch {
		case p.any && ft != anyType,
			p.complex != nil && ft.Kind() != reflect.Struct,
			p.simple != nil && ft.Kind() != reflect.String:
			panic(fmt.Sprintf("esdscom: field %s.%s has type %s", t, p.field, ft))
		}
		s := &slot{idx: len(c.slots), elem: p, path: append([]*particle(nil), path...)}
		p.firstSlot, p.lastSlot = s.idx, s.idx
		c.slots = append(c.slots, s)
		c.byName[p.name] = append(c.byName[p.name], s)
		return
	}
	if ft.Kind() != reflect.Struct {
		panic(fmt.Sprintf("esdscom: field %s.%s must hold a struct", t, p.field))
	}
	p.firstSlot = len(c.slots)
	path = append(path, p)
	for _, ch := range p.children {
		c.bind(ch, ft, path)
	}
	p.lastSlot = len(c.slots) - 1
}

// prepare computes emptiability and the descendant containers.
func (c *complexType) prepare(p *particle) {
	for _, ch := range p.children {
		c.prepare(ch)
		if ch.kind != pElement {
			if ch.field != "" {
				p.containers = append(p.containers, ch)
			}
			p.containers = append(p.containers, ch.containers...)
		}
	}
	p.emptiable = p.min == 0
	switch p.kind {
	case pSequence:
		if !p.emptiable {
			p.emptiable = true
			for _, ch := range p.children {
				if !ch.emptiable {
					p.emptiable = false
				}
			}
		}
	case pChoice:
		for _, ch := range p.children {
			if ch.emptiable {
				p.emptiable = true
			}
		}
	}
}

// branchOf returns the child of the choice ch that contains slot s.
func (p *particle) branchOf(idx int) *particle {
	for _, b := range p.children {
		if idx >= b.firstSlot && idx <= b.lastSlot {
			return b
		}
	}
	return nil
}

package xsd

import "fmt"

// resolve checks that every reference names a known definition or a
// supported built-in type, and that derivations and group references are
// free of cycles.
func (p *parser) resolve() error {
	r := &resolver{s: p.s}
	for _, e := range p.s.Elements {
		if err := r.element(e); err != nil {
			return err
		}
	}
	for _, t := range p.s.ComplexTypes {
		if err := r.complexType(t); err != nil {
			return err
		}
	}
	for _, t := range p.s.SimpleTypes {
		if err := r.simpleType(t); err != nil {
			return err
		}
	}
	for _, g := range p.s.Groups {
		if err := r.particle(g.Particle); err != nil {
			return err
		}
		if err := r.groupCycle(g, map[*Group]bool{}); err != nil {
			return err
		}
	}
	for _, g := range p.s.AttributeGroups {
		if err := r.attrUses(g.Attributes); err != nil {
			return err
		}
		if err := r.attrGroupCycle(g, map[*AttributeGroup]bool{}); err != nil {
			return err
		}
	}
	for _, t := range p.s.ComplexTypes {
		seen := map[*ComplexType]bool{}
		for c := t; c != nil && c.Content == ComplexExtension; c = p.s.ComplexType(c.Base) {
			if seen[c] {
				return fmt.Errorf("%s:%d: circular extension of %s", t.Pos.File, t.Pos.Line, t.Name)
			}
			seen[c] = true
		}
	}
	for _, t := range p.s.SimpleTypes {
		if err := r.simpleCycle(t, map[*SimpleType]bool{}); err != nil {
			return err
		}
	}
	return nil
}

type resolver struct{ s *Schema }

func refErr(pos Pos, kind string, q QName) error {
	return fmt.Errorf("%s:%d: unresolved %s %s", pos.File, pos.Line, kind, q)
}

func (r *resolver) typeRef(pos Pos, q QName, simpleOnly bool) error {
	if q.IsBuiltin() {
		if !Builtins[q.Local] {
			return &UnsupportedError{pos, "built-in type xs:" + q.Local}
		}
		if simpleOnly && q.Local == "anyType" {
			return &UnsupportedError{pos, "xs:anyType where a simple type is required"}
		}
		return nil
	}
	if r.s.SimpleType(q) != nil {
		return nil
	}
	if !simpleOnly && r.s.ComplexType(q) != nil {
		return nil
	}
	return refErr(pos, "type", q)
}

func (r *resolver) element(e *Element) error {
	if !e.Type.IsZero() {
		return r.typeRef(e.Pos, e.Type, false)
	}
	if e.ComplexType != nil {
		return r.complexType(e.ComplexType)
	}
	if e.SimpleType != nil {
		return r.simpleType(e.SimpleType)
	}
	return nil
}

func (r *resolver) complexType(t *ComplexType) error {
	switch t.Content {
	case ComplexExtension:
		if r.s.ComplexType(t.Base) == nil {
			if t.Base.IsBuiltin() || r.s.SimpleType(t.Base) != nil {
				return &UnsupportedError{t.Pos, "complex content extension of simple type " + t.Base.String()}
			}
			return refErr(t.Pos, "base type", t.Base)
		}
	case SimpleExtension:
		if err := r.typeRef(t.Pos, t.Base, true); err != nil {
			if r.s.ComplexType(t.Base) != nil {
				return &UnsupportedError{t.Pos, "simple content extension of complex type " + t.Base.String()}
			}
			return err
		}
	}
	if t.Particle != nil {
		if err := r.particle(t.Particle); err != nil {
			return err
		}
	}
	return r.attrUses(t.Attributes)
}

func (r *resolver) particle(p *Particle) error {
	switch p.Kind {
	case ElementParticle:
		return r.element(p.Element)
	case GroupRefParticle:
		if r.s.Group(p.Ref) == nil {
			return refErr(p.Pos, "group", p.Ref)
		}
	}
	for _, c := range p.Children {
		if err := r.particle(c); err != nil {
			return err
		}
	}
	return nil
}

func (r *resolver) attrUses(us []*AttributeUse) error {
	for _, u := range us {
		if u.Attribute == nil {
			if r.s.AttributeGroup(u.GroupRef) == nil {
				return fmt.Errorf("unresolved attribute group %s", u.GroupRef)
			}
			continue
		}
		a := u.Attribute
		if a.SimpleType != nil {
			if err := r.simpleType(a.SimpleType); err != nil {
				return err
			}
			continue
		}
		if err := r.typeRef(a.Pos, a.Type, true); err != nil {
			return err
		}
	}
	return nil
}

func (r *resolver) simpleType(t *SimpleType) error {
	if t.Kind == Union {
		for _, m := range t.Members {
			if err := r.typeRef(t.Pos, m, true); err != nil {
				return err
			}
		}
		return nil
	}
	if t.BaseType != nil {
		return r.simpleType(t.BaseType)
	}
	return r.typeRef(t.Pos, t.Base, true)
}

func (r *resolver) groupCycle(g *Group, seen map[*Group]bool) error {
	if seen[g] {
		return fmt.Errorf("%s:%d: circular group reference %s", g.Pos.File, g.Pos.Line, g.Name)
	}
	seen[g] = true
	defer delete(seen, g)
	var walk func(p *Particle) error
	walk = func(p *Particle) error {
		if p.Kind == GroupRefParticle {
			return r.groupCycle(r.s.Group(p.Ref), seen)
		}
		for _, c := range p.Children {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(g.Particle)
}

func (r *resolver) attrGroupCycle(g *AttributeGroup, seen map[*AttributeGroup]bool) error {
	if seen[g] {
		return fmt.Errorf("%s:%d: circular attribute group reference %s", g.Pos.File, g.Pos.Line, g.Name)
	}
	seen[g] = true
	defer delete(seen, g)
	for _, u := range g.Attributes {
		if u.Attribute == nil {
			if err := r.attrGroupCycle(r.s.AttributeGroup(u.GroupRef), seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *resolver) simpleCycle(t *SimpleType, seen map[*SimpleType]bool) error {
	if seen[t] {
		return fmt.Errorf("%s:%d: circular simple type derivation %s", t.Pos.File, t.Pos.Line, t.Name)
	}
	seen[t] = true
	defer delete(seen, t)
	var next []*SimpleType
	if t.Kind == Union {
		for _, m := range t.Members {
			if st := r.s.SimpleType(m); st != nil {
				next = append(next, st)
			}
		}
	} else if t.BaseType != nil {
		next = append(next, t.BaseType)
	} else if st := r.s.SimpleType(t.Base); st != nil {
		next = append(next, st)
	}
	for _, n := range next {
		if err := r.simpleCycle(n, seen); err != nil {
			return err
		}
	}
	return nil
}

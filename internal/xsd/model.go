// Package xsd reads the subset of XML Schema 1.0 that the eSDScom schema
// uses into a resolved, in-memory model.
//
// The parser is deliberately strict: every XSD element and attribute it
// meets must be one it understands. Anything else is reported as an
// UnsupportedError with file and line, so that a new schema release using
// a new construct fails at generation time instead of being dropped.
package xsd

// Namespace is the XML Schema namespace.
const Namespace = "http://www.w3.org/2001/XMLSchema"

// Unbounded is the value of Max for maxOccurs="unbounded".
const Unbounded = -1

// QName is a namespace-qualified name.
type QName struct {
	Space string
	Local string
}

// IsBuiltin reports whether q names a type of the XML Schema namespace.
func (q QName) IsBuiltin() bool { return q.Space == Namespace }

// IsZero reports whether q is the empty name.
func (q QName) IsZero() bool { return q.Space == "" && q.Local == "" }

func (q QName) String() string {
	if q.Space == "" {
		return q.Local
	}
	return "{" + q.Space + "}" + q.Local
}

// Pos is a source position.
type Pos struct {
	File string // base name of the schema file
	Line int
}

// Schema is the resolved content of a root schema document and all
// documents it includes.
type Schema struct {
	TargetNamespace string
	Version         string
	// Files lists the base names of all loaded documents in load order.
	Files []string

	Elements        []*Element // global element declarations
	ComplexTypes    []*ComplexType
	SimpleTypes     []*SimpleType
	Groups          []*Group
	AttributeGroups []*AttributeGroup

	complexByName map[string]*ComplexType
	simpleByName  map[string]*SimpleType
	groupByName   map[string]*Group
	attrGrpByName map[string]*AttributeGroup
	elemByName    map[string]*Element
}

// ComplexType returns the named top-level complex type in the target
// namespace, or nil.
func (s *Schema) ComplexType(q QName) *ComplexType {
	if q.Space != s.TargetNamespace {
		return nil
	}
	return s.complexByName[q.Local]
}

// SimpleType returns the named top-level simple type in the target
// namespace, or nil.
func (s *Schema) SimpleType(q QName) *SimpleType {
	if q.Space != s.TargetNamespace {
		return nil
	}
	return s.simpleByName[q.Local]
}

// Group returns the named model group, or nil.
func (s *Schema) Group(q QName) *Group {
	if q.Space != s.TargetNamespace {
		return nil
	}
	return s.groupByName[q.Local]
}

// AttributeGroup returns the named attribute group, or nil.
func (s *Schema) AttributeGroup(q QName) *AttributeGroup {
	if q.Space != s.TargetNamespace {
		return nil
	}
	return s.attrGrpByName[q.Local]
}

// Element returns the named global element, or nil.
func (s *Schema) Element(name string) *Element { return s.elemByName[name] }

// Element is an element declaration, global or local.
type Element struct {
	Name string
	// Type is the referenced type. It is zero when the type is anonymous
	// (ComplexType or SimpleType is set) or when the declaration has no
	// type at all, which in XSD means xs:anyType.
	Type        QName
	ComplexType *ComplexType // anonymous complex type
	SimpleType  *SimpleType  // anonymous simple type
	Doc         string
	Pos         Pos
}

// IsAnyType reports whether the element has no type, which XSD defines
// as xs:anyType.
func (e *Element) IsAnyType() bool {
	return e.Type.IsZero() && e.ComplexType == nil && e.SimpleType == nil ||
		e.Type == QName{Namespace, "anyType"}
}

// ParticleKind distinguishes the particles of a content model.
type ParticleKind int

// Particle kinds.
const (
	ElementParticle ParticleKind = iota + 1
	SequenceParticle
	ChoiceParticle
	GroupRefParticle
)

func (k ParticleKind) String() string {
	switch k {
	case ElementParticle:
		return "element"
	case SequenceParticle:
		return "sequence"
	case ChoiceParticle:
		return "choice"
	case GroupRefParticle:
		return "group"
	}
	return "invalid"
}

// Particle is one term of a content model with its occurrence range.
type Particle struct {
	Kind     ParticleKind
	Min, Max int // Max is Unbounded for maxOccurs="unbounded"
	Element  *Element
	Ref      QName // group reference
	Children []*Particle
	Doc      string
	Pos      Pos
}

// ContentKind classifies the content of a complex type.
type ContentKind int

// Content kinds.
const (
	// ElementContent is a content model of child elements, possibly empty.
	ElementContent ContentKind = iota + 1
	// ComplexExtension extends a complex base type with further particles.
	ComplexExtension
	// SimpleExtension adds attributes to a simple base type.
	SimpleExtension
)

// ComplexType is a complex type definition, named or anonymous.
type ComplexType struct {
	Name    string // empty for anonymous types
	Doc     string
	Pos     Pos
	Content ContentKind
	// Base is the base type of an extension.
	Base QName
	// Particle is the own content model, nil for empty content.
	Particle *Particle
	// Attributes holds the attribute uses in document order.
	Attributes []*AttributeUse
}

// AttributeUse is either an attribute declaration or an attribute group
// reference.
type AttributeUse struct {
	Attribute *Attribute
	GroupRef  QName
}

// Attribute is an attribute declaration.
type Attribute struct {
	Name       string
	Type       QName
	SimpleType *SimpleType // anonymous type
	Default    *string
	Doc        string
	Pos        Pos
}

// SimpleKind distinguishes restriction from union types.
type SimpleKind int

// Simple type varieties.
const (
	Restriction SimpleKind = iota + 1
	Union
)

// SimpleType is a simple type definition, named or anonymous.
type SimpleType struct {
	Name string // empty for anonymous types
	Doc  string
	Pos  Pos
	Kind SimpleKind
	// Base is the base of a restriction; BaseType holds an anonymous base.
	Base     QName
	BaseType *SimpleType
	Facets   Facets
	// Members lists the member types of a union.
	Members []QName
}

// Facets are the constraining facets of one restriction step.
type Facets struct {
	Enumerations   []Enumeration
	Patterns       []string
	Length         *int
	MinLength      *int
	MaxLength      *int
	MinInclusive   *string
	MaxInclusive   *string
	TotalDigits    *int
	FractionDigits *int
	WhiteSpace     string
}

// Enumeration is one allowed value.
type Enumeration struct {
	Value string
	Doc   string
}

// Group is a named model group.
type Group struct {
	Name     string
	Doc      string
	Pos      Pos
	Particle *Particle // a sequence or a choice
}

// AttributeGroup is a named attribute group.
type AttributeGroup struct {
	Name       string
	Pos        Pos
	Attributes []*AttributeUse
}

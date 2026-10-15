package esdscom

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// maxDepth bounds the nesting of decoded elements. The schema nests about
// thirty levels deep; the bound keeps hostile input from exhausting the
// stack.
const maxDepth = 256

const xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"

type decoder struct {
	x      *xml.Decoder
	strict bool
	depth  int
}

// limitReader fails once more than n bytes have been read.
type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n < 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.n {
		p = p[:l.n+1]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	if l.n < 0 {
		return n, ErrTooLarge
	}
	return n, err
}

func decode(r io.Reader, o decodeOptions) (*DatasheetFeed, error) {
	x := xml.NewDecoder(&limitReader{r: r, n: o.maxSize})
	x.Strict = true
	d := &decoder{x: x, strict: o.strict}
	for {
		tok, err := d.token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("esdscom: no root element")
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != Namespace || t.Name.Local != rootName {
				return nil, fmt.Errorf("%w: got {%s}%s, want {%s}%s", ErrRoot, t.Name.Space, t.Name.Local, Namespace, rootName)
			}
			feed := new(DatasheetFeed)
			if err := d.complex(rootType, reflect.ValueOf(feed).Elem(), t, ""); err != nil {
				return nil, err
			}
			if err := d.trailer(); err != nil {
				return nil, err
			}
			return feed, nil
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return nil, fmt.Errorf("esdscom: text before the root element")
			}
		}
	}
}

// trailer reads the rest of the document after the root element.
func (d *decoder) trailer() error {
	for {
		tok, err := d.token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return fmt.Errorf("esdscom: element %s after the root element", t.Name.Local)
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return fmt.Errorf("esdscom: text after the root element")
			}
		}
	}
}

// token returns the next token, rejecting document type declarations and
// other directives.
func (d *decoder) token() (xml.Token, error) {
	tok, err := d.x.Token()
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return nil, ErrTooLarge
		}
		if errors.Is(err, io.EOF) {
			return nil, err
		}
		return nil, fmt.Errorf("esdscom: %w", err)
	}
	if _, ok := tok.(xml.Directive); ok {
		return nil, ErrDTD
	}
	return tok, nil
}

func isNamespaceDecl(a xml.Attr) bool {
	return a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns"
}

// unknown reports content the schema does not describe. In strict mode it
// is an error.
func (d *decoder) unknown(path, what string) error {
	if d.strict {
		return &UnknownError{Path: path, What: what}
	}
	return nil
}

// complex decodes the element start into v, a struct described by c.
func (d *decoder) complex(c *complexType, v reflect.Value, start xml.StartElement, path string) error {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > maxDepth {
		return fmt.Errorf("esdscom: %s: elements nested deeper than %d", path, maxDepth)
	}
	extra := v.Field(c.extraIndex).Addr().Interface().(*Extra)
	for _, a := range start.Attr {
		if isNamespaceDecl(a) {
			continue
		}
		if a.Name.Space == "" {
			if at, ok := c.attrByName[a.Name.Local]; ok {
				f := v.Field(at.index)
				p := reflect.New(f.Type().Elem())
				p.Elem().SetString(a.Value)
				f.Set(p)
				continue
			}
		}
		if a.Name.Space != xsiNamespace || path != "" {
			if err := d.unknown(joinPath(path, "@"+a.Name.Local), "attribute"); err != nil {
				return err
			}
		}
		extra.Attrs = append(extra.Attrs, a)
	}
	st := &bindState{last: map[*particle]int{}, cursor: -1}
	var text strings.Builder
	for {
		tok, err := d.token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("esdscom: unexpected end of document in %s", path)
			}
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := d.child(c, v, st, t, path, extra); err != nil {
				return err
			}
		case xml.CharData:
			if c.text != nil {
				text.Write(t)
				continue
			}
			if len(bytes.TrimSpace(t)) == 0 {
				continue
			}
			if err := d.unknown(path, "text"); err != nil {
				return err
			}
			extra.Nodes = append(extra.Nodes, RawNode{Text: string(t), anchor: st.known + 1})
		case xml.EndElement:
			if c.text != nil {
				v.Field(c.textIndex).SetString(text.String())
			}
			return nil
		}
	}
}

func (d *decoder) child(c *complexType, v reflect.Value, st *bindState, t xml.StartElement, path string, extra *Extra) error {
	var s *slot
	if t.Name.Space == "" {
		s = c.pick(v, st, t.Name.Local)
	}
	if s == nil {
		if err := d.unknown(joinPath(path, t.Name.Local), "element"); err != nil {
			return err
		}
		raw, err := d.raw(t)
		if err != nil {
			return err
		}
		extra.Nodes = append(extra.Nodes, RawNode{Element: raw, anchor: st.known + 1})
		return nil
	}
	target := c.bindSlot(v, st, s, false)
	st.known++
	p := s.elem
	childPath := joinPath(path, p.name)
	switch {
	case p.any:
		a := target.Addr().Interface().(*AnyType)
		return d.anyContent(t, &a.Attrs, &a.Nodes)
	case p.complex != nil:
		return d.complex(p.complex, target, t, childPath)
	default:
		s, err := d.simple(t, childPath)
		if err != nil {
			return err
		}
		target.SetString(s)
		return nil
	}
}

// simple reads the text of an element with a simple type. Attributes and
// child elements cannot be kept on a simple value and are errors.
func (d *decoder) simple(start xml.StartElement, path string) (string, error) {
	for _, a := range start.Attr {
		if !isNamespaceDecl(a) {
			return "", fmt.Errorf("esdscom: %s: attribute %s on an element with simple content", path, a.Name.Local)
		}
	}
	var b strings.Builder
	for {
		tok, err := d.token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", fmt.Errorf("esdscom: unexpected end of document in %s", path)
			}
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			return "", fmt.Errorf("esdscom: %s: element %s inside an element with simple content", path, t.Name.Local)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

// raw reads the element start and its content verbatim.
func (d *decoder) raw(start xml.StartElement) (*RawElement, error) {
	e := &RawElement{Name: start.Name}
	return e, d.anyContent(start, &e.Attrs, &e.Nodes)
}

func (d *decoder) anyContent(start xml.StartElement, attrs *[]xml.Attr, nodes *[]RawNode) error {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > maxDepth {
		return fmt.Errorf("esdscom: elements nested deeper than %d", maxDepth)
	}
	for _, a := range start.Attr {
		if !isNamespaceDecl(a) {
			*attrs = append(*attrs, a)
		}
	}
	for {
		tok, err := d.token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("esdscom: unexpected end of document in %s", start.Name.Local)
			}
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := d.raw(t)
			if err != nil {
				return err
			}
			*nodes = append(*nodes, RawNode{Element: child})
		case xml.CharData:
			*nodes = append(*nodes, RawNode{Text: string(t)})
		case xml.EndElement:
			return nil
		}
	}
}

// bindState is the progress of matching the children of one element
// against its content model.
type bindState struct {
	// cursor is the slot of the last bound element.
	cursor int
	// last holds, per container, the last slot bound in its current
	// repetition.
	last map[*particle]int
	// known counts the bound elements; it anchors unknown content.
	known int
}

// pick selects the slot for an element named name. Matching follows the
// schema order: the first slot at or after the previous one wins, so
// that an element name used at several places of a content model lands at
// the right one. An element out of order is bound to the first slot that
// can still take it; Encode then writes it in schema order.
func (c *complexType) pick(v reflect.Value, st *bindState, name string) *slot {
	cands := c.byName[name]
	for _, s := range cands {
		if s.idx >= st.cursor && c.bindSlot(v, st, s, true).IsValid() {
			return s
		}
	}
	for _, s := range cands {
		if c.bindSlot(v, st, s, true).IsValid() {
			return s
		}
	}
	return nil
}

// bindSlot walks from the struct v down to the field of slot s, creating
// containers and repetitions on the way, and returns the value the
// element decodes into. With dry set it changes nothing and only reports,
// by a valid result, whether the slot can take another element.
func (c *complexType) bindSlot(v reflect.Value, st *bindState, s *slot, dry bool) reflect.Value {
	cur := v
	fresh := false
	for _, p := range s.path {
		f := cur.Field(p.index)
		switch {
		case p.max != 1:
			if fresh || f.Len() == 0 || st.newRepetition(p, s) {
				if dry {
					return reflect.ValueOf(true)
				}
				f.Set(reflect.Append(f, reflect.New(f.Type().Elem()).Elem()))
				for _, d := range p.containers {
					delete(st.last, d)
				}
				delete(st.last, p)
				fresh = true
			}
			cur = f.Index(f.Len() - 1)
		case f.Kind() == reflect.Pointer:
			if f.IsNil() {
				if dry {
					return reflect.ValueOf(true)
				}
				f.Set(reflect.New(f.Type().Elem()))
				fresh = true
			}
			cur = f.Elem()
		default:
			cur = f
		}
	}
	e := s.elem
	f := cur.Field(e.index)
	if e.max != 1 {
		if dry {
			return reflect.ValueOf(true)
		}
		f.Set(reflect.Append(f, reflect.New(f.Type().Elem()).Elem()))
		st.mark(s)
		return f.Index(f.Len() - 1)
	}
	if !f.IsNil() {
		return reflect.Value{}
	}
	if dry {
		return reflect.ValueOf(true)
	}
	f.Set(reflect.New(f.Type().Elem()))
	st.mark(s)
	return f.Elem()
}

func (st *bindState) mark(s *slot) {
	for _, p := range s.path {
		st.last[p] = s.idx
	}
	st.cursor = s.idx
}

// newRepetition reports whether binding slot s into the repeated
// container p starts its next repetition: when s does not continue the
// current repetition in schema order, or, for a choice, when s belongs to
// another branch than the one already taken.
func (st *bindState) newRepetition(p *particle, s *slot) bool {
	l, ok := st.last[p]
	if !ok {
		return false
	}
	switch {
	case s.idx < l:
		return true
	case s.idx == l:
		return s.elem.max == 1
	case p.kind == pChoice:
		return p.branchOf(s.idx) != p.branchOf(l)
	}
	return false
}

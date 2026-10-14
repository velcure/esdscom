package esdscom

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

// wellKnownPrefixes are used for these namespaces when Encode has to
// declare them.
var wellKnownPrefixes = map[string]string{
	Namespace:    "eSDScom",
	xsiNamespace: "xsi",
}

type encoder struct {
	w      *bufio.Writer
	indent string
	depth  int
	// scopes holds the namespace prefixes declared on each open element.
	scopes []map[string]string
	nextNS int
	// open is set while the last start tag still lacks its closing '>',
	// so that an element without content can be written as <name/>.
	open bool
}

// write writes content, closing a pending start tag first. Write errors
// stick to the bufio.Writer and are returned by the final Flush.
func (e *encoder) write(s string) {
	if e.open {
		_ = e.w.WriteByte('>')
		e.open = false
	}
	_, _ = e.w.WriteString(s)
}

func encode(w io.Writer, f *DatasheetFeed, o encodeOptions) error {
	e := &encoder{w: bufio.NewWriter(w), indent: o.indent}
	e.write(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	e.complex(rootType, reflect.ValueOf(f).Elem(), xml.Name{Space: Namespace, Local: rootName})
	e.write("\n")
	return e.w.Flush()
}

// prefix returns the prefix bound to ns, declaring it in decls if it is
// not yet in scope.
func (e *encoder) prefix(ns string, decls map[string]string) string {
	if ns == xmlNamespace {
		return "xml"
	}
	for i := len(e.scopes) - 1; i >= 0; i-- {
		if p, ok := e.scopes[i][ns]; ok {
			return p
		}
	}
	if p, ok := decls[ns]; ok {
		return p
	}
	p, ok := wellKnownPrefixes[ns]
	if !ok || e.prefixTaken(p, decls) {
		for {
			e.nextNS++
			p = fmt.Sprintf("ns%d", e.nextNS)
			if !e.prefixTaken(p, decls) {
				break
			}
		}
	}
	decls[ns] = p
	return p
}

func (e *encoder) prefixTaken(p string, decls map[string]string) bool {
	for _, q := range decls {
		if q == p {
			return true
		}
	}
	for _, s := range e.scopes {
		for _, q := range s {
			if q == p {
				return true
			}
		}
	}
	return false
}

func (e *encoder) newline() {
	if e.indent == "" {
		return
	}
	e.write("\n" + strings.Repeat(e.indent, e.depth))
}

// start writes a start tag with its attributes and opens a namespace
// scope. Namespace declarations among attrs are dropped; the encoder
// declares what the names need.
func (e *encoder) start(name xml.Name, attrs []xml.Attr) {
	decls := map[string]string{}
	var b strings.Builder
	b.WriteByte('<')
	if name.Space != "" {
		b.WriteString(e.prefix(name.Space, decls) + ":")
	}
	b.WriteString(name.Local)
	var rest strings.Builder
	for _, a := range attrs {
		if isNamespaceDecl(a) {
			continue
		}
		rest.WriteByte(' ')
		if a.Name.Space != "" {
			rest.WriteString(e.prefix(a.Name.Space, decls) + ":")
		}
		rest.WriteString(a.Name.Local + `="`)
		escape(&rest, a.Value, true)
		rest.WriteByte('"')
	}
	nss := make([]string, 0, len(decls))
	for ns := range decls {
		nss = append(nss, ns)
	}
	sort.Strings(nss)
	for _, ns := range nss {
		b.WriteString(" xmlns:" + decls[ns] + `="`)
		escape(&b, ns, true)
		b.WriteByte('"')
	}
	b.WriteString(rest.String())
	e.write(b.String())
	e.open = true
	e.scopes = append(e.scopes, decls)
}

func (e *encoder) end(name xml.Name) {
	if e.open {
		_, _ = e.w.WriteString("/>")
		e.open = false
	} else {
		prefix := ""
		if name.Space != "" {
			prefix = e.prefix(name.Space, map[string]string{}) + ":"
		}
		e.write("</" + prefix + name.Local + ">")
	}
	e.scopes = e.scopes[:len(e.scopes)-1]
}

func (e *encoder) complex(c *complexType, v reflect.Value, name xml.Name) {
	extra := v.Field(c.extraIndex).Addr().Interface().(*Extra)
	attrs := make([]xml.Attr, 0, len(c.attrs)+len(extra.Attrs))
	for i := range c.attrs {
		f := v.Field(c.attrs[i].index)
		if !f.IsNil() {
			attrs = append(attrs, xml.Attr{Name: xml.Name{Local: c.attrs[i].name}, Value: f.Elem().String()})
		}
	}
	attrs = append(attrs, extra.Attrs...)
	e.start(name, attrs)

	if c.text != nil {
		var b strings.Builder
		escape(&b, v.Field(c.textIndex).String(), false)
		e.write(b.String())
		e.rawNodes(extra.Nodes)
		e.end(name)
		return
	}

	// Indentation would add text to mixed content, so an element that
	// holds stray text is written without it.
	saved := e.indent
	for _, n := range extra.Nodes {
		if n.Element == nil {
			e.indent = ""
		}
	}
	e.depth++
	w := &extraWriter{e: e, nodes: orderedExtras(extra.Nodes)}
	w.flush(0)
	known := 0
	if c.model != nil {
		e.particle(c.model, v, func(p *particle, val reflect.Value) {
			e.newline()
			e.element(p, val)
			known++
			w.flush(known)
		})
	}
	w.rest()
	e.depth--
	if known > 0 || w.wroteElem {
		e.newline()
	}
	e.indent = saved
	e.end(name)
}

// orderedExtras returns the nodes in the order Encode writes them:
// decoded nodes by their anchor, then nodes added by the caller.
func orderedExtras(nodes []RawNode) []RawNode {
	out := append([]RawNode(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].anchor, out[j].anchor
		if a == 0 {
			return false
		}
		return b == 0 || a < b
	})
	return out
}

type extraWriter struct {
	e         *encoder
	nodes     []RawNode
	next      int
	wroteElem bool
}

// flush writes the nodes anchored after the known-th known element.
func (w *extraWriter) flush(known int) {
	for w.next < len(w.nodes) && w.nodes[w.next].anchor != 0 && w.nodes[w.next].anchor <= known+1 {
		w.write(w.nodes[w.next])
		w.next++
	}
}

func (w *extraWriter) rest() {
	for ; w.next < len(w.nodes); w.next++ {
		w.write(w.nodes[w.next])
	}
}

func (w *extraWriter) write(n RawNode) {
	if n.Element != nil {
		w.e.newline()
		w.wroteElem = true
	}
	w.e.rawNodes([]RawNode{n})
}

// particle calls emit for every element of the content model held by
// the struct v, in schema order.
func (e *encoder) particle(p *particle, v reflect.Value, emit func(*particle, reflect.Value)) {
	if p.kind == pSequence && p.field == "" {
		for _, c := range p.children {
			e.particle(c, v, emit)
		}
		return
	}
	f := v.Field(p.index)
	var items []reflect.Value
	switch {
	case f.Kind() == reflect.Slice:
		for i := 0; i < f.Len(); i++ {
			items = append(items, f.Index(i))
		}
	case f.Kind() == reflect.Pointer:
		if !f.IsNil() {
			items = append(items, f.Elem())
		}
	default:
		items = append(items, f)
	}
	for _, it := range items {
		if p.kind == pElement {
			emit(p, it)
			continue
		}
		for _, c := range p.children {
			e.particle(c, it, emit)
		}
	}
}

func (e *encoder) element(p *particle, v reflect.Value) {
	name := xml.Name{Local: p.name}
	switch {
	case p.any:
		a := v.Addr().Interface().(*AnyType)
		e.rawElement(&RawElement{Name: name, Attrs: a.Attrs, Nodes: a.Nodes})
	case p.complex != nil:
		e.complex(p.complex, v, name)
	default:
		e.start(name, nil)
		var b strings.Builder
		escape(&b, v.String(), false)
		e.write(b.String())
		e.end(name)
	}
}

func (e *encoder) rawNodes(nodes []RawNode) {
	for _, n := range nodes {
		if n.Element != nil {
			e.rawElement(n.Element)
			continue
		}
		var b strings.Builder
		escape(&b, n.Text, false)
		e.write(b.String())
	}
}

// rawElement writes verbatim content, without indentation, so that its
// text is written back exactly.
func (e *encoder) rawElement(r *RawElement) {
	e.start(r.Name, r.Attrs)
	e.rawNodes(r.Nodes)
	e.end(r.Name)
}

// escape writes s with the XML special characters escaped. Characters
// that XML 1.0 does not allow are replaced by U+FFFD. In attribute values
// whitespace other than the space is escaped as well, so that attribute
// value normalization does not change it.
func escape(b *strings.Builder, s string, attr bool) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"' && attr:
			b.WriteString("&quot;")
		case r == '\r':
			b.WriteString("&#xD;")
		case r == '\n' && attr:
			b.WriteString("&#xA;")
		case r == '\t' && attr:
			b.WriteString("&#x9;")
		case r == utf8.RuneError && size == 1, !isXMLChar(r):
			b.WriteRune('�')
		default:
			b.WriteRune(r)
		}
	}
}

func isXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

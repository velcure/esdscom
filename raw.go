package esdscom

import "encoding/xml"

// Extra holds content of an element that the generated types do not
// describe: attributes and child elements of a newer schema version or of
// a national extension this package does not generate, misplaced or
// surplus elements, and stray text. Decode fills it, Encode writes it back
// at the position it was read from, and Validate reports it.
type Extra struct {
	Attrs []xml.Attr
	Nodes []RawNode
}

// Empty reports whether e holds nothing.
func (e *Extra) Empty() bool { return len(e.Attrs) == 0 && len(e.Nodes) == 0 }

// RawNode is an element or a piece of character data kept verbatim.
// Exactly one of Element and Text is meaningful: Text is used when Element
// is nil.
type RawNode struct {
	Element *RawElement
	Text    string

	// anchor is one more than the number of schema-described sibling
	// elements that preceded the node in the decoded document. Zero means
	// the node was not decoded and is written after all known content.
	anchor int
}

// RawElement is an XML element kept verbatim with its attributes and
// content. Namespace declarations are not kept; Encode declares the
// namespaces the names need.
type RawElement struct {
	Name  xml.Name
	Attrs []xml.Attr
	Nodes []RawNode
}

// AnyType is the content of an element declared without a type, which XML
// Schema defines as xs:anyType: any attributes and any mixed content. In
// eSDScom such elements are markers like NewLine or placeholders for
// national legislation.
type AnyType struct {
	Attrs []xml.Attr
	Nodes []RawNode
}

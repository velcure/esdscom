// Package esdscom reads, writes and validates safety data sheets in the
// eSDScom exchange format (XML schema eSDSxml, formerly SDScomXML),
// schema version 5.5, including the national extensions EU and DE.
//
// The types of this package are generated from the official XML schema
// by cmd/esdscom-gen. Each complex type becomes a struct whose fields
// follow the schema order:
//
//   - an element that occurs at most once is a pointer, so that an absent
//     element differs from an empty one;
//   - an element that may repeat is a slice;
//   - a choice is a struct with one field per alternative, of which
//     exactly one is set;
//   - simple values are string types holding the lexical form, with
//     constants for enumerations and a Valid method.
//
// Decode is tolerant: content the generated types do not describe, such
// as elements of a newer schema version or of a national extension other
// than EU and DE, is kept in the Extra field of the enclosing struct and
// written back by Encode. Validate checks a feed against the rules of the
// schema and returns every violation with its path.
//
// The package has no dependencies outside the standard library, makes no
// network requests and does not resolve phrase codes.
package esdscom

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaxSize is the default limit of the input size of Decode.
const DefaultMaxSize = 64 << 20

var (
	// ErrTooLarge is returned by Decode when the input exceeds the size
	// limit.
	ErrTooLarge = errors.New("esdscom: input exceeds the size limit")
	// ErrDTD is returned by Decode for a document with a document type
	// declaration. eSDScom documents have none, and refusing them rules out
	// entity expansion attacks.
	ErrDTD = errors.New("esdscom: document type declarations are not allowed")
	// ErrRoot is returned by Decode when the root element is not
	// DatasheetFeed in the eSDScom namespace.
	ErrRoot = errors.New("esdscom: unexpected root element")
)

// UnknownError is returned by Decode in strict mode for content the
// generated types do not describe.
type UnknownError struct {
	Path string // path of the content below the root, e.g. "Datasheet/Foo"
	What string // "element", "attribute" or "text"
}

func (e *UnknownError) Error() string {
	return fmt.Sprintf("esdscom: unknown %s at %s", e.What, e.Path)
}

type decodeOptions struct {
	maxSize int64
	strict  bool
}

// DecodeOption configures Decode.
type DecodeOption func(*decodeOptions)

// WithMaxSize sets the maximum number of bytes Decode reads. Larger input
// fails with ErrTooLarge. The default is DefaultMaxSize.
func WithMaxSize(n int64) DecodeOption {
	return func(o *decodeOptions) { o.maxSize = n }
}

// DecodeStrict makes content that the generated types do not describe an
// error instead of keeping it in Extra.
func DecodeStrict() DecodeOption {
	return func(o *decodeOptions) { o.strict = true }
}

// Decode reads a DatasheetFeed, the root element of an eSDScom document in
// the namespace http://www.esdscom.eu/eSDScom.
//
// Decode does not check the content against the schema beyond what it
// needs to place it in the generated types; use Validate for that.
// Elements are matched to the schema by name and order. An element that
// is known but out of order is placed into its field, so Encode writes it
// in schema order. Surplus and unknown content goes to Extra.
func Decode(r io.Reader, opts ...DecodeOption) (*DatasheetFeed, error) {
	o := decodeOptions{maxSize: DefaultMaxSize}
	for _, opt := range opts {
		opt(&o)
	}
	return decode(r, o)
}

type encodeOptions struct {
	indent string
}

// EncodeOption configures Encode.
type EncodeOption func(*encodeOptions)

// WithIndent sets the string used to indent nested elements. The default
// is two spaces; the empty string writes everything on one line.
func WithIndent(indent string) EncodeOption {
	return func(o *encodeOptions) { o.indent = indent }
}

// Encode writes f as a UTF-8 eSDScom document. The root element carries
// the eSDScom namespace, elements are written in schema order, and Extra
// content is written back where it was read. When
// InformationFromExportingSystem has no SdscomVersionNo, Encode writes
// SchemaVersion there; f itself is not changed.
func Encode(w io.Writer, f *DatasheetFeed, opts ...EncodeOption) error {
	if f == nil {
		return errors.New("esdscom: Encode of a nil feed")
	}
	o := encodeOptions{indent: "  "}
	for _, opt := range opts {
		opt(&o)
	}
	if info := f.InformationFromExportingSystem; info != nil && info.SdscomVersionNo == nil {
		cf, ci := *f, *info
		ci.SdscomVersionNo = Ptr(SdscomVersionEnum(SchemaVersion))
		cf.InformationFromExportingSystem = &ci
		f = &cf
	}
	return encode(w, f, o)
}

// Severity grades a ValidationError.
type Severity int

const (
	// SeverityError marks a violation of the schema.
	SeverityError Severity = iota
	// SeverityHint marks content that is kept but not checked, such as
	// elements of a national extension this package does not generate.
	SeverityHint
)

func (s Severity) String() string {
	if s == SeverityHint {
		return "hint"
	}
	return "error"
}

// ValidationError is one finding of Validate.
type ValidationError struct {
	// Path locates the finding below the root, e.g.
	// "Datasheet[0]/Identification/TradeName[1]". Attributes are written as
	// "@Name".
	Path string
	// Rule names the violated rule: "required", "minOccurs", "maxOccurs",
	// "choice", "enum", "union", "pattern", "length", "minLength",
	// "maxLength", "minInclusive", "maxInclusive", "totalDigits",
	// "fractionDigits", "type", "unexpected", "text" or, for hints,
	// "unknown".
	Rule     string
	Msg      string
	Severity Severity
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s (%s)", e.Severity, e.Path, e.Msg, e.Rule)
}

// ValidationErrors is the list of findings of Validate, in document order.
type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	var b strings.Builder
	for i, e := range v {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.Error())
	}
	return b.String()
}

// Errors returns the findings of severity SeverityError.
func (v ValidationErrors) Errors() ValidationErrors { return v.filter(SeverityError) }

// Hints returns the findings of severity SeverityHint.
func (v ValidationErrors) Hints() ValidationErrors { return v.filter(SeverityHint) }

// Valid reports whether v holds no finding of severity SeverityError.
func (v ValidationErrors) Valid() bool { return len(v.Errors()) == 0 }

func (v ValidationErrors) filter(s Severity) ValidationErrors {
	var out ValidationErrors
	for _, e := range v {
		if e.Severity == s {
			out = append(out, e)
		}
	}
	return out
}

// Validate checks f against the rules of the schema and returns all
// findings, not only the first: missing required elements, occurrence
// limits, choices with no or several alternatives, and the facets of
// simple values. Content kept in Extra is reported as a hint when it is
// unknown and as an error when it is a known element in a place where the
// schema does not allow it. Validate returns nil when there is nothing to
// report.
func (f *DatasheetFeed) Validate() ValidationErrors {
	vd := &validator{}
	vd.complex(rootType, reflectValue(f), "")
	return vd.out
}

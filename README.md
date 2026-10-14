# esdscom

[![CI](https://github.com/velcure/esdscom/actions/workflows/ci.yml/badge.svg)](https://github.com/velcure/esdscom/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/velcure/esdscom.svg)](https://pkg.go.dev/github.com/velcure/esdscom)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/velcure/esdscom/badge)](https://scorecard.dev/viewer/?uri=github.com/velcure/esdscom)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Read, write and validate safety data sheets in the **eSDScom** exchange
format (XML schema eSDSxml, formerly SDScomXML) with Go.

- Go types for the whole schema, generated from the official XSD, including
  the national extensions **EU** and **DE**
- `Decode` keeps content it does not know, `Encode` writes it back
- `Validate` reports every schema violation with its path, not only the first
- pure Go, standard library only, no cgo, no network access

## Status

Version 0.x. The API may change until the first production use; see
[CHANGELOG.md](CHANGELOG.md).

| Schema release | National extensions with Go types | Package version |
|---|---|---|
| eSDScom v5.5 | EU, DE | v0.x |

Content of the other national extensions (AR, AT, BR, CA, CH, DK, GB, JP,
MX, NO, RU, TR, US) is kept unchanged and reported as a hint by `Validate`.
The generator can produce their types as well.

## Installation

```sh
go get github.com/velcure/esdscom
```

## Usage

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/velcure/esdscom"
)

func main() {
	f, err := os.Open("sds.xml")
	if err != nil {
		log.Fatal(err)
	}
	feed, err := esdscom.Decode(f) // esdscom.WithMaxSize, esdscom.DecodeStrict
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	for _, e := range feed.Validate() {
		// e.g. "error: Datasheet[0]/Composition/RegionEU/Component[0]/Substance/CasNo:
		//       "67-63-X" does not match the pattern ... (pattern)"
		fmt.Println(e)
	}

	for _, c := range feed.Datasheet[0].Composition.RegionEU.Choice.Component {
		fmt.Println(*c.Substance.CasNo)
	}

	if err := esdscom.Encode(os.Stdout, feed); err != nil {
		log.Fatal(err)
	}
}
```

### How the schema maps to Go

| XSD | Go |
|---|---|
| complex type | struct, fields in schema order, plus `Extra` for unknown content |
| element, at most once | pointer (`*T`); `nil` means absent |
| element, repeated | slice (`[]T`) |
| choice | struct with one field per alternative; exactly one is set |
| optional or repeated sequence | pointer to or slice of a struct |
| named group | struct of the group's name |
| extension | the base type's fields followed by the extension's |
| simple type | `string` type with constants for enumerations and `Valid()` |
| `xs:boolean`, `xs:float`, `xs:date` … | `Boolean`, `Float`, `Date` …, with `Bool()`, `Float64()`, `Time()` |
| element without type (`xs:anyType`) | `AnyType`, kept verbatim |

Simple values keep their lexical form, so a malformed value never stops
`Decode`; `Validate` reports it. `esdscom.Ptr` builds pointers to values.

### Decode

- tolerant by default: unknown elements, attributes and text go to the
  `Extra` field of the enclosing struct, and `Encode` writes them back at the
  same position; `DecodeStrict()` makes them an error
- elements out of schema order are placed into their fields; `Encode` writes
  them in schema order
- input is limited to 64 MiB by default (`WithMaxSize`)
- documents with a document type declaration are rejected; no external
  entities or DTDs are ever loaded

### Encode

`Encode` writes UTF-8 with the eSDScom namespace on the root element and all
elements in schema order. When `InformationFromExportingSystem` has no
`SdscomVersionNo`, it writes `SchemaVersion` there.

### Validate

`Validate` checks required elements, occurrence limits, choices and all
facets of simple types (enumerations, patterns, lengths, ranges, digits) and
returns every finding with a path like
`Datasheet[0]/Identification/TradeName[1]` and a rule name (`required`,
`enum`, `pattern`, `maxLength`, `choice`, …). Unknown content kept in `Extra`
is a hint (`SeverityHint`), not an error. The tests compare `Validate` with
`xmllint --schema` on valid and deliberately broken documents.

Phrase codes are passed through; the phrase catalogue (eSDSphrac) is not
part of this package.

## Code generation

The types are generated from the vendored schema by
[`cmd/esdscom-gen`](cmd/esdscom-gen) and committed:

```sh
go generate ./...
```

The generator supports exactly the XSD constructs the schema uses and stops
at any other construct. See [docs/schema-update.md](docs/schema-update.md)
for moving to a new schema release.

## License

The code of this repository is licensed under the
[Apache License 2.0](LICENSE).

The eSDScom XML schema in [`schema/`](schema) is © the eSDScom project and
licensed under [CC BY-ND 4.0](https://creativecommons.org/licenses/by-nd/4.0/legalcode).
It is included unchanged; see [NOTICE](NOTICE) and
[schema/v5.5/NOTICE](schema/v5.5/NOTICE).

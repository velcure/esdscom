# Moving to a new schema release

The Go types follow one eSDScom schema release (`SchemaVersion`). A new
release is adopted as follows:

1. Copy the XSD files of the release tag of
   https://github.com/esdscom/sdscom-xml unchanged into `schema/vX.Y/` and
   add a `NOTICE` with source, tag, commit and licence, like
   `schema/v5.5/NOTICE`.
2. Point the `go:generate` line in `generate.go` and the constant
   `schemaFile` in `esdscom_test.go` to the new directory.
3. Run `go generate ./...`. The generator stops at XSD constructs it does
   not support; such a construct is added to `internal/xsd` and
   `cmd/esdscom-gen` together with a golden test.
4. Update the test documents (`SdscomVersionNo`, changed elements) and run
   `go test ./...`, including the `xmllint` cross-check.
5. The release is a minor version, or a major version when generated types
   change incompatibly.

National extensions are selected with `-ext` in `generate.go`. Adding a
country, for example `-ext EU,DE,AT`, creates `types_at_gen.go`; elements of
that extension then have Go types instead of being kept in `Extra`.

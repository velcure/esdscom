# Contributing

Thank you for helping with esdscom. This document describes how the project
is developed.

## Development

Requirements: Go (see `go.mod`), and `xmllint` from libxml2 for the schema
cross-check (`apt install libxml2-utils`; macOS ships it). Without
`xmllint` the cross-check tests are skipped locally; CI always runs them.

```sh
go test ./...                  # all tests
go test -race ./...            # with the race detector
go vet ./...
golangci-lint run              # configuration in .golangci.yml
go test -run '^$' -fuzz FuzzDecode -fuzztime 60s .
```

## Generated code

`types_gen.go`, `types_eu_gen.go`, `types_de_gen.go` and `validate_gen.go`
are generated from `schema/` by `cmd/esdscom-gen`. Do not edit them; change
the generator and run

```sh
go generate ./...
```

CI fails when the committed files differ from a fresh generation. Changes to
the generator come with a golden test in `cmd/esdscom-gen/testdata/golden`
(`go test ./cmd/esdscom-gen -run Golden -update` rewrites the golden files).

## Tests and test data

`testdata/valid` holds schema-valid documents, `testdata/invalid` documents
with one deliberate error each and an `<!-- expect: rule path -->` comment
that names the finding `Validate` must report. All test documents are
written by hand with fictitious data. Supplier safety data sheets are added
only with the supplier's permission and without personal data.

## Commits

- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat: …`, `fix: …`, `docs: …`, `test: …`, `ci: …`, `chore: …`; a breaking
  change carries `!` or a `BREAKING CHANGE:` footer. release-please derives
  versions and the changelog from them.
- Every commit carries a `Signed-off-by` line (Developer Certificate of
  Origin, https://developercertificate.org/); `git commit -s` adds it.
- Commits on `main` are signed (`git commit -S`).
- Pull requests are merged by squash; the pull request title becomes the
  commit message and therefore follows Conventional Commits as well.

## Code comments

Comments describe what the code does and why it is built that way.

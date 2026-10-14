// Command esdscom-gen generates the Go types and validation rules of the
// esdscom package from the eSDScom XML schema.
//
// Usage:
//
//	esdscom-gen -schema schema/v5.5/SDSComXML.xsd -ext EU,DE -out .
//
// The generator understands exactly the XSD constructs the eSDScom schema
// uses and stops with an error at any other construct, so that a schema
// release with new constructs cannot be generated incompletely. Output is
// deterministic: the same schema always yields byte-identical files.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/velcure/esdscom/internal/xsd"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "esdscom-gen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("esdscom-gen", flag.ContinueOnError)
	schemaPath := fs.String("schema", "", "path of the root schema document")
	root := fs.String("root", "DatasheetFeed", "name of the root element")
	ext := fs.String("ext", "", "comma-separated national extensions to generate, e.g. EU,DE")
	pkg := fs.String("pkg", "esdscom", "Go package name")
	out := fs.String("out", ".", "output directory")
	verbose := fs.Bool("v", false, "list elements left out because their extension is not generated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" {
		return fmt.Errorf("-schema is required")
	}
	var exts []string
	for _, e := range strings.Split(*ext, ",") {
		if e = strings.TrimSpace(e); e != "" {
			exts = append(exts, strings.ToUpper(e))
		}
	}
	files, dropped, err := generate(*schemaPath, Config{
		Package:    *pkg,
		Root:       *root,
		Extensions: exts,
		Source:     filepath.ToSlash(*schemaPath),
	})
	if err != nil {
		return err
	}
	if *verbose {
		for _, d := range dropped {
			fmt.Fprintln(os.Stderr, "left out:", d)
		}
	}
	return writeFiles(*out, files)
}

// generate parses the schema and renders all output files.
func generate(schemaPath string, cfg Config) (map[string][]byte, []string, error) {
	s, err := xsd.ParseFile(schemaPath)
	if err != nil {
		return nil, nil, err
	}
	if s.Version != "" {
		cfg.Source += " (version " + s.Version + ")"
	}
	m, err := build(s, cfg)
	if err != nil {
		return nil, nil, err
	}
	files, err := m.output()
	return files, m.Dropped, err
}

// writeFiles writes the generated files and removes generated type files
// of extensions that are no longer selected.
func writeFiles(dir string, files map[string][]byte) error {
	old, err := filepath.Glob(filepath.Join(dir, "types_*gen.go"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if _, keep := files[filepath.Base(f)]; !keep {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), files[n], 0o644); err != nil {
			return err
		}
	}
	return nil
}

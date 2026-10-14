package esdscom

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const schemaFile = "schema/v5.5/SDSComXML.xsd"

func testFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", dir, "*.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no test files in testdata/%s", dir)
	}
	return files
}

func decodeFile(t *testing.T, path string, opts ...DecodeOption) *DatasheetFeed {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	feed, err := Decode(f, opts...)
	if err != nil {
		t.Fatalf("Decode %s: %v", path, err)
	}
	return feed
}

func encodeFeed(t *testing.T, feed *DatasheetFeed, opts ...EncodeOption) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, feed, opts...); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return buf.Bytes()
}

// xmllint validates a document against the schema with libxml2. It skips
// the test when xmllint is not installed, unless ESDSCOM_REQUIRE_XMLLINT is
// set, as in the cross-check job of CI.
func xmllint(t *testing.T, doc []byte) (bool, string) {
	t.Helper()
	bin, err := exec.LookPath("xmllint")
	if err != nil {
		if os.Getenv("ESDSCOM_REQUIRE_XMLLINT") != "" {
			t.Fatal("xmllint is required (ESDSCOM_REQUIRE_XMLLINT is set) but not installed")
		}
		t.Skip("xmllint not found: install libxml2 (xmllint) to run the schema cross-check; CI runs it")
	}
	cmd := exec.Command(bin, "--noout", "--nonet", "--schema", schemaFile, "-")
	cmd.Stdin = bytes.NewReader(doc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 3 {
			return false, string(out)
		}
		t.Fatalf("xmllint: %v\n%s", err, out)
	}
	return true, string(out)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestRoundTrip checks that Decode, Encode and Decode again yield the
// same tree for every test document, and that Encode is stable.
func TestRoundTrip(t *testing.T) {
	files := append(testFiles(t, "valid"), testFiles(t, "invalid")...)
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			first := decodeFile(t, path)
			out := encodeFeed(t, first)
			second, err := Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("Decode of encoded output: %v\n%s", err, out)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("tree changed by the round trip; encoded:\n%s", out)
			}
			if again := encodeFeed(t, second); !bytes.Equal(out, again) {
				t.Fatalf("second Encode differs:\n%s\n---\n%s", out, again)
			}
			compact := encodeFeed(t, first, WithIndent(""))
			third, err := Decode(bytes.NewReader(compact))
			if err != nil {
				t.Fatalf("Decode of compact output: %v", err)
			}
			if !reflect.DeepEqual(first, third) {
				t.Fatalf("tree changed by the compact round trip:\n%s", compact)
			}
		})
	}
}

// TestEncodeIsSchemaValid checks the output of Encode for every valid test
// document with xmllint.
func TestEncodeIsSchemaValid(t *testing.T) {
	for _, path := range testFiles(t, "valid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			out := encodeFeed(t, decodeFile(t, path))
			if ok, msg := xmllint(t, out); !ok {
				t.Fatalf("encoded output is not schema-valid:\n%s\n%s", msg, out)
			}
		})
	}
}

var expectRE = regexp.MustCompile(`<!-- expect: (\S+) (\S+) -->`)

// TestValidateAgreesWithXmllint compares Validate with xmllint on valid
// and deliberately broken documents.
func TestValidateAgreesWithXmllint(t *testing.T) {
	for _, dir := range []string{"valid", "invalid"} {
		for _, path := range testFiles(t, dir) {
			t.Run(dir+"/"+filepath.Base(path), func(t *testing.T) {
				doc := readFile(t, path)
				lintOK, lintMsg := xmllint(t, doc)
				feed := decodeFile(t, path)
				findings := feed.Validate()
				if lintOK != findings.Valid() {
					t.Fatalf("xmllint valid=%v, Validate valid=%v\nxmllint: %s\nValidate:\n%v", lintOK, findings.Valid(), lintMsg, findings)
				}
				if lintOK != (dir == "valid") {
					t.Fatalf("xmllint valid=%v for a file in %s: %s", lintOK, dir, lintMsg)
				}
				m := expectRE.FindSubmatch(doc)
				if dir == "valid" {
					if m != nil {
						t.Fatal("valid file with an expect comment")
					}
					return
				}
				if m == nil {
					t.Fatal("invalid file without an expect comment")
				}
				rule, wantPath := string(m[1]), string(m[2])
				for _, e := range findings.Errors() {
					if e.Rule == rule && e.Path == wantPath {
						return
					}
				}
				t.Fatalf("no finding %s at %s; got:\n%v", rule, wantPath, findings)
			})
		}
	}
}

func TestValidateReportsUnknownContentAsHints(t *testing.T) {
	feed := decodeFile(t, "testdata/valid/national-extensions.xml")
	findings := feed.Validate()
	if !findings.Valid() {
		t.Fatalf("unexpected errors: %v", findings.Errors())
	}
	var paths []string
	for _, h := range findings.Hints() {
		if h.Rule != "unknown" {
			t.Errorf("hint with rule %s", h.Rule)
		}
		paths = append(paths, h.Path)
	}
	want := []string{
		"Datasheet[0]/RegulatoryInformation/RegionEU/NationalLegislation/NationalLegislationAustria",
		"Datasheet[0]/RegulatoryInformation/RegionEU/NationalLegislation/NationalLegislationSwitzerland",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("hints at %q, want %q", paths, want)
	}
	nl := feed.Datasheet[0].RegulatoryInformation.RegionEU.NationalLegislation
	if got := *nl.NationalLegislationGermany.StorageClassDE; got != StorageClassEnumDE_10 {
		t.Fatalf("StorageClassDE = %q", got)
	}
}

func TestExtraKeepsItsPosition(t *testing.T) {
	out := string(encodeFeed(t, decodeFile(t, "testdata/valid/national-extensions.xml")))
	a := strings.Index(out, "<NationalLegislationAustria>")
	s := strings.Index(out, "<NationalLegislationSwitzerland>")
	g := strings.Index(out, "<NationalLegislationGermany>")
	if a < 0 || s < 0 || g < 0 || a >= s || s >= g {
		t.Fatalf("order of national legislation changed:\n%s", out)
	}
	if !strings.Contains(out, `xsi:schemaLocation="http://www.esdscom.eu/eSDScom SDSComXML.xsd"`) {
		t.Fatalf("root attribute lost:\n%s", out)
	}
}

func TestDecodeStrict(t *testing.T) {
	decodeFile(t, "testdata/valid/minimal.xml", DecodeStrict())
	decodeFile(t, "testdata/valid/mixture-eu-de.xml", DecodeStrict())
	f, err := os.Open("testdata/valid/national-extensions.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = Decode(f, DecodeStrict())
	var unknown *UnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownError", err)
	}
	if unknown.Path != "Datasheet/RegulatoryInformation/RegionEU/NationalLegislation/NationalLegislationAustria" || unknown.What != "element" {
		t.Fatalf("unknown = %+v", unknown)
	}
}

func TestDecodeRejects(t *testing.T) {
	minimal := string(readFile(t, "testdata/valid/minimal.xml"))
	tests := []struct {
		name string
		doc  string
		opts []DecodeOption
		want error
	}{
		{"doctype", `<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "aaaa">]><eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"/>`, nil, ErrDTD},
		{"size", minimal, []DecodeOption{WithMaxSize(200)}, ErrTooLarge},
		{"root name", `<eSDScom:Feed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"/>`, nil, ErrRoot},
		{"root namespace", `<DatasheetFeed/>`, nil, ErrRoot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tt.doc), tt.opts...)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := Decode(strings.NewReader(minimal), WithMaxSize(int64(len(minimal)))); err != nil {
		t.Fatalf("input of exactly the limit: %v", err)
	}
}

func TestDecodeErrors(t *testing.T) {
	const head = `<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"><InformationFromExportingSystem>`
	for name, doc := range map[string]string{
		"empty":           ``,
		"malformed":       `<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"><a></b>`,
		"truncated":       head,
		"text before":     `x<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"/>`,
		"second root":     `<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"/><x/>`,
		"text after":      `<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"/>x`,
		"simple attr":     head + `<SdscomVersionNo a="1">5.5</SdscomVersionNo>`,
		"simple child":    head + `<SdscomVersionNo><b/></SdscomVersionNo>`,
		"truncated value": head + `<SdscomVersionNo>5.5`,
		"truncated raw":   head + `<Foo><Bar>`,
		"deep":            head + strings.Repeat("<a>", maxDepth+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(doc)); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestDecodeKeepsStrayText(t *testing.T) {
	doc := `<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom" foo="1">stray<Unknown x="y">a<b/>c</Unknown></eSDScom:DatasheetFeed>`
	feed, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]bool{}
	for _, e := range feed.Validate() {
		rules[e.Rule+"/"+e.Severity.String()] = true
	}
	for _, want := range []string{"text/error", "unknown/hint", "required/error"} {
		if !rules[want] {
			t.Errorf("missing finding %s in %v", want, feed.Validate())
		}
	}
	out := encodeFeed(t, feed)
	again, err := Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(feed, again) {
		t.Fatalf("round trip changed the tree:\n%s", out)
	}
	if _, err := Decode(strings.NewReader(doc), DecodeStrict()); err == nil {
		t.Fatal("strict decode accepted unknown content")
	}
}

func TestEncodeFillsSchemaVersion(t *testing.T) {
	feed := decodeFile(t, "testdata/valid/minimal.xml")
	feed.InformationFromExportingSystem.SdscomVersionNo = nil
	out := encodeFeed(t, feed)
	if !bytes.Contains(out, []byte("<SdscomVersionNo>5.5</SdscomVersionNo>")) {
		t.Fatalf("schema version missing:\n%s", out)
	}
	if feed.InformationFromExportingSystem.SdscomVersionNo != nil {
		t.Fatal("Encode changed its argument")
	}
	if err := Encode(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
}

func TestEncodeEscapes(t *testing.T) {
	feed := decodeFile(t, "testdata/valid/minimal.xml")
	feed.InformationFromExportingSystem.SystemUsedInPreparation = Ptr(String64("a<b>&\"c\"\r\x00"))
	feed.Extra.Attrs = append(feed.Extra.Attrs, xmlAttr("note", "x\ty\n\"z\""))
	out := encodeFeed(t, feed)
	if !bytes.Contains(out, []byte("a&lt;b&gt;&amp;\"c\"&#xD;�")) {
		t.Fatalf("text not escaped:\n%s", out)
	}
	if !bytes.Contains(out, []byte(`note="x&#x9;y&#xA;&quot;z&quot;"`)) {
		t.Fatalf("attribute not escaped:\n%s", out)
	}
	again, err := Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if got := *again.InformationFromExportingSystem.SystemUsedInPreparation; got != "a<b>&\"c\"\r�" {
		t.Fatalf("value after round trip = %q", got)
	}
}

func xmlAttr(name, value string) xml.Attr {
	return xml.Attr{Name: xml.Name{Local: name}, Value: value}
}

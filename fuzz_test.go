package esdscom

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzDecode checks that Decode never panics, honors the size limit, and
// that whatever it accepts survives an Encode and Decode round trip.
func FuzzDecode(f *testing.F) {
	files, _ := filepath.Glob("testdata/*/*.xml")
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte(`<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom"><x:y xmlns:x="urn:x" x:a="1">t</x:y></eSDScom:DatasheetFeed>`))
	const limit = 4096
	f.Fuzz(func(t *testing.T, data []byte) {
		feed, err := Decode(bytes.NewReader(data), WithMaxSize(limit))
		if len(data) > limit+1 && !errors.Is(err, ErrTooLarge) && err == nil {
			t.Fatalf("input of %d bytes accepted with a limit of %d", len(data), limit)
		}
		if err != nil {
			return
		}
		_ = feed.Validate()
		var out bytes.Buffer
		if err := Encode(&out, feed); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		again, err := Decode(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Fatalf("Decode of encoded output: %v\n%s", err, out.Bytes())
		}
		if info := feed.InformationFromExportingSystem; info != nil && info.SdscomVersionNo == nil {
			// Encode writes the schema version when it is missing.
			info.SdscomVersionNo = Ptr(SdscomVersionEnum(SchemaVersion))
		}
		if !reflect.DeepEqual(feed, again) {
			t.Fatalf("round trip changed the tree:\n%s", out.Bytes())
		}
	})
}

package esdscom_test

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/velcure/esdscom"
)

func Example() {
	f, err := os.Open("testdata/valid/mixture-eu-de.xml")
	if err != nil {
		log.Fatal(err)
	}
	feed, err := esdscom.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	if findings := feed.Validate(); !findings.Valid() {
		log.Fatal(findings.Errors())
	}

	ds := feed.Datasheet[0]
	for _, c := range ds.Composition.RegionEU.Choice.Component {
		fmt.Println(*c.Substance.IupacName, *c.Substance.CasNo)
	}
	de := ds.RegulatoryInformation.RegionEU.NationalLegislation.NationalLegislationGermany
	fmt.Println("WGK", *de.WaterHazardClass.Class)
	// Output:
	// propan-2-ol 67-63-0
	// water 7732-18-5
	// WGK 1
}

func ExampleEncode() {
	feed := &esdscom.DatasheetFeed{
		InformationFromExportingSystem: &esdscom.DatasheetFeedInformationFromExportingSystem{
			Language: &esdscom.DatasheetFeedInformationFromExportingSystemLanguage{
				FreetextLanguageCode: esdscom.Ptr(esdscom.LanguageCodeEnum_de),
			},
			RegulationsRelatedToCountryOrRegion: []esdscom.DatasheetFeedInformationFromExportingSystemRegulationsRelatedToCountryOrRegion{
				{RegulationsRelatedToCountryOrRegionCode: esdscom.Ptr(esdscom.CountryCodeEnum_DE)},
			},
			SystemUsedInPreparation: esdscom.Ptr(esdscom.String64("example")),
			DateGeneratedExport:     esdscom.Ptr(esdscom.Date("2026-10-14")),
		},
	}
	for _, e := range feed.Validate() {
		fmt.Println(e.Rule, e.Path)
	}
	var b strings.Builder
	if err := esdscom.Encode(&b, feed); err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.Contains(b.String(), "<SdscomVersionNo>5.5</SdscomVersionNo>"))
	// Output:
	// required InformationFromExportingSystem/SdscomVersionNo
	// required Datasheet
	// true
}

func ExampleDatasheetFeed_Validate() {
	doc := `<eSDScom:DatasheetFeed xmlns:eSDScom="http://www.esdscom.eu/eSDScom">
  <InformationFromExportingSystem>
    <SdscomVersionNo>5.5</SdscomVersionNo>
    <Language><FreetextLanguageCode>xx</FreetextLanguageCode></Language>
  </InformationFromExportingSystem>
</eSDScom:DatasheetFeed>`
	feed, err := esdscom.Decode(strings.NewReader(doc))
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range feed.Validate() {
		fmt.Printf("%s %s\n", e.Rule, e.Path)
	}
	// Output:
	// enum InformationFromExportingSystem/Language/FreetextLanguageCode
	// required InformationFromExportingSystem/RegulationsRelatedToCountryOrRegion
	// required InformationFromExportingSystem/SystemUsedInPreparation
	// required InformationFromExportingSystem/DateGeneratedExport
	// required Datasheet
}

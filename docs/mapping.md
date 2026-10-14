# Mapping notes

## Names

Go names are the XSD names with the first letter in upper case; `-` and `.`
become `_`. Anonymous types are named after their context, for example the
type of element `Language` inside `InformationFromExportingSystem` inside
`DatasheetFeed` is `DatasheetFeedInformationFromExportingSystemLanguage`.

| Construct | Go name |
|---|---|
| choice | field `Choice` (`Choice2`, … for further choices of the same type), type `<Parent>Choice` |
| optional or repeated sequence | field `<FirstElement>Group`, type `<Parent><Field>` |
| sequence starting with a group | field and type named after the group |
| enumeration value | constant `<Type>_<value>`, with characters outside letters and digits turned into `_` (`%` becomes `pct`) |
| attribute named like an element | field `<Name>Attr` |

A type derived by extension repeats the fields of its base type and shares
the structs of the inherited choices and sequences, so
`NumericRangeWithUnitAndQualifier.Choice` has the same type as
`NumericRangeWithQualifier.Choice`.

## Namespaces

Only the root element `DatasheetFeed` is in the namespace
`http://www.esdscom.eu/eSDScom`; all other elements are unqualified, as the
schema declares no `elementFormDefault`. Encode writes the root with the
prefix `eSDScom`. Elements in the eSDScom namespace below the root are not
schema-valid and are kept in `Extra`.

## National extensions

Elements typed by a national extension other than EU and DE, such as
`RegionUS` or `NationalLegislationAustria`, are not part of the Go types.
Decode keeps them in `Extra` at their position, Encode writes them back, and
Validate reports them as hints. A choice that has such an element as an
alternative is not required in the Go types, because the alternative present
in a document may be the one kept in `Extra`.

## Validation scope

Validate checks the Go tree. Element order is not checked, because the
types hold elements in schema order and Encode writes them that way. Surplus
or misplaced known elements kept in `Extra` are errors (`unexpected`);
unknown elements are hints (`unknown`).

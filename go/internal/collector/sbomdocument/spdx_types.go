package sbomdocument

// spdxDocument is the slice of the SPDX 2.x JSON shape Eshu projects into
// stable facts. Other fields are surfaced as warnings instead of silently
// consumed.
type spdxDocument struct {
	SPDXVersion                string             `json:"spdxVersion"`
	DataLicense                string             `json:"dataLicense"`
	SPDXID                     string             `json:"SPDXID"`
	Name                       string             `json:"name"`
	DocumentNamespace          string             `json:"documentNamespace"`
	CreationInfo               spdxCreationInfo   `json:"creationInfo"`
	Packages                   []spdxPackage      `json:"packages"`
	Files                      []map[string]any   `json:"files"`
	Snippets                   []map[string]any   `json:"snippets"`
	Relationships              []spdxRelationship `json:"relationships"`
	HasExtractedLicensingInfos []map[string]any   `json:"hasExtractedLicensingInfos"`
	Annotations                []map[string]any   `json:"annotations"`
}

type spdxCreationInfo struct {
	Created            string   `json:"created"`
	Creators           []string `json:"creators"`
	LicenseListVersion string   `json:"licenseListVersion"`
	Comment            string   `json:"comment"`
}

type spdxPackage struct {
	SPDXID                string            `json:"SPDXID"`
	Name                  string            `json:"name"`
	VersionInfo           string            `json:"versionInfo"`
	Supplier              string            `json:"supplier"`
	Originator            string            `json:"originator"`
	DownloadLocation      string            `json:"downloadLocation"`
	PackageFileName       string            `json:"packageFileName"`
	PrimaryPackagePurpose string            `json:"primaryPackagePurpose"`
	HomePage              string            `json:"homepage"`
	FilesAnalyzed         any               `json:"filesAnalyzed"`
	LicenseConcluded      string            `json:"licenseConcluded"`
	LicenseDeclared       string            `json:"licenseDeclared"`
	LicenseInfoFromFiles  []string          `json:"licenseInfoFromFiles"`
	Copyright             string            `json:"copyrightText"`
	Checksums             []spdxChecksum    `json:"checksums"`
	ExternalRefs          []spdxExternalRef `json:"externalRefs"`
	Comment               string            `json:"comment"`
	Description           string            `json:"description"`
	Summary               string            `json:"summary"`
	Annotations           []map[string]any  `json:"annotations"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
	Comment           string `json:"comment"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
	Comment            string `json:"comment"`
}

package googleworkspace

const (
	ExportMIMEDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	ExportMIMEXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	ExportMIMEPPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

func exportMIME(kind FileKind) (string, bool) {
	switch kind {
	case FileKindDocument:
		return ExportMIMEDOCX, true
	case FileKindSpreadsheet:
		return ExportMIMEXLSX, true
	case FileKindPresentation:
		return ExportMIMEPPTX, true
	default:
		return "", false
	}
}

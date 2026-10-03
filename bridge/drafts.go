package bridge

import (
	"errors"
	"unicode/utf8"
)

type draftRegistry struct {
	documents   map[string]string
	registered  permissions
	exportCheck func(string) (bool, error)
}

func (d *draftRegistry) configure(c Config, registered permissions) error {
	d.documents = map[string]string{}
	d.registered, d.exportCheck = registered, c.ExportCheck
	for id, text := range c.Documents {
		if !registered[id] || !utf8.ValidString(text) {
			return errors.New("invalid synthetic document")
		}
		d.documents[id] = text
	}
	return nil
}

func (d draftRegistry) resolve(id string) (string, string) {
	if !d.registered[id] {
		return "", "DENIED"
	}
	text, exists := d.documents[id]
	if !exists {
		return "", "NOT_FOUND"
	}
	return text, ""
}

func (d draftRegistry) read(args readDraftArgs) (*DraftData, string) {
	text, code := d.resolve(args.DocumentID)
	if code != "" {
		return nil, code
	}
	data := draftData(args.DocumentID, text)
	if code := data.validate(args.ExpectedRevision); code != "" {
		return nil, code
	}
	return d.approved(data)
}

func (d draftRegistry) approved(data *DraftData) (*DraftData, string) {
	if code := d.export(data.Text); code != "" {
		return nil, code
	}
	return data, ""
}

func draftData(id, text string) *DraftData {
	return &DraftData{Summary: Label, Text: text, Revision: byteDigest([]byte(text)),
		ByteCount: len(text), DocumentID: id, Provenance: "host synthetic fixture; untrusted data", Simulated: true}
}

func (d DraftData) validate(expected *string) string {
	if d.ByteCount > MaxDraftBytes {
		return "LIMIT_EXCEEDED"
	}
	if expected != nil && *expected != d.Revision {
		return "REVISION_MISMATCH"
	}
	return ""
}

func (d draftRegistry) export(text string) (code string) {
	defer func() {
		if recover() != nil {
			code = "RESOURCE_UNAVAILABLE"
		}
	}()
	if d.exportCheck == nil {
		return "EXPORT_BLOCKED"
	}
	allowed, err := d.exportCheck(text)
	return exportDecision(allowed, err)
}

func exportDecision(allowed bool, err error) string {
	if err != nil {
		return "RESOURCE_UNAVAILABLE"
	}
	if !allowed {
		return "EXPORT_BLOCKED"
	}
	return ""
}

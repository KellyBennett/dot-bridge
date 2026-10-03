package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"
)

var (
	idPattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	revisionPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	operations      = map[string]bool{"read_draft": true, "write_draft": true, "submit_task": true, "read_run": true, "cancel_run": true, "read_pr": true}
)

type request struct {
	ContractVersion string          `json:"contract_version"`
	RequestID       string          `json:"request_id"`
	Operation       string          `json:"operation"`
	ProjectID       string          `json:"project_id"`
	Arguments       json.RawMessage `json:"arguments"`
}

type readDraftArgs struct {
	DocumentID       string  `json:"document_id"`
	ExpectedRevision *string `json:"expected_revision,omitempty"`
}

func validatedObject(raw []byte) (jsonObject, error) {
	return boundedObject(raw, MaxRequestBytes)
}

func boundedObject(raw []byte, limit int) (jsonObject, error) {
	if len(raw) > limit || !utf8.Valid(raw) {
		return nil, errors.New("invalid input size or encoding")
	}
	if err := uniqueJSON(raw); err != nil {
		return nil, err
	}
	var object jsonObject
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("object required")
	}
	return object, nil
}

func (o jsonObject) fields(required, optional []string) error {
	allowed := permissions{}
	for _, field := range required {
		if _, exists := o[field]; !exists {
			return errors.New("required field absent")
		}
		allowed[field] = true
	}
	for _, field := range optional {
		allowed[field] = true
	}
	return o.allowed(allowed)
}

func (o jsonObject) allowed(allowed permissions) error {
	for field, value := range o {
		if !allowed[field] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("unknown or null field")
		}
	}
	return nil
}

func (o jsonObject) decode(raw []byte, target any, required, optional []string) error {
	if err := o.fields(required, optional); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func validateRequest(raw []byte) (request, readDraftArgs, string) {
	if len(raw) > MaxRequestBytes {
		return request{}, readDraftArgs{}, "LIMIT_EXCEEDED"
	}
	r, err := parseRequest(raw)
	if err != nil {
		return r, readDraftArgs{}, "INVALID_ARGUMENT"
	}
	return r.arguments()
}

func parseRequest(raw []byte) (request, error) {
	var r request
	object, err := validatedObject(raw)
	if err != nil {
		return r, err
	}
	err = object.decode(raw, &r, []string{"contract_version", "request_id", "operation", "project_id", "arguments"}, nil)
	if err != nil || !r.valid() {
		return r, errors.New("invalid request")
	}
	return r, nil
}

func (r request) valid() bool {
	return r.ContractVersion == "1" && uuidPattern.MatchString(r.RequestID) &&
		idPattern.MatchString(r.ProjectID) && operations[r.Operation]
}

func (r request) arguments() (request, readDraftArgs, string) {
	object, err := validatedObject(r.Arguments)
	if err != nil {
		return r, readDraftArgs{}, "INVALID_ARGUMENT"
	}
	if r.Operation != "read_draft" {
		return r, readDraftArgs{}, "OPERATION_NOT_IMPLEMENTED"
	}
	args, err := parseReadDraft(r.Arguments, object)
	if err != nil {
		return r, args, "INVALID_ARGUMENT"
	}
	return r, args, ""
}

func parseReadDraft(raw []byte, object jsonObject) (readDraftArgs, error) {
	var args readDraftArgs
	err := object.decode(raw, &args, []string{"document_id"}, []string{"expected_revision"})
	if err != nil || !args.valid() {
		return args, errors.New("invalid draft selector")
	}
	return args, nil
}

func (a readDraftArgs) valid() bool {
	return idPattern.MatchString(a.DocumentID) &&
		(a.ExpectedRevision == nil || revisionPattern.MatchString(*a.ExpectedRevision))
}

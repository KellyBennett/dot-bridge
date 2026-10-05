package bridge

import (
	"encoding/json"
	"errors"
	"time"
)

const MaxRunResponseBytes = 256 * 1024

func (t *taskTransaction) readDetails(args readRunArgs, response Response, now time.Time) (Response, error) {
	if code, err := t.poll(now); code != "" || err != nil {
		return taskResponse(t.receipt, code), err
	}
	details, code, err := t.details(args, response)
	if code != "" || err != nil {
		return taskResponse(t.receipt, code), err
	}
	if err = details.describeRunExport(); err != nil {
		return Response{}, err
	}
	if details.Receipt.ExportedBytes > MaxRunResponseBytes-8192 {
		return taskResponse(t.receipt, "LIMIT_EXCEEDED"), nil
	}
	return details, nil
}
func (t *taskTransaction) details(args readRunArgs, response Response) (Response, string, error) {
	life, err := t.lifecycle(response.Run.RunID)
	if err != nil {
		return Response{}, "", err
	}
	response = t.receipt.runResponse(life.run, response.Receipt.ApprovalID, "run_read")
	if code, err := t.eventPage(args, &response); code != "" || err != nil {
		return Response{}, code, err
	}
	code, err := t.resultPage(args, &response)
	return response, code, err
}
func (t *taskTransaction) resultPage(args readRunArgs, response *Response) (string, error) {
	if !response.resultReady() {
		return args.resultUnavailable(), nil
	}
	result, err := t.storedResult(response.runID(), response.runDigest())
	if err != nil {
		return "", err
	}
	return response.applyResult(result, args.ArtifactIDs), nil
}
func (t *receiptTransaction) storedResult(id, digest string) (AdapterResult, error) {
	raw, err := t.queries.GetRunResult(t.ctx, id)
	if err != nil {
		return AdapterResult{}, err
	}
	return decodeResult(raw, digest)
}
func (response Response) resultReady() bool { return response.Run.ResultAvailable }
func (response Response) runID() string     { return response.Run.RunID }
func (response Response) runDigest() string { return response.Run.EnvelopeDigest }
func (response *Response) applyResult(result AdapterResult, ids []string) string {
	response.Result = &result.Manifest
	artifacts, code := result.selectArtifacts(ids)
	response.Artifacts = artifacts
	return code
}
func (args readRunArgs) resultUnavailable() string {
	if len(args.ArtifactIDs) > 0 {
		return "RESULT_NOT_READY"
	}
	return ""
}
func decodeResult(raw, digest string) (AdapterResult, error) {
	var result AdapterResult
	if len(raw) > 192*1024 {
		return result, errors.New("oversized result")
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, err
	}
	if !result.Manifest.validText() || result.Manifest.EnvelopeDigest != digest || !result.validArtifacts() {
		return result, errors.New("invalid result")
	}
	return result, nil
}
func (result AdapterResult) selectArtifacts(ids []string) ([]ResultArtifact, string) {
	selected := []ResultArtifact{}
	for _, id := range ids {
		artifact, found := result.artifact(id)
		if !found {
			return nil, "NOT_FOUND"
		}
		selected = append(selected, artifact)
	}
	return selected, ""
}
func (result AdapterResult) artifact(id string) (ResultArtifact, bool) {
	for _, artifact := range result.Artifacts {
		if artifact.ID == id {
			return artifact, true
		}
	}
	return ResultArtifact{}, false
}

package bridge

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

func (life lifecycle) validateObservation(observation AdapterObservation, now time.Time) error {
	if !observation.validSize() || !observation.validState() {
		return errors.New("invalid observation")
	}
	verified, valid := observation.verifiedTime(life.run.LastVerifiedAt, now)
	if !valid {
		return errors.New("invalid observation time")
	}
	if !life.orderedEvents(observation.Events, verified) {
		return errors.New("invalid event stream")
	}
	if !observation.stateEvidence(life.run.LastConfirmedState) {
		return errors.New("missing terminal evidence")
	}
	return nil
}
func (observation AdapterObservation) validSize() bool {
	raw, err := json.Marshal(observation)
	return err == nil && len(observation.Events) <= 128 && len(raw) <= 192*1024
}
func (observation AdapterObservation) validState() bool {
	return observation.State == "running" || observation.State == "completed" || observation.State == "failed"
}
func (life lifecycle) orderedEvents(events []AdapterEvent, verified time.Time) bool {
	sequence := life.adapterSequence
	previous, _ := time.Parse(time.RFC3339Nano, life.run.LastVerifiedAt)
	for i, event := range events {
		if !event.valid(sequence+1, previous, verified) || (event.Kind == "terminated" && i != len(events)-1) {
			return false
		}
		sequence, previous = event.Sequence, eventTime(event)
	}
	return true
}
func (event AdapterEvent) valid(sequence int64, previous, verified time.Time) bool {
	at, err := time.Parse(time.RFC3339Nano, event.At)
	return err == nil && event.Sequence == sequence && event.Simulated && labelled(event.Summary) &&
		len(event.Summary) <= 1024 && safeText(event.Summary) && !at.Before(previous) && !at.After(verified) && event.validKind()
}
func (event AdapterEvent) validKind() bool {
	if event.Kind == "started" || event.Kind == "progress" {
		return event.State == "running"
	}
	return event.Kind == "terminated" && (event.State == "completed" || event.State == "failed")
}
func (observation AdapterObservation) stateEvidence(previous string) bool {
	if len(observation.Events) == 0 {
		return previous == observation.State
	}
	last := observation.Events[len(observation.Events)-1]
	return last.State == observation.State && (observation.State == "running" || last.Kind == "terminated")
}
func (observation AdapterObservation) verifiedTime(previous string, now time.Time) (time.Time, bool) {
	verified, err := time.Parse(time.RFC3339Nano, observation.LastVerifiedAt)
	old, oldErr := time.Parse(time.RFC3339Nano, previous)
	return verified, err == nil && oldErr == nil && !verified.After(now) && !verified.Before(old)
}
func safeText(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}

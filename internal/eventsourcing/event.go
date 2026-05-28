package eventsourcing

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

const CurrentSchemaVersion uint32 = 1

type Event struct {
	ID            ulid.ULID       `json:"id"`
	Type          string          `json:"type"`
	TenantID      string          `json:"tenant_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
	Version       uint32          `json:"version"`
	CorrelationID *ulid.ULID      `json:"correlation_id,omitempty"`
	CausationID   *ulid.ULID      `json:"causation_id,omitempty"`
}

func NewEvent(tenantID, eventType string, payload json.RawMessage) (Event, error) {
	if tenantID == "" {
		return Event{}, fmt.Errorf("eventsourcing: tenant id is required")
	}
	if eventType == "" {
		return Event{}, fmt.Errorf("eventsourcing: event type is required")
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return Event{}, fmt.Errorf("eventsourcing: payload is not valid JSON")
	}
	now := time.Now().UTC()
	id, err := ulid.New(ulid.Timestamp(now), rand.Reader)
	if err != nil {
		return Event{}, fmt.Errorf("eventsourcing: generate ulid: %w", err)
	}
	return Event{
		ID:         id,
		Type:       eventType,
		TenantID:   tenantID,
		OccurredAt: now,
		Payload:    payload,
		Version:    CurrentSchemaVersion,
	}, nil
}

func (e Event) Validate() error {
	if e.ID == (ulid.ULID{}) {
		return fmt.Errorf("eventsourcing: event id is zero")
	}
	if e.TenantID == "" {
		return fmt.Errorf("eventsourcing: tenant id is required")
	}
	if e.Type == "" {
		return fmt.Errorf("eventsourcing: event type is required")
	}
	if e.OccurredAt.IsZero() {
		return fmt.Errorf("eventsourcing: occurred_at is zero")
	}
	if e.Version == 0 {
		return fmt.Errorf("eventsourcing: version must be >= 1")
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("eventsourcing: payload is empty")
	}
	if !json.Valid(e.Payload) {
		return fmt.Errorf("eventsourcing: payload is not valid JSON")
	}
	return nil
}

func (e Event) Chain(eventType string, payload json.RawMessage) (Event, error) {
	child, err := NewEvent(e.TenantID, eventType, payload)
	if err != nil {
		return Event{}, err
	}
	if e.CorrelationID != nil {
		child.CorrelationID = e.CorrelationID
	} else {
		root := e.ID
		child.CorrelationID = &root
	}
	parent := e.ID
	child.CausationID = &parent
	return child, nil
}

package consolequery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

type AuditEvent struct {
	ID           string         `json:"id"`
	ActorType    string         `json:"actor_type"`
	ActorID      string         `json:"actor_id"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	RequestID    string         `json:"request_id"`
	Reason       *string        `json:"reason,omitempty"`
	Summary      map[string]any `json:"summary"`
	CreatedAt    time.Time      `json:"created_at"`
}

type HealthEvent struct {
	ID         string         `json:"id"`
	DeviceID   string         `json:"device_id"`
	Source     string         `json:"source"`
	EventType  string         `json:"event_type"`
	Severity   string         `json:"severity"`
	Reason     string         `json:"reason"`
	Payload    map[string]any `json:"payload"`
	ObservedAt time.Time      `json:"observed_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Service struct{ db *database.DB }

func New(db *database.DB) *Service { return &Service{db: db} }

func (service *Service) ListAuditEvents(ctx context.Context, page paging.Page) ([]AuditEvent, int, error) {
	if service == nil || service.db == nil {
		return nil, 0, fmt.Errorf("console query database is not configured")
	}
	var total int
	if err := service.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_audit_events`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := service.db.Pool().Query(ctx, `SELECT id,actor_type,actor_id,action,resource_type,resource_id,
		request_id,reason,summary,created_at FROM device_audit_events
		ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]AuditEvent, 0)
	for rows.Next() {
		var item AuditEvent
		var summary []byte
		if err := rows.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.Action, &item.ResourceType,
			&item.ResourceID, &item.RequestID, &item.Reason, &summary, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(summary, &item.Summary); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (service *Service) ListHealthEvents(ctx context.Context, deviceID string, page paging.Page) ([]HealthEvent, int, error) {
	if service == nil || service.db == nil {
		return nil, 0, fmt.Errorf("console query database is not configured")
	}
	var total int
	if err := service.db.Pool().QueryRow(ctx, `SELECT count(*) FROM device_health_events WHERE device_id=$1`, deviceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := service.db.Pool().Query(ctx, `SELECT id,device_id,source,event_type,severity,reason,payload,observed_at,created_at
		FROM device_health_events WHERE device_id=$1 ORDER BY observed_at DESC,id DESC LIMIT $2 OFFSET $3`,
		deviceID, page.Limit(), page.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]HealthEvent, 0)
	for rows.Next() {
		var item HealthEvent
		var payload []byte
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.Source, &item.EventType, &item.Severity,
			&item.Reason, &payload, &item.ObservedAt, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

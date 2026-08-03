package repository

import (
	"context"
	"fmt"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
)

type DeviceRepository struct{}

func (DeviceRepository) UpdateLifecycle(
	ctx context.Context,
	querier database.Querier,
	id string,
	from domain.DeviceLifecycleStatus,
	to domain.DeviceLifecycleStatus,
) error {
	result, err := querier.Exec(ctx, `
        UPDATE devices
        SET lifecycle_status = $3, updated_at = clock_timestamp()
        WHERE id = $1 AND lifecycle_status = $2`, id, from, to)
	if err != nil {
		return fmt.Errorf("update device lifecycle: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

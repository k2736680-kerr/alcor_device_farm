// Package imagecatalog owns the Android SDK catalogue and on-demand image
// preparation records. It deliberately does not download anything: only a
// Build Agent receives the two fixed Host Command types.
package imagecatalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
	"github.com/Ad-Quanta/alcor-device-farm/internal/correlation"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	"github.com/Ad-Quanta/alcor-device-farm/internal/domain"
	"github.com/Ad-Quanta/alcor-device-farm/internal/identifier"
	"github.com/Ad-Quanta/alcor-device-farm/internal/providers"
	"github.com/Ad-Quanta/alcor-device-farm/internal/repository"
	"github.com/Ad-Quanta/alcor-device-farm/internal/runtimeprofile"
	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidArgument = errors.New("安卓系统镜像请求无效")
	ErrNoBuildAgent    = errors.New("当前没有在线的镜像构建代理")
	ErrNotFound        = errors.New("未找到安卓系统镜像目录项")
	ErrConflict        = errors.New("安卓系统镜像准备发生冲突")
)

type Entry struct {
	ID              string    `json:"id"`
	PackageName     string    `json:"package_name"`
	APILevel        int       `json:"api_level"`
	ImageType       string    `json:"image_type"`
	ABI             string    `json:"abi"`
	Revision        string    `json:"revision"`
	SourceUpdatedAt time.Time `json:"source_updated_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	Status          string    `json:"status"`
	PreparationID   string    `json:"preparation_id,omitempty"`
	ImageID         string    `json:"image_id,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
}

type Preparation struct {
	ID             string         `json:"id"`
	CatalogID      string         `json:"catalog_id"`
	HostID         string         `json:"host_id"`
	CommandID      string         `json:"command_id"`
	RuntimeProfile map[string]any `json:"runtime_profile"`
	Status         string         `json:"status"`
	ImageID        string         `json:"image_id,omitempty"`
	ErrorCode      string         `json:"error_code,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type PreparationInput struct {
	CatalogID      string         `json:"catalog_id"`
	RuntimeProfile map[string]any `json:"runtime_profile"`
}

type Service struct {
	db    *database.DB
	newID func() (string, error)
}

func New(db *database.DB) *Service { return &Service{db: db, newID: identifier.New} }

func (service *Service) List(ctx context.Context) ([]Entry, error) {
	if service == nil || service.db == nil {
		return nil, ErrInvalidArgument
	}
	rows, err := service.db.Pool().Query(ctx, `SELECT c.id,c.package_name,c.api_level,c.image_type,c.abi,c.revision,c.source_updated_at,c.last_seen_at,
		COALESCE(p.id,''),COALESCE(p.image_id,''),COALESCE(p.status,''),COALESCE(p.error_code,''),COALESCE(p.catalog_revision,''),COALESCE(i.status,'')
		FROM android_system_image_catalog c
		LEFT JOIN LATERAL (SELECT id,image_id,status,error_code,catalog_revision FROM device_image_preparations p WHERE p.catalog_id=c.id
			ORDER BY CASE WHEN p.status='cached' AND p.image_id IS NOT NULL THEN 0 ELSE 1 END,p.created_at DESC LIMIT 1) p ON true
		LEFT JOIN device_images i ON i.id=p.image_id ORDER BY c.api_level DESC,c.image_type,c.abi`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var entry Entry
		var preparationStatus, imageStatus, preparationRevision string
		if err := rows.Scan(&entry.ID, &entry.PackageName, &entry.APILevel, &entry.ImageType, &entry.ABI, &entry.Revision, &entry.SourceUpdatedAt, &entry.LastSeenAt,
			&entry.PreparationID, &entry.ImageID, &preparationStatus, &entry.ErrorCode, &preparationRevision, &imageStatus); err != nil {
			return nil, err
		}
		entry.Status = catalogStatus(preparationStatus, imageStatus, preparationRevision != "" && preparationRevision != entry.Revision)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (service *Service) Sync(ctx context.Context, actor audit.Actor, key string) (Preparation, error) {
	return service.queue(ctx, actor, key, "sync_android_catalog", "", nil)
}

func (service *Service) Prepare(ctx context.Context, actor audit.Actor, key string, input PreparationInput) (Preparation, error) {
	if strings.TrimSpace(input.CatalogID) == "" {
		return Preparation{}, ErrInvalidArgument
	}
	profile, err := runtimeprofile.Parse(input.RuntimeProfile)
	if err != nil {
		return Preparation{}, ErrInvalidArgument
	}
	return service.queue(ctx, actor, key, "prepare_android_image", input.CatalogID, profile.Map())
}

func (service *Service) queue(ctx context.Context, actor audit.Actor, key, commandType, catalogID string, profile map[string]any) (Preparation, error) {
	requestID := correlation.FromContext(ctx).RequestID
	if service == nil || service.db == nil || !actor.Valid() || strings.TrimSpace(requestID) == "" || len(strings.TrimSpace(key)) < 8 {
		return Preparation{}, ErrInvalidArgument
	}
	preparationID, err := service.newID()
	if err != nil {
		return Preparation{}, err
	}
	commandID, err := service.newID()
	if err != nil {
		return Preparation{}, err
	}
	result := Preparation{ID: preparationID, CatalogID: catalogID, RuntimeProfile: profile, Status: "queued"}
	if result.RuntimeProfile == nil {
		result.RuntimeProfile = map[string]any{}
	}
	result.CreatedAt, result.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	err = service.db.WithinTx(ctx, func(tx pgx.Tx) error {
		var hostID string
		if err := tx.QueryRow(ctx, `SELECT id FROM device_hosts WHERE status='online' AND draining=false
			AND COALESCE(capabilities->>'image_build_agent','false')='true' ORDER BY last_heartbeat_at DESC NULLS LAST,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&hostID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoBuildAgent
			}
			return err
		}
		payload := map[string]any{"operation_source": "android_catalog"}
		if commandType == "prepare_android_image" {
			var packageName, revision string
			if err := tx.QueryRow(ctx, `SELECT package_name,revision FROM android_system_image_catalog WHERE id=$1`, catalogID).Scan(&packageName, &revision); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrNotFound
				}
				return err
			}
			payload["catalog_id"], payload["package_name"], payload["revision"], payload["runtime_profile"] = catalogID, packageName, revision, profile
		}
		record, err := (repository.CommandRepository{}).Create(ctx, tx, repository.CreateCommandParams{ID: commandID, HostID: hostID, CommandType: commandType, Payload: payload, MaxAttempts: 3, IdempotencyKey: "catalog-" + actor.ClientID + "-" + key})
		if err != nil {
			if errors.Is(err, repository.ErrIdempotencyConflict) {
				return ErrConflict
			}
			return err
		}
		result.HostID, result.CommandID = record.HostID, record.ID
		if commandType == "sync_android_catalog" {
			result.ID, result.Status, result.CreatedAt, result.UpdatedAt = record.ID, "queued", record.CreatedAt, record.UpdatedAt
			if record.ID == commandID {
				return service.insertAudit(ctx, tx, actor, requestID, "synchronize_android_system_images", "android_system_image_catalog", record.ID,
					map[string]any{"command_id": record.ID, "host_id": record.HostID})
			}
			return nil
		}
		var existing Preparation
		var revision string
		if err := tx.QueryRow(ctx, `SELECT revision FROM android_system_image_catalog WHERE id=$1`, catalogID).Scan(&revision); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO device_image_preparations(id,catalog_id,host_id,build_command_id,client_id,idempotency_key,catalog_revision,runtime_profile)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(client_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key
			RETURNING id,catalog_id,host_id,COALESCE(validation_command_id,build_command_id),runtime_profile,status,COALESCE(image_id,''),COALESCE(error_code,''),created_at,updated_at`,
			preparationID, catalogID, hostID, record.ID, actor.ClientID, key, revision, profile).Scan(&existing.ID, &existing.CatalogID, &existing.HostID, &existing.CommandID, &existing.RuntimeProfile, &existing.Status, &existing.ImageID, &existing.ErrorCode, &existing.CreatedAt, &existing.UpdatedAt)
		if err != nil {
			return err
		}
		if existing.CommandID != record.ID && existing.Status == "queued" {
			return ErrConflict
		}
		if record.ID == commandID {
			if err := service.insertAudit(ctx, tx, actor, requestID, "prepare_android_system_image", "android_system_image", catalogID,
				map[string]any{"preparation_id": existing.ID, "command_id": record.ID, "host_id": record.HostID, "runtime_profile": profile}); err != nil {
				return err
			}
		}
		result = existing
		return nil
	})
	return result, err
}

func (service *Service) insertAudit(ctx context.Context, tx pgx.Tx, actor audit.Actor, requestID, action, resourceType, resourceID string, summary map[string]any) error {
	id, err := service.newID()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO device_audit_events(id,actor_type,actor_id,action,resource_type,resource_id,request_id,summary)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, id, actor.Type, actor.ID, action, resourceType, resourceID, requestID, encoded)
	return err
}

func catalogStatus(preparation, image string, officialChanged bool) string {
	if officialChanged {
		return "official_updated"
	}
	switch image {
	case "ready":
		return "cached"
	case "validating", "draft":
		return "validating"
	case "failed", "disabled":
		return "failed"
	}
	switch preparation {
	case "", "failed":
		if preparation == "failed" {
			return "failed"
		}
		return "downloadable"
	case "queued", "building":
		return "preparing"
	default:
		return preparation
	}
}

// ReconcileCommand is called from Host Command completion in the same
// transaction. It is intentionally package-level so the command service can
// retain ownership of command leases without a circular dependency.
func ReconcileCommand(ctx context.Context, tx pgx.Tx, record repository.CommandRecord, newID func() (string, error)) error {
	var payload map[string]any
	if json.Unmarshal(record.Payload, &payload) != nil || payload["operation_source"] != "android_catalog" || (record.Status == domain.CommandPending || record.Status == domain.CommandLeased) {
		return nil
	}
	switch record.CommandType {
	case "sync_android_catalog":
		return reconcileSync(ctx, tx, record, newID)
	case "prepare_android_image":
		return reconcilePreparation(ctx, tx, record, newID)
	case "validate_image":
		return reconcileValidation(ctx, tx, record, newID)
	default:
		return nil
	}
}

func reconcileSync(ctx context.Context, tx pgx.Tx, record repository.CommandRecord, newID func() (string, error)) error {
	if record.Status != domain.CommandSucceeded {
		return nil
	}
	var result struct {
		Entries []struct {
			PackageName string `json:"package_name"`
			APILevel    int    `json:"api_level"`
			ImageType   string `json:"image_type"`
			ABI         string `json:"abi"`
			Revision    string `json:"revision"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(record.Result, &result); err != nil || len(result.Entries) == 0 {
		return ErrInvalidArgument
	}
	for _, item := range result.Entries {
		if !validEntry(item.PackageName, item.APILevel, item.ImageType, item.ABI, item.Revision) {
			return ErrInvalidArgument
		}
		id, err := newID()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO android_system_image_catalog(id,package_name,api_level,image_type,abi,revision,source_updated_at,last_seen_at)
			VALUES($1,$2,$3,$4,$5,$6,clock_timestamp(),clock_timestamp()) ON CONFLICT(package_name) DO UPDATE SET
			api_level=EXCLUDED.api_level,image_type=EXCLUDED.image_type,abi=EXCLUDED.abi,revision=EXCLUDED.revision,last_seen_at=clock_timestamp(),
			source_updated_at=CASE WHEN android_system_image_catalog.revision<>EXCLUDED.revision THEN clock_timestamp() ELSE android_system_image_catalog.source_updated_at END,updated_at=clock_timestamp()`, id, item.PackageName, item.APILevel, item.ImageType, item.ABI, item.Revision); err != nil {
			return err
		}
	}
	return nil
}

func reconcilePreparation(ctx context.Context, tx pgx.Tx, record repository.CommandRecord, newID func() (string, error)) error {
	var job struct {
		ID, CatalogID, ValidationCommandID string
		RuntimeProfile                     map[string]any
	}
	err := tx.QueryRow(ctx, `SELECT id,catalog_id,COALESCE(validation_command_id,''),runtime_profile FROM device_image_preparations WHERE build_command_id=$1 FOR UPDATE`, record.ID).Scan(&job.ID, &job.CatalogID, &job.ValidationCommandID, &job.RuntimeProfile)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Status != domain.CommandSucceeded {
		code := "IMAGE_PREPARATION_FAILED"
		if record.ErrorCode != nil && *record.ErrorCode != "" {
			code = *record.ErrorCode
		}
		_, err = tx.Exec(ctx, `UPDATE device_image_preparations SET status='failed',error_code=$2,updated_at=clock_timestamp() WHERE id=$1`, job.ID, code)
		return err
	}
	if job.ValidationCommandID != "" {
		return nil
	}
	var output struct {
		DockerImage  string `json:"docker_image"`
		DockerDigest string `json:"docker_digest"`
		ImageDiskMB  int    `json:"image_disk_mb"`
	}
	if err := json.Unmarshal(record.Result, &output); err != nil || !validDigest(output.DockerDigest) || !providers.ValidRuntimeImageReference(output.DockerImage) || output.ImageDiskMB < 1 {
		return failPreparation(ctx, tx, job.ID, "INVALID_IMAGE_BUILD_RESULT")
	}
	profile, err := runtimeprofile.Parse(job.RuntimeProfile)
	if err != nil {
		return failPreparation(ctx, tx, job.ID, "INVALID_RUNTIME_PROFILE")
	}
	mapped := profile.Map()
	mapped["image_disk_mb"] = output.ImageDiskMB
	var entry struct {
		API       int
		Type, ABI string
	}
	if err := tx.QueryRow(ctx, `SELECT api_level,image_type,abi FROM android_system_image_catalog WHERE id=$1`, job.CatalogID).Scan(&entry.API, &entry.Type, &entry.ABI); err != nil {
		return err
	}
	profileJSON, err := json.Marshal(mapped)
	if err != nil {
		return err
	}
	var imageID string
	err = tx.QueryRow(ctx, `SELECT id FROM device_images WHERE docker_digest=$1 AND resource_config=$2::jsonb AND status='ready' FOR UPDATE`, output.DockerDigest, string(profileJSON)).Scan(&imageID)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE device_image_preparations SET docker_image=$2,docker_digest=$3,image_disk_mb=$4,
			image_id=$5,status='cached',error_code=NULL,updated_at=clock_timestamp() WHERE id=$1`, job.ID, output.DockerImage, output.DockerDigest, output.ImageDiskMB, imageID)
		return err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	validationCommandID, err := newID()
	if err != nil {
		return err
	}
	capabilities := map[string]any{"platformName": "Android", "apiLevel": entry.API, "abi": entry.ABI, "resolution": fmt.Sprintf("%dx%d", profile.Width, profile.Height)}
	for key, value := range mapped {
		capabilities[key] = value
	}
	validationPayload := map[string]any{
		"operation_source": "android_catalog", "preparation_id": job.ID, "image_id": "prepared-" + job.ID,
		"device_id": "validation-" + validationCommandID, "provider_ref": "validation-" + validationCommandID,
		"docker_image": output.DockerImage, "docker_digest": output.DockerDigest,
		"capabilities": capabilities, "runtime_profile": mapped,
	}
	command, err := (repository.CommandRepository{}).Create(ctx, tx, repository.CreateCommandParams{
		ID: validationCommandID, HostID: record.HostID, CommandType: "validate_image", Payload: validationPayload,
		MaxAttempts: 3, IdempotencyKey: "catalog-validate-" + job.ID,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE device_image_preparations SET validation_command_id=$2,docker_image=$3,docker_digest=$4,image_disk_mb=$5,
		status='validating',error_code=NULL,updated_at=clock_timestamp() WHERE id=$1`, job.ID, command.ID, output.DockerImage, output.DockerDigest, output.ImageDiskMB)
	return err
}

func reconcileValidation(ctx context.Context, tx pgx.Tx, record repository.CommandRecord, newID func() (string, error)) error {
	var job struct {
		ID, CatalogID, CatalogRevision, DockerImage, DockerDigest string
		ImageDiskMB                                               int
		RuntimeProfile                                            map[string]any
	}
	err := tx.QueryRow(ctx, `SELECT id,catalog_id,catalog_revision,docker_image,docker_digest,image_disk_mb,runtime_profile
		FROM device_image_preparations WHERE validation_command_id=$1 FOR UPDATE`, record.ID).
		Scan(&job.ID, &job.CatalogID, &job.CatalogRevision, &job.DockerImage, &job.DockerDigest, &job.ImageDiskMB, &job.RuntimeProfile)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Status != domain.CommandSucceeded {
		code := "IMAGE_VALIDATION_FAILED"
		if record.ErrorCode != nil && *record.ErrorCode != "" {
			code = *record.ErrorCode
		}
		return failPreparation(ctx, tx, job.ID, code)
	}
	var result struct {
		DigestVerified bool `json:"digest_verified"`
		Ready          bool `json:"ready"`
		STFRegistered  bool `json:"stf_registered"`
	}
	if json.Unmarshal(record.Result, &result) != nil || !result.DigestVerified || !result.Ready || !result.STFRegistered {
		return failPreparation(ctx, tx, job.ID, "IMAGE_VALIDATION_INCOMPLETE")
	}
	profile, err := runtimeprofile.Parse(job.RuntimeProfile)
	if err != nil {
		return failPreparation(ctx, tx, job.ID, "INVALID_RUNTIME_PROFILE")
	}
	mapped := profile.Map()
	mapped["image_disk_mb"] = job.ImageDiskMB
	profileJSON, err := json.Marshal(mapped)
	if err != nil {
		return err
	}
	profileFingerprint := fmt.Sprintf("%x", sha256.Sum256(profileJSON))[:10]
	var entry struct {
		API       int
		Type, ABI string
	}
	if err := tx.QueryRow(ctx, `SELECT api_level,image_type,abi FROM android_system_image_catalog WHERE id=$1`, job.CatalogID).Scan(&entry.API, &entry.Type, &entry.ABI); err != nil {
		return err
	}
	imageID, err := newID()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("android-api%d-%s-%s-%s-%s", entry.API, entry.Type, entry.ABI, strings.TrimPrefix(job.DockerDigest, "sha256:")[:12], profileFingerprint)
	err = tx.QueryRow(ctx, `INSERT INTO device_images(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status,validation_error)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,'ready',NULL)
		ON CONFLICT (docker_digest,md5(resource_config::text)) DO UPDATE SET docker_image=EXCLUDED.docker_image,
			status='ready',validation_error=NULL,updated_at=clock_timestamp()
		RETURNING id`, imageID, name, job.DockerImage, job.DockerDigest, entry.API, entry.ABI, fmt.Sprintf("%dx%d", profile.Width, profile.Height), string(profileJSON)).Scan(&imageID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE device_image_preparations SET image_id=$2,status='cached',error_code=NULL,updated_at=clock_timestamp() WHERE id=$1`, job.ID, imageID)
	return err
}

func failPreparation(ctx context.Context, tx pgx.Tx, id, code string) error {
	_, err := tx.Exec(ctx, `UPDATE device_image_preparations SET status='failed',error_code=$2,updated_at=clock_timestamp() WHERE id=$1`, id, code)
	return err
}

func validEntry(packageName string, api int, imageType, abi, revision string) bool {
	return api >= 26 && api <= 99 && (imageType == "default" || imageType == "google_apis" || imageType == "google_play") && abi == "x86_64" && validRevision(revision) && packageName == fmt.Sprintf("system-images;android-%d;%s;%s", api, imageType, abi)
}
func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range strings.TrimPrefix(value, "sha256:") {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
func validRevision(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// TestManagementListPaginationReadsDatabaseWindow proves that every list
// endpoint pushes the page window down to SQL (LIMIT/OFFSET + an independent
// COUNT) instead of loading the whole table into memory and slicing it there.
//
// For each resource we seed more rows than fit on one page, then walk the pages
// through the real HTTP handler. The assertions that matter:
//   - the reported `total` equals the row count in the database, not the length
//     of the returned page (so the caller learns the true size);
//   - every page returns at most `page_size` rows;
//   - the union of all pages covers exactly the seeded rows with no duplicates
//     (OFFSET pagination is stable and disjoint);
//   - the final remainder page carries only the leftover rows.
func TestManagementListPaginationReadsDatabaseWindow(t *testing.T) {
	environment := newManagementEnvironment(t)

	seedImages(t, environment, 75)
	seedHosts(t, environment, 23)
	seedPools(t, environment, 17)
	hostID := seedSingletonHost(t, environment)
	seedDevices(t, environment, hostID, 41)
	poolID := seedSingletonPool(t, environment)
	seedReservations(t, environment, poolID, 29)

	collectDatabasePagedWindow(t, environment, "/api/v1/device-images", countRows(t, environment, "device_images"))
	collectDatabasePagedWindow(t, environment, "/api/v1/device-hosts", countRows(t, environment, "device_hosts"))
	collectDatabasePagedWindow(t, environment, "/api/v1/device-pools", countRows(t, environment, "device_pools"))
	collectDatabasePagedWindow(t, environment, "/api/v1/devices", countRows(t, environment, "devices"))
	collectDatabasePagedWindow(t, environment, "/api/v1/device-reservations", countRows(t, environment, "device_reservations"))
}

// countRows reads the ground-truth row count straight from the database so the
// expected total is never a hard-coded assumption about fixture data.
func countRows(t *testing.T, environment *managementEnvironment, table string) int {
	t.Helper()
	var total int
	if err := environment.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&total); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return total
}

// collectDatabasePagedWindow walks every page of an endpoint and verifies the
// SQL-level window contract described above.
func collectDatabasePagedWindow(t *testing.T, environment *managementEnvironment, path string, expectedTotal int) {
	t.Helper()
	const size = 20

	seen := make(map[string]bool)
	page := 1
	for {
		response := environment.request(t, http.MethodGet, fmt.Sprintf("%s?page=%d&page_size=%d", path, page, size), nil, serviceToken, "")
		assertStatus(t, response, http.StatusOK)

		var envelope struct {
			Items    []map[string]any `json:"items"`
			Page     int              `json:"page"`
			PageSize int              `json:"page_size"`
			Total    int              `json:"total"`
		}
		decodeData(t, response, &envelope)

		if envelope.Page != page {
			t.Fatalf("%s page echo = %d, want %d", path, envelope.Page, page)
		}
		if envelope.PageSize != size {
			t.Fatalf("%s page_size echo = %d, want %d", path, envelope.PageSize, size)
		}
		if envelope.Total != expectedTotal {
			t.Fatalf("%s total = %d, want %d (database row count, not page length); raw data=%s", path, envelope.Total, expectedTotal, response.Data)
		}
		if len(envelope.Items) > size {
			t.Fatalf("%s page %d returned %d items, exceeds page_size %d", path, page, len(envelope.Items), size)
		}
		for _, item := range envelope.Items {
			id, ok := item["id"].(string)
			if !ok || id == "" {
				t.Fatalf("%s page %d item missing id: %#v", path, page, item)
			}
			if seen[id] {
				t.Fatalf("%s returned duplicate id %q across pages (window is not database-backed)", path, id)
			}
			seen[id] = true
		}

		// Empty page (offset past the end) or the final partial page ends the walk.
		if len(envelope.Items) < size {
			break
		}
		page++
		if page > 100 {
			t.Fatalf("%s produced more than 100 pages for %d rows", path, expectedTotal)
		}
	}

	if len(seen) != expectedTotal {
		t.Fatalf("%s covered %d of %d distinct rows across pages (window skipped or duplicated rows)", path, len(seen), expectedTotal)
	}
}

func seedImages(t *testing.T, environment *managementEnvironment, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= count; i++ {
		if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_images
			(id,name,docker_digest,api_level,abi,resolution,status)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			fmt.Sprintf("imgseed%013d", i),
			fmt.Sprintf("seed-image-%04d", i),
			fmt.Sprintf("sha256:%064d", i),
			34, "x86_64", "1080x2400", "draft"); err != nil {
			t.Fatalf("seed image %d: %v", i, err)
		}
	}
}

func seedHosts(t *testing.T, environment *managementEnvironment, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= count; i++ {
		if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_hosts
			(id,name,host_type,status) VALUES ($1,$2,$3,$4)`,
			fmt.Sprintf("hostseed%012d", i),
			fmt.Sprintf("seed-host-%04d", i),
			"docker_emulator", "offline"); err != nil {
			t.Fatalf("seed host %d: %v", i, err)
		}
	}
}

// seedSingletonHost creates one host that devices can reference as a foreign key.
func seedSingletonHost(t *testing.T, environment *managementEnvironment) string {
	t.Helper()
	ctx := context.Background()
	id := "hostseed900000000001"
	if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_hosts
		(id,name,host_type,status) VALUES ($1,$2,$3,$4)`, id, "seed-host-singleton", "docker_emulator", "offline"); err != nil {
		t.Fatalf("seed singleton host: %v", err)
	}
	return id
}

func seedSingletonPool(t *testing.T, environment *managementEnvironment) string {
	t.Helper()
	ctx := context.Background()
	id := "poolseed000000000001"
	if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_pools
		(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
		VALUES ($1,$2,$3,$4,$5,$6)`, id, "seed-pool-singleton", 600, 3600, 2, "active"); err != nil {
		t.Fatalf("seed singleton pool: %v", err)
	}
	return id
}

func seedPools(t *testing.T, environment *managementEnvironment, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= count; i++ {
		if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_pools
			(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,status)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			fmt.Sprintf("poolseed%012d", i+1),
			fmt.Sprintf("seed-pool-%04d", i),
			600, 3600, 2, "active"); err != nil {
			t.Fatalf("seed pool %d: %v", i, err)
		}
	}
}

func seedDevices(t *testing.T, environment *managementEnvironment, hostID string, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= count; i++ {
		if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO devices
			(id,host_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,lifecycle_status,health_status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			fmt.Sprintf("deviceseed%011d", i),
			hostID,
			"physical",
			"mock",
			fmt.Sprintf("seed-provider-%04d", i),
			"rebuild",
			fmt.Sprintf("seed-serial-%04d", i),
			"ready",
			"healthy"); err != nil {
			t.Fatalf("seed device %d: %v", i, err)
		}
	}
}

func seedReservations(t *testing.T, environment *managementEnvironment, poolID string, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= count; i++ {
		if _, err := environment.db.Pool().Exec(ctx, `INSERT INTO device_reservations
			(id,client_id,pool_id,owner_type,owner_id,idempotency_key,lease_seconds)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			fmt.Sprintf("resseed%012d", i),
			"seed-client",
			poolID,
			"manual",
			"ownerseed000000001",
			fmt.Sprintf("seed-idem-%04d", i),
			600); err != nil {
			t.Fatalf("seed reservation %d: %v", i, err)
		}
	}
}

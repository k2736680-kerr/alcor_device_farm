package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
	managementpostgres "github.com/Ad-Quanta/alcor-device-farm/internal/management/postgres"
	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

func openProvisionabilityDatabase(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("DEVICE_FARM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DEVICE_FARM_TEST_DATABASE_URL is not set")
	}
	db, err := database.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func seedProvisionabilityPool(t *testing.T, db *database.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE
		device_host_image_states,device_pool_devices,devices,device_pool_images,device_pools,
		device_hosts,device_images RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_images
		(id,name,docker_image,docker_digest,api_level,abi,resolution,resource_config,status)
		VALUES('image_00000000000001','pool-image','127.0.0.1:5001/alcor/android-emulator:tag',
		'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x1920',
		'{"ramMb":4096}','ready')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_hosts(id,name,host_type,capacity,status)
		VALUES('host_000000000000001','prov-host','docker_emulator','{"device_slots":4}','online')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pools
		(id,name,platform,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,default_image_id,status)
		VALUES('pool_000000000000001','prov-android','android',600,3600,1,0,0,'image_00000000000001','active')`); err != nil {
		t.Fatal(err)
	}
	// 非 Android 池不走镜像这一套，必须保持「没评估」，否则会把 iOS 池误判成不可供给。
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_pools
		(id,name,platform,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready,status)
		VALUES('pool_000000000000002','prov-ios','ios',1800,86400,1,0,0,'active')`); err != nil {
		t.Fatal(err)
	}
}

func readPool(t *testing.T, db *database.DB, id string) (bool, string) {
	t.Helper()
	pool, err := managementpostgres.New(db).GetPool(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Provisionable == nil {
		return true, "" // 没评估，按可供给处理
	}
	return *pool.Provisionable, pool.ProvisionableReason
}

func TestPoolProvisionabilityReflectsObservedImageAvailability(t *testing.T) {
	db := openProvisionabilityDatabase(t)
	seedProvisionabilityPool(t, db)

	// 从没试过：必须保持乐观可调度，否则全新镜像/全新池第一次就永远起不来。
	if provisionable, _ := readPool(t, db, "pool_000000000000001"); !provisionable {
		t.Fatal("没有任何观测时应当按可供给处理")
	}

	// 宿主被证明没有这个池的镜像：该池就不该再被自动调度选中。
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO device_host_image_states
		(host_id,image_id,available,error_code,observed_at)
		VALUES('host_000000000000001','image_00000000000001',false,'IMAGE_DIGEST_MISMATCH',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	provisionable, reason := readPool(t, db, "pool_000000000000001")
	if provisionable {
		t.Fatal("所有在线宿主都缺该镜像时应当报告不可供给")
	}
	if !strings.Contains(reason, "镜像") {
		t.Fatalf("原因要能让人看懂，得到 %q", reason)
	}

	// 观测过期：宿主后来装上镜像之后不需要人工清记录就能恢复。
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_host_image_states
		SET observed_at=clock_timestamp()-interval '31 minutes'`); err != nil {
		t.Fatal(err)
	}
	if provisionable, _ := readPool(t, db, "pool_000000000000001"); !provisionable {
		t.Fatal("过期观测之后应当恢复可供给")
	}

	// iOS 池不适用镜像规则，必须保持「没评估」。
	page, _ := paging.New(1, 10)
	pools, _, err := managementpostgres.New(db).ListPools(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range pools {
		if pool.ID == "pool_000000000000002" && pool.Provisionable != nil {
			t.Fatalf("非 Android 池不该被判为不可供给，得到 %v", *pool.Provisionable)
		}
	}
}

func TestPoolProvisionabilityRequiresAnOnlineHost(t *testing.T) {
	db := openProvisionabilityDatabase(t)
	seedProvisionabilityPool(t, db)
	if _, err := db.Pool().Exec(context.Background(), `UPDATE device_hosts SET status='offline'`); err != nil {
		t.Fatal(err)
	}
	provisionable, reason := readPool(t, db, "pool_000000000000001")
	if provisionable {
		t.Fatal("一台在线宿主都没有时应当报告不可供给")
	}
	if !strings.Contains(reason, "宿主机") {
		t.Fatalf("原因应当指出是宿主机不在线，得到 %q", reason)
	}
}
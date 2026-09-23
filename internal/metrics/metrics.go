package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo"
	"github.com/Ad-Quanta/alcor-device-farm/internal/database"
)

type httpKey struct {
	method string
	route  string
	status int
}

type httpSample struct {
	count    uint64
	duration time.Duration
}

type Registry struct {
	db               *database.DB
	startedAt        time.Time
	mutex            sync.Mutex
	httpSamples      map[httpKey]httpSample
	collectionErrors atomic.Uint64
}

func New(db *database.DB) *Registry {
	return &Registry{db: db, startedAt: time.Now(), httpSamples: map[httpKey]httpSample{}}
}

func (registry *Registry) ObserveHTTP(method, route string, status int, duration time.Duration) {
	if registry == nil {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "UNKNOWN"
	}
	route = strings.TrimSpace(route)
	route = strings.TrimPrefix(route, method+" ")
	if route == "" {
		route = "unmatched"
	}
	key := httpKey{method: method, route: route, status: status}
	registry.mutex.Lock()
	sample := registry.httpSamples[key]
	sample.count++
	sample.duration += duration
	registry.httpSamples[key] = sample
	registry.mutex.Unlock()
}

func (registry *Registry) Ready(ctx context.Context) error {
	if registry == nil || registry.db == nil {
		return errors.New("database is not configured")
	}
	return registry.databaseReady(ctx)
}

func (registry *Registry) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		http.Error(writer, "不支持当前请求方法", http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	if err := registry.write(ctx, writer); err != nil {
		registry.collectionErrors.Add(1)
	}
}

func (registry *Registry) write(ctx context.Context, output io.Writer) error {
	info := buildinfo.Values()
	metric(output, "device_farm_up", "Device Farm Server process is running.", "gauge", nil, 1)
	metric(output, "device_farm_build_info", "Build information for Device Farm Server.", "gauge", map[string]string{
		"version": info.Version, "commit": info.Commit, "build_date": info.BuildDate,
		"go_version": info.GoVersion, "os": info.OS, "arch": info.Arch,
	}, 1)
	metric(output, "device_farm_process_uptime_seconds", "Device Farm Server process uptime.", "gauge", nil, time.Since(registry.startedAt).Seconds())
	registry.writeHTTPSamples(output)
	metric(output, "device_farm_metric_collection_errors_total", "Metric collection errors.", "counter", nil, float64(registry.collectionErrors.Load()))

	if registry.db == nil {
		metric(output, "device_farm_database_ready", "Whether PostgreSQL is reachable.", "gauge", nil, 0)
		return nil
	}
	if err := registry.databaseReady(ctx); err != nil {
		metric(output, "device_farm_database_ready", "Whether PostgreSQL is reachable.", "gauge", nil, 0)
		return err
	}
	metric(output, "device_farm_database_ready", "Whether PostgreSQL is reachable.", "gauge", nil, 1)
	stats := registry.db.Pool().Stat()
	metric(output, "device_farm_database_connections", "PostgreSQL pool connections by state.", "gauge", map[string]string{"state": "acquired"}, float64(stats.AcquiredConns()))
	sample(output, "device_farm_database_connections", map[string]string{"state": "idle"}, float64(stats.IdleConns()))
	sample(output, "device_farm_database_connections", map[string]string{"state": "total"}, float64(stats.TotalConns()))

	if err := registry.writeDatabaseState(ctx, output); err != nil {
		return err
	}
	return nil
}

func (registry *Registry) databaseReady(ctx context.Context) error {
	if err := registry.db.Pool().Ping(ctx); err != nil {
		return err
	}
	var schemaReady bool
	if err := registry.db.Pool().QueryRow(ctx, `SELECT
		to_regclass('public.devices') IS NOT NULL AND
		to_regclass('public.device_reservations') IS NOT NULL AND
		to_regclass('public.device_host_commands') IS NOT NULL AND
		to_regclass('public.device_idempotency_records') IS NOT NULL`).Scan(&schemaReady); err != nil {
		return err
	}
	if !schemaReady {
		return errors.New("device farm schema is not ready")
	}
	return nil
}

func (registry *Registry) writeHTTPSamples(output io.Writer) {
	registry.mutex.Lock()
	keys := make([]httpKey, 0, len(registry.httpSamples))
	for key := range registry.httpSamples {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})
	samples := make([]httpSample, len(keys))
	for index, key := range keys {
		samples[index] = registry.httpSamples[key]
	}
	registry.mutex.Unlock()

	helpType(output, "device_farm_http_requests_total", "HTTP requests handled by method, route and status.", "counter")
	helpType(output, "device_farm_http_request_duration_seconds", "HTTP request duration summary.", "summary")
	for index, key := range keys {
		labels := labels(map[string]string{"method": key.method, "route": key.route, "status": strconv.Itoa(key.status)})
		fmt.Fprintf(output, "device_farm_http_requests_total%s %d\n", labels, samples[index].count)
		fmt.Fprintf(output, "device_farm_http_request_duration_seconds_sum%s %s\n", labels, formatFloat(samples[index].duration.Seconds()))
		fmt.Fprintf(output, "device_farm_http_request_duration_seconds_count%s %d\n", labels, samples[index].count)
	}
}

func (registry *Registry) writeDatabaseState(ctx context.Context, output io.Writer) error {
	if err := registry.writeGroups(ctx, output, "device_farm_devices", "Devices by lifecycle and health state.",
		`SELECT platform,lifecycle_status,health_status,count(*) FROM devices
		GROUP BY platform,lifecycle_status,health_status ORDER BY platform,lifecycle_status,health_status`,
		"gauge", []string{"platform", "lifecycle_status", "health_status"}); err != nil {
		return err
	}
	if err := registry.writeGroups(ctx, output, "device_farm_pool_target", "Configured Device Pool capacity objectives.",
		`SELECT id,platform,'total',total_target::bigint FROM device_pools WHERE status='active'
		UNION ALL SELECT id,platform,'min_ready',min_ready::bigint FROM device_pools WHERE status='active'
		UNION ALL SELECT id,platform,'max_concurrency',max_concurrency::bigint FROM device_pools WHERE status='active'
		ORDER BY 1,3`, "gauge", []string{"pool_id", "platform", "objective"}); err != nil {
		return err
	}
	if err := registry.writeGroups(ctx, output, "device_farm_pool_devices", "Enabled Pool devices by user-facing availability.",
		`WITH classified AS (
			SELECT pd.pool_id,d.id,CASE
				WHEN d.lifecycle_status='ready' AND d.health_status='healthy' THEN 'available'
				WHEN d.lifecycle_status IN ('reserved','busy') AND d.health_status='healthy' THEN 'in_use'
				WHEN d.lifecycle_status IN ('provisioning','booting') OR
					(d.lifecycle_status='recycling' AND d.health_status='healthy') THEN 'recovering'
				ELSE 'fault' END AS availability
			FROM device_pool_devices pd JOIN devices d ON d.id=pd.device_id
			WHERE pd.enabled AND d.lifecycle_status<>'deleted'
		), states(availability) AS (VALUES ('available'),('in_use'),('recovering'),('fault'))
		SELECT p.id,p.platform,states.availability,count(classified.id)
		FROM device_pools p CROSS JOIN states
		LEFT JOIN classified ON classified.pool_id=p.id AND classified.availability=states.availability
		WHERE p.status='active' GROUP BY p.id,p.platform,states.availability ORDER BY p.id,states.availability`,
		"gauge", []string{"pool_id", "platform", "availability"}); err != nil {
		return err
	}
	if err := registry.writeGroups(ctx, output, "device_farm_reservations", "Reservations by state.",
		`SELECT status,count(*) FROM device_reservations GROUP BY status ORDER BY status`, "gauge", []string{"status"}); err != nil {
		return err
	}
	if err := registry.writeGroups(ctx, output, "device_farm_agents", "Device hosts by Agent-visible state.",
		`SELECT status,count(*) FROM device_hosts GROUP BY status ORDER BY status`, "gauge", []string{"status"}); err != nil {
		return err
	}
	if err := registry.writeGroups(ctx, output, "device_farm_host_commands", "Host commands by state.",
		`SELECT status,count(*) FROM device_host_commands GROUP BY status ORDER BY status`, "gauge", []string{"status"}); err != nil {
		return err
	}
	if err := registry.writeGroups(ctx, output, "device_farm_health_events_total", "Persisted health events by severity.",
		`SELECT severity,count(*) FROM device_health_events GROUP BY severity ORDER BY severity`, "counter", []string{"severity"}); err != nil {
		return err
	}
	var pendingAge, heartbeatAge float64
	if err := registry.db.Pool().QueryRow(ctx, `SELECT coalesce(extract(epoch FROM clock_timestamp()-min(created_at)),0)
		FROM device_reservations WHERE status='pending'`).Scan(&pendingAge); err != nil {
		return err
	}
	if err := registry.db.Pool().QueryRow(ctx, `SELECT coalesce(max(extract(epoch FROM clock_timestamp()-last_heartbeat_at)),0)
		FROM device_hosts WHERE last_heartbeat_at IS NOT NULL`).Scan(&heartbeatAge); err != nil {
		return err
	}
	metric(output, "device_farm_scheduler_oldest_pending_seconds", "Age of the oldest pending reservation.", "gauge", nil, pendingAge)
	metric(output, "device_farm_agent_heartbeat_max_age_seconds", "Maximum age of the latest Host Agent heartbeats.", "gauge", nil, heartbeatAge)
	return nil
}

func (registry *Registry) writeGroups(ctx context.Context, output io.Writer, name, help, query, metricType string, labelNames []string) error {
	rows, err := registry.db.Pool().Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	helpType(output, name, help, metricType)
	for rows.Next() {
		values := make([]string, len(labelNames))
		destinations := make([]any, 0, len(labelNames)+1)
		for index := range values {
			destinations = append(destinations, &values[index])
		}
		var count int64
		destinations = append(destinations, &count)
		if err := rows.Scan(destinations...); err != nil {
			return err
		}
		labelValues := make(map[string]string, len(labelNames))
		for index, name := range labelNames {
			labelValues[name] = values[index]
		}
		fmt.Fprintf(output, "%s%s %d\n", name, labels(labelValues), count)
	}
	return rows.Err()
}

func metric(output io.Writer, name, help, metricType string, labelValues map[string]string, value float64) {
	helpType(output, name, help, metricType)
	sample(output, name, labelValues, value)
}

func sample(output io.Writer, name string, labelValues map[string]string, value float64) {
	fmt.Fprintf(output, "%s%s %s\n", name, labels(labelValues), formatFloat(value))
}

func helpType(output io.Writer, name, help, metricType string) {
	fmt.Fprintf(output, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func labels(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(values[key])
		parts = append(parts, key+`="`+value+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

package reservation

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/Ad-Quanta/alcor-device-farm/internal/audit"
)

// schedulableCapabilityKeys 与迁移 000014 的
// device_schedulable_capabilities() 白名单保持一致。
//
// 这份名单只用来在选池前过滤请求里的噪音键；真正的匹配仍然交给数据库里的
// 同一个 SQL 函数，所以两边不会因为这里漏写某个键而改变语义。
var schedulableCapabilityKeys = map[string]bool{
	"platformName":        true,
	"platformVersion":     true,
	"automationName":      true,
	"deviceClass":         true,
	"realDevice":          true,
	"model":               true,
	"apiLevel":            true,
	"abi":                 true,
	"resolution":          true,
	"hardware_profile_id": true,
}

// androidMinSdkLevelKey 是「设备 API 至少要到多少」的下限要求，不是精确匹配。
//
// 为什么需要它：调用方（Alcor）跑的是 APK，APK 的 minSdk 决定「设备 API >= minSdk」
// 才装得上。而 apiLevel 在这套能力体系里是精确相等匹配，直接用 minSdk 填 apiLevel
// 会变成「要一台正好 API 21 的设备」，把能装的 API 34 设备全排除掉。
//
// 所以它是一个独立键，只有明确携带它的请求才有下限语义；缺省时 apiLevel 仍然是
// 原来的精确匹配，老调用方的行为完全不变。
const androidMinSdkLevelKey = "androidMinSdkLevel"

// minSDKLowerBound 从请求里取出 androidMinSdkLevel 的数值下限，没有则返回 0。
func minSDKLowerBound(requested map[string]any) int {
	value, exists := requested[androidMinSdkLevelKey]
	if !exists {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0
		}
		return int(parsed)
	}
	return 0
}

// 自动选池：调用方只给能力要求，由农场自己挑一个池。
//
// 为什么这件事必须由农场做、而不是由 Alcor 隔着 API 做：
// 「这个池现在供不供得出符合要求的设备」取决于农场的内部状态 —— 镜像在宿主本地
// 仓库（127.0.0.1:5001，同 tag 不同宿主可能是不同内容）、宿主是否在线/draining、
// 池里有没有匹配的活跃设备、iOS 池靠伸缩服务补池还是靠池成员关系。这些都不是
// 一个稳定的对外契约，调用方隔着几个 REST 接口去推断，只能猜个大概，而且每次
// 农场内部实现变化都会把调用方的推断变成错的。
//
// 这里把「能不能服务这次请求」用一条 SQL 直接问出来，并且与真正分配设备的
// allocator（ReservationRepository.LockNextAllocatablePending）用同一套条件，
// 保证「选得出来的池」和「能真分配到设备的池」不会打架。

// PoolSelectionUnavailableError 表示没有任何池能服务这次请求。
//
// 这必须是一个明确的错误，而不是「随便挑一个凑合」：挑一个供不出设备的池，调用方
// 会拿到一个永远 pending 的预约，一直等到租约超时，中间还占住 worker 槽位。
type PoolSelectionUnavailableError struct {
	Platform string
	Reason   string
}

func (err *PoolSelectionUnavailableError) Error() string {
	if err.Reason != "" {
		return "当前没有可服务该请求的设备池：" + err.Reason
	}
	return "当前没有可服务该请求的设备池（平台 " + err.Platform + "）"
}

func (err *PoolSelectionUnavailableError) Is(target error) bool {
	return target == ErrPoolUnavailable
}

// poolSelectionCursor 让同一平台的多个可用池轮流被选中。
//
// 没有它就会永远选中同一个池：那个池到达 max_concurrency 之后新预约只能排队，
// 而其它同样可用的池完全空闲。用进程内游标做加权轮转，比每次随机更容易复现和调试。
var (
	poolSelectionMu     sync.Mutex
	poolSelectionCursor = map[string]int{}
)

// selectPoolForRequest 选出一个能服务 requestedCapabilities 的 active 池。
//
// 返回的池一定满足：平台匹配、active、且**当前真的能供出一台符合请求的设备**
// —— 「已有匹配的活跃设备」或「能按需拉起匹配的设备」二者之一。
func (service *Service) selectPoolForRequest(
	ctx context.Context,
	actor audit.Actor,
	requested map[string]any,
) (string, error) {
	if service == nil || service.db == nil {
		return "", ErrPoolUnavailable
	}
	platformName := ""
	if value, exists := requested["platformName"]; exists {
		if text, ok := value.(string); ok {
			platformName = strings.ToLower(strings.TrimSpace(text))
		}
	}
	candidates, err := service.listSelectablePools(ctx, platformName, requested)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		if platformName == "" {
			return "", &PoolSelectionUnavailableError{Reason: "请求里没有可识别的 platformName"}
		}
		return "", &PoolSelectionUnavailableError{Platform: platformName}
	}
	return selectWeightedPool(platformName, candidates), nil
}

// selectablePool 是一个候选池及其容量权重。
type selectablePool struct {
	ID             string
	MaxConcurrency int
}

// listSelectablePools 用一条 SQL 找出所有能服务这次请求的池。
//
// 条件分两部分：
//
//	A. 池内已有可直接分配的匹配设备 —— 与 allocator 的条件逐条对应。
//	B. 池没有可直接分配的设备，但农场能按需拉起匹配设备（base 设备或默认镜像），
//	   且至少有一台宿主能真正创建它（在线、不 draining、未被证明缺该镜像）。
//
// B 是必要的：预热池在缩到 0、或设备还没建好时，A 不成立，但这个池其实完全可用。
// 只看 A 会把「运行都挤向唯一有活跃设备的池」，让它到 max_concurrency 后干等，
// 而另一个能按需拉起的池闲置。
func (service *Service) listSelectablePools(
	ctx context.Context,
	platformName string,
	requested map[string]any,
) ([]selectablePool, error) {
	if platformName == "" {
		return nil, nil
	}
	encoded, err := encodeRequestedCapabilities(requested)
	if err != nil {
		return nil, err
	}
	minSDK := minSDKLowerBound(requested)
	rows, err := service.db.Pool().Query(ctx, `
		SELECT p.id, p.max_concurrency
		FROM device_pools p
		WHERE p.status = 'active' AND lower(p.platform) = $1
		  AND (
		    -- A. 池内已有匹配且可直接分配的活跃设备
		    EXISTS (
		      SELECT 1
		      FROM devices d
		      JOIN device_pool_devices pd ON pd.device_id = d.id
		      JOIN device_hosts h ON h.id = d.host_id
		      WHERE pd.pool_id = p.id AND pd.enabled
		        AND p.platform = d.platform
		        AND h.status = 'online' AND NOT h.draining
		        AND d.lifecycle_status = 'ready' AND d.health_status = 'healthy'
		        AND d.capabilities @> device_schedulable_capabilities($2::jsonb)
		        -- androidMinSdkLevel 是下限：设备 API 必须 >= 它，而不是等于它。
		        AND ($3 = 0 OR COALESCE(
		              NULLIF(d.capabilities->>'apiLevel', '')::int, 0) >= $3)
		    )
		    OR (
		      -- B. 池没有可直接分配的设备，但能按需拉起匹配的设备
		      NOT EXISTS (
		        SELECT 1
		        FROM devices d2
		        JOIN device_pool_devices pd2 ON pd2.device_id = d2.id
		        WHERE pd2.pool_id = p.id AND pd2.enabled
		          AND d2.lifecycle_status NOT IN ('deleted','stopped')
		      )
		      AND p.default_image_id IS NOT NULL
		      AND EXISTS (
		        SELECT 1
		        FROM device_pool_images pi
		        JOIN device_images i ON i.id = pi.image_id
		        JOIN device_hosts h ON h.status = 'online' AND NOT h.draining
		                         AND h.host_type IN ('docker_emulator','hybrid')
		        WHERE pi.pool_id = p.id AND pi.enabled AND pi.image_id = p.default_image_id
		          AND i.status = 'ready'
		          -- 镜像的 apiLevel 必须满足请求的能力要求（精确包含语义）
		          AND i.api_level::text = COALESCE(
		              device_schedulable_capabilities($2::jsonb)->>'apiLevel',
		              i.api_level::text)
		          -- 下限要求：镜像的 API 也要 >= minSdk。
		          AND ($3 = 0 OR i.api_level >= $3)
		          -- 这台宿主没有被证明缺少该镜像（负向记录永久有效）
		          AND NOT EXISTS (
		            SELECT 1 FROM device_host_image_states s
		            WHERE s.host_id = h.id AND s.image_id = p.default_image_id
		              AND s.available = false
		          )
		      )
		    )
		  )
		ORDER BY p.id`, platformName, encoded, minSDK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]selectablePool, 0)
	for rows.Next() {
		var candidate selectablePool
		if err := rows.Scan(&candidate.ID, &candidate.MaxConcurrency); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

// selectWeightedPool 按 max_concurrency 加权轮转挑一个池。
//
// 权重用池自己的并发容量：容量大的池自然分到更多运行，不需要调用方在选池阶段声明
// 「我这批要多少并发」—— 运行是一次一个提交的，调用方本来也无从知道。
func selectWeightedPool(platformName string, candidates []selectablePool) string {
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	total := 0
	for _, candidate := range candidates {
		total += poolSelectionWeight(candidate)
	}
	if total <= 0 {
		total = len(candidates)
	}
	poolSelectionMu.Lock()
	defer poolSelectionMu.Unlock()
	cursor := poolSelectionCursor[platformName] % total
	selected := candidates[0].ID
	for _, candidate := range candidates {
		weight := poolSelectionWeight(candidate)
		if cursor < weight {
			selected = candidate.ID
			break
		}
		cursor -= weight
	}
	poolSelectionCursor[platformName] = (poolSelectionCursor[platformName] + 1) % total
	return selected
}

func poolSelectionWeight(candidate selectablePool) int {
	if candidate.MaxConcurrency > 0 {
		return candidate.MaxConcurrency
	}
	return 1
}

// encodeRequestedCapabilities 把请求能力编码成 jsonb 字面量，供
// device_schedulable_capabilities() 做精确包含匹配。
//
// 只保留那套白名单里的键：请求里若混入农场不认的键（例如 maxConcurrency），直接塞进
// jsonb 不会影响 @> 语义，但会让排查噪音变大。这里统一经过白名单。
//
// 下划线开头的内部键（例如 _device_farm_target_device_id）一律剔除：那是农场自己在
// 预约记录里存的内部标记，不是调用方可以要求的能力。
//
// androidMinSdkLevel 也在这里剔除：它由 minSDKLowerBound 单独按「>=」处理，
// 不能进 @> 匹配 —— 那是精确相等语义，会让 minSdk=21 变成「只要 API 21」。
func encodeRequestedCapabilities(requested map[string]any) ([]byte, error) {
	filtered := make(map[string]any, len(requested))
	for key, value := range requested {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || strings.HasPrefix(trimmed, "_") {
			continue
		}
		if trimmed == androidMinSdkLevelKey {
			continue
		}
		if !schedulableCapabilityKeys[trimmed] {
			continue
		}
		filtered[trimmed] = value
	}
	return json.Marshal(filtered)
}
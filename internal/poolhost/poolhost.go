// Package poolhost 负责「池与宿主机的调度归属」这一条规则的单一定义。
//
// 背景：多台 Android 宿主机各有一个独立池（如 `android-10.0.30.54`），
// 池名里带着宿主地址。但**建机**（internal/warmpool）和**选池**
// （internal/reservation）原先都不看这个归属：
//
//   - 建机侧：任何在线安卓宿主都是任何池的候选，实际退化成按 host id 塞满，
//     出现「预约指定 54、设备建到 55」的串台（正式服双向复现）。
//   - 选池侧：判「这个池现在能不能服务」时用的是**任意**在线安卓宿主，
//     所以可能说「能」而目标宿主其实建不出来（缺镜像/没内存），
//     请求被选中后一直排队，表现为假通过。
//
// 两侧必须用**同一个**推导规则，否则又会不一致。这个包就是那份唯一定义：
// 从池名抽取完整 IPv4，按 `device_hosts.address` 匹配。
//
// 设计约束（见 docs/01_existing_capability_reuse_matrix.md 与
// docs/02_architecture_alignment.md）：不新增表、字段或 API。
// 池名是既有的运维可读约定，这里只是把名字里**本来就有的**宿主线索用于调度。
package poolhost

import "fmt"

// AddressFromPoolName 从池名里推导该池所属宿主机的地址。
//
// 只接受**完整 IPv4**：四段、每段 0-255、左右边界严格。
// 抽不到（如 `default-android`、`android-16-api36`）返回空串，
// 调用方按「该池未声明宿主归属」处理。
//
// 严格性是刻意的：`android-16-api36` 里的 16 不是地址，
// 把版本号误判成地址会让调度收到一个永远匹配不到的归属上。
func AddressFromPoolName(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			continue
		}
		// 左边必须是分隔符或串首，避免把 `abc123.4.5.6` 里的片段当地址。
		if i > 0 {
			previous := name[i-1]
			if previous != '-' && previous != ' ' && previous != '_' && previous != '/' {
				continue
			}
		}
		octets := make([]int, 0, 4)
		position := i
		matched := true
		for len(octets) < 4 {
			start := position
			for position < len(name) && name[position] >= '0' && name[position] <= '9' {
				position++
			}
			if start == position {
				matched = false
				break
			}
			value := 0
			for _, digit := range name[start:position] {
				value = value*10 + int(digit-'0')
				if value > 255 {
					break
				}
			}
			if value > 255 {
				matched = false
				break
			}
			octets = append(octets, value)
			if len(octets) == 4 {
				break
			}
			if position >= len(name) || name[position] != '.' {
				matched = false
				break
			}
			position++
		}
		if !matched || len(octets) != 4 {
			continue
		}
		// 右边必须是边界，避免匹配到更长数字串的前缀。
		if position < len(name) {
			next := name[position]
			if (next >= '0' && next <= '9') || next == '.' {
				continue
			}
		}
		return fmt.Sprintf("%d.%d.%d.%d", octets[0], octets[1], octets[2], octets[3])
	}
	return ""
}

// Scope 描述一个池的宿主归属在 SQL 里该怎么收窄。
//
// Address 为空表示「该池没有声明宿主归属」，调用方应保持原行为（全农场任选）。
// Known 为 false 表示池名声明了地址、但农场里没有这台宿主（改名、写错 IP、
// 或宿主已被删）—— 此时也必须回退，否则该池会被永久饿死。
type Scope struct {
	Address string
	Known   bool
}

// ResolveScope 判定池名的宿主归属是否可用。
//
// exists 由调用方按当前事务里的 device_hosts 现状提供
// （布尔查询，避免把「宿主是否存在」这个关注点混进这里）。
func ResolveScope(poolName string, exists func(address string) bool) Scope {
	address := AddressFromPoolName(poolName)
	if address == "" {
		return Scope{}
	}
	return Scope{Address: address, Known: exists != nil && exists(address)}
}

// Applies 报告是否应按 Address 收窄候选宿主。
func (scope Scope) Applies() bool { return scope.Address != "" && scope.Known }
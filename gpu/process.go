package gpu

import (
	"context"
	"sort"
	"sync"
	"time"
)

// UtilizationUnknown 利用率未知
// 进程级 GPU 利用率由相邻两次采样的引擎时间差分得到，首次采样时返回该值
const UtilizationUnknown = -1.0

// ProcessStat 进程级 GPU 占用
// Devices 为设备标识，多卡进程会有多个；Linux 下为 /dev/dri 节点名或 nvidia 序号
// Vendor 取占用显存最多的设备所属厂商，无法判定时为空
type ProcessStat struct {
	PID         int32    `json:"pid"`         // 进程 ID
	Name        string   `json:"name"`        // 进程名
	Vendor      string   `json:"vendor"`      // 厂商：nvidia | amd | intel | 空
	MemoryUsed  uint64   `json:"memoryUsed"`  // 显存占用（字节）
	Utilization float64  `json:"utilization"` // GPU 利用率（0-100），首次采样为 UtilizationUnknown
	Devices     []string `json:"devices"`     // 占用的设备标识

	engineNs map[string]uint64 // 各引擎累计 GPU 时间（纳秒），仅用于计算利用率
}

// processEngineSample 上一次采样的引擎时间快照
type processEngineSample struct {
	name    string
	engines map[string]uint64
	at      time.Time
}

var processEngineSamples = struct {
	sync.Mutex
	values map[int32]processEngineSample
}{
	values: make(map[int32]processEngineSample),
}

// GetProcessStats 采集所有正在使用 GPU 的进程及其占用，按显存占用降序排列
// Linux：优先 DRM fdinfo（内核通用接口，覆盖 AMD / Intel / NVIDIA 及多数开源驱动），
// 缺失时由 nvidia-smi 补齐显存（闭源驱动未暴露 fdinfo 的场景）
// 其他平台：暂无公开接口，返回空列表
func GetProcessStats(ctx context.Context) ([]*ProcessStat, error) {
	stats, err := collectGPUProcesses(ctx)
	if len(stats) == 0 {
		return nil, err
	}

	applyProcessUtilization(stats, time.Now())

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].MemoryUsed == stats[j].MemoryUsed {
			return stats[i].PID < stats[j].PID
		}
		return stats[i].MemoryUsed > stats[j].MemoryUsed
	})

	return stats, err
}

// applyProcessUtilization 用相邻两次采样计算进程 GPU 利用率
// 同一进程可能同时使用多个引擎（渲染 / 计算 / 编码），引擎并行工作，
// 因此取其峰值而不是累加值，否则会超过 100%
func applyProcessUtilization(list []*ProcessStat, now time.Time) {
	processEngineSamples.Lock()
	defer processEngineSamples.Unlock()

	next := make(map[int32]processEngineSample, len(list))

	for _, stat := range list {
		stat.Utilization = UtilizationUnknown

		if len(stat.engineNs) > 0 {
			previous, ok := processEngineSamples.values[stat.PID]
			elapsed := now.Sub(previous.at).Seconds()
			// 进程名不同视为 PID 已被复用，避免用两代进程的数据做差分
			if ok && previous.name == stat.Name && elapsed > 0 {
				var peak float64
				var matched bool
				for name, current := range stat.engineNs {
					base, exists := previous.engines[name]
					if !exists || current < base {
						continue
					}
					matched = true
					usage := float64(current-base) / 1e9 / elapsed * 100
					if usage > peak {
						peak = usage
					}
				}
				if matched {
					if peak > 100 {
						peak = 100
					}
					stat.Utilization = peak
				}
			}
			next[stat.PID] = processEngineSample{name: stat.Name, engines: stat.engineNs, at: now}
		}
	}

	processEngineSamples.values = next
}

// appendDevice 追加设备标识并去重
func appendDevice(stat *ProcessStat, device string) {
	if device == "" {
		return
	}
	for _, exist := range stat.Devices {
		if exist == device {
			return
		}
	}
	stat.Devices = append(stat.Devices, device)
}

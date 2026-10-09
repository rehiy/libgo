//go:build !linux

package gpu

import "context"

// collectGPUProcesses 非 Linux 平台的进程级 GPU 占用采集
// 这些平台没有内核通用接口（DRM fdinfo），仅能用 nvidia-smi 补齐 NVIDIA 显存与卡号
// Windows 另有 PDH "GPU Engine" 计数器可获取利用率，macOS 无公开接口，均待后续支持
func collectGPUProcesses(ctx context.Context) ([]*ProcessStat, error) {
	stats := make(map[int32]*ProcessStat)

	err := collectNvidiaProcessApps(ctx, stats)
	if len(stats) == 0 {
		return nil, err
	}

	list := make([]*ProcessStat, 0, len(stats))
	for _, stat := range stats {
		list = append(list, stat)
	}
	return list, err
}

// readProcComm 非 Linux 平台没有 /proc，进程名留空
func readProcComm(pid int32) string {
	return ""
}

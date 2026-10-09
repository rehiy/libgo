//go:build linux

package gpu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// collectGPUProcesses 采集 Linux 进程级 GPU 占用
// 通用路径为内核 DRM fdinfo（厂商无关），专有工具仅用于补齐缺失数据
func collectGPUProcesses(ctx context.Context) ([]*ProcessStat, error) {
	var collectErr error
	stats := make(map[int32]*ProcessStat)

	if hasDRMDevices() {
		collected, err := collectDRMProcesses()
		if err != nil {
			collectErr = errors.Join(collectErr, err)
		}
		if len(collected) > 0 {
			stats = collected
		}
	}

	// NVIDIA 闭源驱动部分版本未暴露 DRM fdinfo，用 nvidia-smi 补齐显存与卡号
	if err := collectNvidiaProcessApps(ctx, stats); err != nil {
		collectErr = errors.Join(collectErr, err)
	}

	if len(stats) == 0 {
		return nil, collectErr
	}

	list := make([]*ProcessStat, 0, len(stats))
	for _, stat := range stats {
		list = append(list, stat)
	}
	return list, collectErr
}

// hasDRMDevices 判断系统是否存在 DRM 设备节点，不存在时跳过 /proc 扫描
func hasDRMDevices() bool {
	info, err := os.Stat("/dev/dri")
	return err == nil && info.IsDir()
}

// collectDRMProcesses 扫描 /proc 中所有进程的 DRM 文件描述符
func collectDRMProcesses() (map[int32]*ProcessStat, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	vendors := drmDeviceVendors()
	result := make(map[int32]*ProcessStat)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, runtime.NumCPU()*2) // 限制并发，避免进程过多时耗尽文件描述符
	)

	for _, entry := range entries {
		pid, err := strconv.ParseInt(entry.Name(), 10, 32)
		if err != nil || pid <= 0 {
			continue
		}

		wg.Add(1)
		go func(pid int32) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			stat := scanProcessDRM(pid, vendors)
			if stat == nil {
				return
			}
			mu.Lock()
			result[pid] = stat
			mu.Unlock()
		}(int32(pid))
	}

	wg.Wait()
	return result, nil
}

// scanProcessDRM 采集单个进程打开的 DRM 设备占用情况
func scanProcessDRM(pid int32, vendors map[string]string) *ProcessStat {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}

	fdInfoDir := fmt.Sprintf("/proc/%d/fdinfo", pid)
	devices := make(map[string]bool)
	stat := &ProcessStat{
		PID:         pid,
		Name:        readProcComm(pid),
		Utilization: UtilizationUnknown,
	}

	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil || !strings.HasPrefix(link, "/dev/dri/") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(fdInfoDir, fd.Name()))
		if err != nil {
			continue
		}

		memory, engines := parseDRMFdInfo(data)
		stat.MemoryUsed += memory

		for name, nanos := range engines {
			if stat.engineNs == nil {
				stat.engineNs = make(map[string]uint64)
			}
			// 同名引擎在多张卡上并行工作，取峰值而非累加
			if nanos > stat.engineNs[name] {
				stat.engineNs[name] = nanos
			}
		}

		devices[filepath.Base(link)] = true
	}

	if len(devices) == 0 {
		return nil
	}

	for device := range devices {
		stat.Devices = append(stat.Devices, device)
		if stat.Vendor == "" {
			stat.Vendor = vendors[device]
		}
	}
	sort.Strings(stat.Devices)

	return stat
}

// parseDRMFdInfo 解析 DRM fdinfo，返回显存占用（字节）与各引擎累计 GPU 时间（纳秒）
// 独立显存优先统计 vram；核显等无独立显存的场景退化为 system / gtt 等共享内存区域
func parseDRMFdInfo(data []byte) (uint64, map[string]uint64) {
	var (
		memoryVRAM  uint64
		memoryOther uint64
		hasVRAM     bool
		engines     map[string]uint64
	)

	for _, line := range strings.Split(string(data), "\n") {
		key, value, unit := splitFdInfoLine(line)

		switch {
		case strings.HasPrefix(key, "drm-memory-"):
			size := parseSizeBytes(value, unit)
			if size == 0 {
				continue
			}
			if strings.Contains(strings.TrimPrefix(key, "drm-memory-"), "vram") {
				memoryVRAM += size
				hasVRAM = true
			} else {
				memoryOther += size
			}

		case strings.HasPrefix(key, "drm-engine-"):
			nanos, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				continue
			}
			if engines == nil {
				engines = make(map[string]uint64)
			}
			name := strings.TrimPrefix(key, "drm-engine-")
			engines[name] += nanos
		}
	}

	if hasVRAM {
		return memoryVRAM, engines
	}
	return memoryOther, engines
}

// splitFdInfoLine 拆分 fdinfo 行，形如 "drm-memory-vram:\t4096 KiB"
func splitFdInfoLine(line string) (key, value, unit string) {
	pos := strings.Index(line, ":")
	if pos < 0 {
		return "", "", ""
	}

	key = strings.TrimSpace(line[:pos])
	fields := strings.Fields(line[pos+1:])
	if len(fields) == 0 {
		return key, "", ""
	}
	if len(fields) > 1 {
		unit = fields[1]
	}
	return key, fields[0], unit
}

// drmDeviceVendors 建立 DRM 设备节点到厂商的映射
func drmDeviceVendors() map[string]string {
	entries, err := filepath.Glob("/sys/class/drm/*")
	if err != nil {
		return nil
	}

	vendors := make(map[string]string, len(entries))
	for _, entry := range entries {
		vid := readSysfsStr(filepath.Join(entry, "device", "vendor"))
		if vid == "" {
			continue
		}
		vendors[filepath.Base(entry)] = vendorByVendorID(vid)
	}
	return vendors
}

// vendorByVendorID 将 PCI vendor ID 映射为统一的小写厂商名
func vendorByVendorID(vid string) string {
	vid = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(vid)), "0x")
	switch vid {
	case "10de":
		return "nvidia"
	case "1002":
		return "amd"
	case "8086":
		return "intel"
	}
	return ""
}

// readProcComm 读取进程名
func readProcComm(pid int32) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

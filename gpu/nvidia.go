package gpu

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	ghwgpu "github.com/jaypipes/ghw/pkg/gpu"
)

func collectNvidiaCards(ctx context.Context, cards []*ghwgpu.GraphicsCard) ([]*DeviceStat, error) {
	return collectNvidiaSMI(ctx, cards)
}

// collectNvidiaProcessApps 用 nvidia-smi 补齐进程显存与卡号
// 仅在 DRM fdinfo 未覆盖或未给出显存时生效，避免与内核数据重复统计
func collectNvidiaProcessApps(ctx context.Context, known map[int32]*ProcessStat) error {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil || path == "" {
		return nil
	}

	cmdCtx, cancel := commandContext(ctx)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, path,
		"--query-compute-apps=pid,used_memory,gpu_uuid",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return err
	}

	records, err := readCSVRecords(out)
	if err != nil {
		return err
	}

	indexByUUID := nvidiaGPUIndex(ctx)

	for _, fields := range records {
		if len(fields) < 3 {
			continue
		}
		pid := parseIntOrDefault(strings.TrimSpace(fields[0]), 0)
		if pid <= 0 {
			continue
		}

		// nounits 下 used_memory 的单位为 MiB
		memory := parseSizeBytes(strings.TrimSpace(fields[1]), "MiB")
		uuid := strings.TrimSpace(fields[2])

		stat, exists := known[int32(pid)]
		if !exists {
			stat = &ProcessStat{
				PID:         int32(pid),
				Name:        readProcComm(int32(pid)),
				Vendor:      "nvidia",
				MemoryUsed:  memory,
				Utilization: UtilizationUnknown,
			}
			known[int32(pid)] = stat
		} else if stat.MemoryUsed == 0 {
			stat.MemoryUsed = memory
			if stat.Vendor == "" {
				stat.Vendor = "nvidia"
			}
		}

		if idx, ok := indexByUUID[uuid]; ok {
			appendDevice(stat, "nvidia"+idx)
		}
	}

	return nil
}

// nvidiaGPUIndex 建立 GPU UUID 到序号的映射，用于把进程归属到具体卡号
func nvidiaGPUIndex(ctx context.Context) map[string]string {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil || path == "" {
		return nil
	}

	cmdCtx, cancel := commandContext(ctx)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, path,
		"--query-gpu=index,uuid", "--format=csv,noheader",
	).Output()
	if err != nil {
		return nil
	}

	records, err := readCSVRecords(out)
	if err != nil {
		return nil
	}

	index := make(map[string]string, len(records))
	for _, fields := range records {
		if len(fields) < 2 {
			continue
		}
		index[strings.TrimSpace(fields[1])] = strings.TrimSpace(fields[0])
	}
	return index
}

// nvidia-smi fallback
func collectNvidiaSMI(ctx context.Context, cards []*ghwgpu.GraphicsCard) ([]*DeviceStat, error) {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil || path == "" {
		return nil, nil
	}

	cmdCtx, cancel := commandContext(ctx)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, path,
		"--query-gpu=index,pci.bus_id,name,memory.used,memory.total,utilization.gpu,temperature.gpu,power.draw,fan.speed",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil, err
	}

	records, err := readCSVRecords(out)
	if err != nil {
		return nil, err
	}

	allowed := addressSet(cards)
	var stats []*DeviceStat
	for _, fields := range records {
		if len(fields) < 9 {
			continue
		}

		busID := normalizePCIAddress(strings.TrimSpace(fields[1]))
		if len(allowed) > 0 && !allowed[busID] {
			continue
		}

		name := strings.TrimSpace(fields[2])
		memUsedMB, _ := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
		memTotalMB, _ := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)
		util, _ := strconv.ParseFloat(strings.TrimSpace(fields[5]), 64)

		stats = append(stats, &DeviceStat{
			Address:     busID,
			Name:        name,
			Vendor:      "nvidia",
			MemoryUsed:  uint64(memUsedMB * 1024 * 1024),
			MemoryTotal: uint64(memTotalMB * 1024 * 1024),
			Utilization: util,
			Temperature: parseIntOrDefault(strings.TrimSpace(fields[6]), -1),
			PowerUsage:  parseFloatOrDefault(strings.TrimSpace(fields[7]), -1),
			FanSpeed:    parseIntOrDefault(strings.TrimSpace(fields[8]), -1),
		})
	}
	return stats, nil
}

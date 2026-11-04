package psutil

import (
	"cmp"
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
)

// CPUStat 区分型号、系统报告的处理器封装、物理核心和逻辑处理器。
// 未知数量为 nil；Processors 仅包含平台能够确认的逐逻辑处理器记录。
type CPUStat struct {
	Models        []string       `json:"models"`        // 非空型号去重，不表示封装数
	Packages      *int           `json:"packages"`      // 已报告逻辑处理器涉及的封装数；未知为 null
	PhysicalCores *int           `json:"physicalCores"` // 物理核心总数；未知为 null
	LogicalCPUs   *int           `json:"logicalCPUs"`   // 系统报告的逻辑处理器总数；未知为 null
	Processors    []CPUProcessor `json:"processors"`    // 可识别的逻辑处理器，按 ID 排序
}

// CPUProcessor 是一个逻辑处理器，不是一个插槽或物理核心。
// CoreID 仅在同一 PackageID 内有意义；任一 ID 为 nil 时不能据此归并物理核心。
type CPUProcessor struct {
	ID        int32   `json:"id"`        // 系统逻辑处理器 ID
	PackageID *string `json:"packageId"` // 系统封装 ID；未知为 null，"0" 是有效 ID
	CoreID    *string `json:"coreId"`    // 封装内物理核心 ID；未知为 null
	Model     string  `json:"model"`     // 此逻辑处理器报告的型号
	Vendor    string  `json:"vendor"`    // 此逻辑处理器报告的厂商
}

// CPUInfo 查询 CPU 静态信息，不采集使用率。返回部分结果及采集错误，不伪造缺失拓扑。
// 当前依赖仅在 Linux 提供可靠的逐逻辑处理器 ID；其他平台返回型号和可确认的数量。
// 步骤之间检查取消，但不能中断底层同步读取；不一致计数置为未知并返回部分错误。
func CPUInfo(ctx context.Context) (*CPUStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	infos, infoErr := cpu.InfoWithContext(ctx)
	var physical, logical int
	var physicalErr, logicalErr error
	if ctx.Err() == nil {
		physical, physicalErr = cpu.CountsWithContext(ctx, false)
	}
	if ctx.Err() == nil {
		logical, logicalErr = cpu.CountsWithContext(ctx, true)
	}
	if physicalErr != nil {
		physical = 0
	}
	if logicalErr != nil {
		logical = 0
	}
	stat, statErr := buildCPUStat(infos, physical, logical, runtime.GOOS)
	if infoErr != nil {
		stat.Packages = nil
	}
	return stat, errors.Join(infoErr, physicalErr, logicalErr, statErr, ctx.Err())
}

// ─── 辅助函数 ───

func buildCPUStat(infos []cpu.InfoStat, physical, logical int, platform string) (*CPUStat, error) {
	stat := &CPUStat{Models: []string{}, Processors: []CPUProcessor{}}
	if logical > 0 {
		stat.LogicalCPUs = new(logical)
	}
	// 其余平台的 Counts(false) 可能直接返回逻辑处理器数，不能宣称是物理核心数。
	if physical > 0 && (platform == "linux" || platform == "windows" || platform == "darwin") {
		stat.PhysicalCores = new(physical)
	}
	var err error
	if platform == "linux" && logical > 0 && len(infos) == 0 {
		err = errors.Join(err, errors.New("psutil: CPU information unavailable"))
	}

	packages := make(map[string]bool)
	cores := make(map[[2]string]bool)
	seen := make(map[int32]bool)
	complete := platform == "linux" && logical > 0 && len(infos) == logical
	for _, info := range infos {
		model := strings.TrimSpace(info.ModelName)
		if model != "" && !slices.Contains(stat.Models, model) {
			stat.Models = append(stat.Models, model)
		}
		if platform != "linux" {
			continue
		}
		if info.CPU < 0 || seen[info.CPU] {
			complete = false
			continue
		}
		seen[info.CPU] = true
		processor := CPUProcessor{
			ID: info.CPU, PackageID: cpuTopologyID(info.PhysicalID), CoreID: cpuTopologyID(info.CoreID),
			Model: model, Vendor: strings.TrimSpace(info.VendorID),
		}
		stat.Processors = append(stat.Processors, processor)
		if processor.PackageID != nil && processor.CoreID != nil {
			cores[[2]string{*processor.PackageID, *processor.CoreID}] = true
		}
		if processor.PackageID == nil {
			complete = false
		} else {
			packages[*processor.PackageID] = true
		}
	}
	if logical > 0 && len(stat.Processors) > logical {
		stat.LogicalCPUs = nil
		err = errors.Join(err, errors.New("psutil: logical CPU count is smaller than identified processors"))
	}
	if stat.PhysicalCores != nil && ((logical > 0 && physical > logical) || physical < len(cores)) {
		stat.PhysicalCores = nil
		err = errors.Join(err, errors.New("psutil: physical core count conflicts with CPU topology"))
	}
	if complete {
		stat.Packages = new(len(packages))
	}
	slices.Sort(stat.Models)
	slices.SortFunc(stat.Processors, func(a, b CPUProcessor) int { return cmp.Compare(a.ID, b.ID) })
	return stat, err
}

func cpuTopologyID(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") {
		return nil
	}
	return new(value)
}

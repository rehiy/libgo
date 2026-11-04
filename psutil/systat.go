package psutil

import (
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

func Summary(withAddr bool) *SummaryStat {
	hi, _ := host.Info()
	cl, _ := cpu.Counts(true)
	cc, _ := cpu.Counts(false)
	cp, _ := cpu.Percent(time.Second, false)
	mv, _ := mem.VirtualMemory()

	stat := &SummaryStat{
		CreateAt:     time.Now().Unix(),
		HostId:       hi.HostID,
		HostName:     hi.Hostname,
		Uptime:       hi.Uptime,
		OS:           hi.OS,
		Platform:     hi.Platform,
		KernelArch:   hi.KernelArch,
		CPUCore:      cc,
		CPUCoreLogic: cl,
		CpuPercent:   cp,
		MemoryTotal:  mv.Total,
		MemoryUsed:   mv.Used,
	}

	if withAddr {
		ipv4, ipv6 := PublicAddress(false)
		stat.PublicIPv4 = ipv4
		stat.PublicIPv6 = ipv6
	}

	return stat
}

func Detail(withAddr bool) *DetailStat {
	ci, cpuErr := cpu.Info()
	ni, _ := net.IOCounters(true)
	di, _ := disk.IOCounters()
	dp, _ := disk.Partitions(false)
	sw, _ := mem.SwapMemory()

	// 网络信息

	netInterface := []NetInterface{}
	netBytesRecv := uint64(0)
	netBytesSent := uint64(0)
	for _, nio := range ni {
		if nio.BytesRecv > 0 || nio.BytesSent > 0 {
			ift := NetInterface{
				Name:      nio.Name,
				BytesRecv: nio.BytesRecv,
				BytesSent: nio.BytesSent,
				Dropin:    nio.Dropin,
				Dropout:   nio.Dropout,
			}
			if withAddr {
				ift.Ipv4List, ift.Ipv6List = InterfaceAddrs(nio.Name)
			}
			netInterface = append(netInterface, ift)
		}
		netBytesRecv += nio.BytesRecv
		netBytesSent += nio.BytesSent
	}

	// 硬盘信息

	diskPartition := []DiskPartition{}
	diskTotaled := ","
	diskTotal := uint64(0)
	diskUsed := uint64(0)
	for _, dpi := range dp {
		du, _ := disk.Usage(dpi.Mountpoint)
		if du.Total > 0 || du.Used > 0 {
			diskPartition = append(diskPartition, DiskPartition{
				dpi.Device,
				dpi.Mountpoint, dpi.Fstype,
				du.Total, du.Used,
			})
		}
		if !strings.Contains(diskTotaled, dpi.Device) {
			diskTotaled += dpi.Device + ","
			diskTotal += du.Total
			diskUsed += du.Used
		}
	}

	// 硬盘 IO 信息

	diskReadBytes := uint64(0)
	diskWriteBytes := uint64(0)
	for _, dio := range di {
		diskReadBytes += dio.ReadBytes
		diskWriteBytes += dio.WriteBytes
	}

	// 汇总信息

	summary := Summary(withAddr)
	cpuStat, _ := buildCPUStat(ci, summary.CPUCore, summary.CPUCoreLogic, runtime.GOOS)
	if cpuErr != nil {
		cpuStat.Packages = nil
	}
	return &DetailStat{
		SummaryStat:  summary,
		CPU:          cpuStat,
		NetInterface: netInterface, NetBytesRecv: netBytesRecv, NetBytesSent: netBytesSent,
		DiskPartition: diskPartition, DiskTotal: diskTotal, DiskUsed: diskUsed,
		DiskReadBytes: diskReadBytes, DiskWriteBytes: diskWriteBytes,
		SwapTotal: sw.Total, SwapUsed: sw.Used,
	}
}

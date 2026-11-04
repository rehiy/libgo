package psutil

import (
	"encoding/json"
)

// Go 内存信息

type GoMemoryStat struct {
	Alloc        uint64 `note:"已分配内存" json:"alloc"`
	Sys          uint64 `note:"已申请内存" json:"sys"`
	HeapAlloc    uint64 `note:"堆已分配内存" json:"heapAlloc"`
	HeapInuse    uint64 `note:"堆已使用内存" json:"heapInuse"`
	HeapIdle     uint64 `note:"堆空闲内存" json:"heapIdle"`
	HeapReleased uint64 `note:"堆已释放内存" json:"heapReleased"`
	HeapObjects  uint64 `note:"堆对象数量" json:"heapObjects"`
	HeapSys      uint64 `note:"堆已申请内存" json:"heapSys"`
	StackInuse   uint64 `note:"栈已使用内存" json:"stackInuse"`
	StackSys     uint64 `note:"栈已申请内存" json:"stackSys"`
	TotalAlloc   uint64 `note:"累计已分配内存" json:"totalAlloc"`
	LastGC       uint64 `note:"最后一次 GC 时间" json:"lastGC"`
	NumGC        uint32 `note:"GC 执行次数" json:"numGC"`
}

// 系统概要信息

type SummaryStat struct {
	CreateAt     int64     `note:"创建时间" json:"createAt"`
	HostId       string    `note:"主机 ID" json:"hostId"`
	HostName     string    `note:"主机名" json:"hostName"`
	Uptime       uint64    `note:"运行时间" json:"uptime"`
	OS           string    `note:"操作系统" json:"os"`
	Platform     string    `note:"平台" json:"platform"`
	KernelArch   string    `note:"内核架构" json:"kernelArch"`
	CPUCore      int       `note:"CPU 核心数" json:"cpuCore"`
	CPUCoreLogic int       `note:"CPU 逻辑核心数" json:"cpuCoreLogic"`
	CpuPercent   []float64 `note:"CPU 使用率" json:"cpuPercent"`
	MemoryTotal  uint64    `note:"内存总量" json:"memoryTotal"`
	MemoryUsed   uint64    `note:"内存使用量" json:"memoryUsed"`
	PublicIPv4   string    `note:"公网 IPV4" json:"publicIpv4"`
	PublicIPv6   string    `note:"公网 IPV6" json:"publicIpv6"`
}

func (p *SummaryStat) From(s string) {
	json.Unmarshal([]byte(s), p)
}

func (p *SummaryStat) String() string {
	jsonbyte, _ := json.Marshal(p)
	return string(jsonbyte)
}

// 系统统计详情

type DetailStat struct {
	*SummaryStat
	CPU            *CPUStat        `note:"CPU 型号及可确认的拓扑" json:"cpu"`
	NetInterface   []NetInterface  `note:"网卡信息" json:"netInterface"`
	NetBytesRecv   uint64          `note:"网卡接收字节数" json:"netBytesRecv"`
	NetBytesSent   uint64          `note:"网卡发送字节数" json:"netBytesSent"`
	DiskPartition  []DiskPartition `note:"硬盘分区信息" json:"diskPartition"`
	DiskTotal      uint64          `note:"硬盘总量" json:"diskTotal"`
	DiskUsed       uint64          `note:"硬盘使用量" json:"diskUsed"`
	DiskReadBytes  uint64          `note:"硬盘读取字节数" json:"diskReadBytes"`
	DiskWriteBytes uint64          `note:"硬盘写入字节数" json:"diskWriteBytes"`
	SwapTotal      uint64          `note:"交换分区总量" json:"swapTotal"`
	SwapUsed       uint64          `note:"交换分区使用量" json:"swapUsed"`
}

// 硬盘分区信息

type DiskPartition struct {
	Device     string `note:"设备名" json:"device"`
	Mountpoint string `note:"挂载点" json:"mountpoint"`
	Fstype     string `note:"文件系统" json:"fstype"`
	Total      uint64 `note:"总量" json:"total"`
	Used       uint64 `note:"使用量" json:"used"`
}

// 网卡信息

type NetInterface struct {
	Name      string   `note:"网卡名称" json:"name"`
	BytesRecv uint64   `note:"接收字节数" json:"bytesRecv"`
	BytesSent uint64   `note:"发送字节数" json:"bytesSent"`
	Dropin    uint64   `note:"丢弃的接收包" json:"dropin"`
	Dropout   uint64   `note:"丢弃的发送包" json:"dropout"`
	Ipv4List  []string `note:"IPV4 列表" json:"ipv4List"`
	Ipv6List  []string `note:"IPV6 列表" json:"ipv6List"`
}

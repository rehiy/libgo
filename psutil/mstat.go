package psutil

import (
	"runtime"
)

// GoMemory 获取 Go 内存统计信息
func GoMemory() *GoMemoryStat {
	mstat := &runtime.MemStats{}
	runtime.ReadMemStats(mstat)

	return &GoMemoryStat{
		Sys:          mstat.Sys,
		HeapAlloc:    mstat.HeapAlloc,
		HeapInuse:    mstat.HeapInuse,
		HeapIdle:     mstat.HeapIdle,
		HeapReleased: mstat.HeapReleased,
		HeapObjects:  mstat.HeapObjects,
		HeapSys:      mstat.HeapSys,
		StackInuse:   mstat.StackInuse,
		StackSys:     mstat.StackSys,
		TotalAlloc:   mstat.TotalAlloc,
		LastGC:       uint64(mstat.LastGC / 1e9),
		NumGC:        mstat.NumGC,
	}
}

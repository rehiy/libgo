package gpu

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var ioregKVPattern = regexp.MustCompile(`"([^"]+)"\s*=\s*(.+)`)

func collectAppleGPUs(ctx context.Context) ([]*DeviceStat, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}

	path, err := exec.LookPath("ioreg")
	if err != nil || path == "" {
		return nil, nil
	}

	cmdCtx, cancel := commandContext(ctx)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, path, "-r", "-c", "AGXAccelerator", "-d", "1").Output()
	if err != nil {
		return nil, err
	}

	return parseIORegGPUs(out), nil
}

func parseIORegGPUs(data []byte) []*DeviceStat {
	var stats []*DeviceStat
	var current map[string]string

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "+-o ") {
			if stat := buildAppleStat(current); stat != nil {
				stats = append(stats, stat)
			}
			current = make(map[string]string)
			continue
		}

		m := ioregKVPattern.FindStringSubmatch(line)
		if m == nil || current == nil {
			continue
		}
		current[m[1]] = m[2]
	}

	if stat := buildAppleStat(current); stat != nil {
		stats = append(stats, stat)
	}
	return stats
}

func buildAppleStat(kv map[string]string) *DeviceStat {
	if len(kv) == 0 {
		return nil
	}

	name := ioregUnquote(kv["model"])
	if name == "" {
		return nil
	}

	stat := &DeviceStat{
		Name:        name,
		Vendor:      "apple",
		Temperature: -1,
		PowerUsage:  -1,
		FanSpeed:    -1,
	}

	perfRaw := kv["PerformanceStatistics"]
	if perfRaw == "" {
		return stat
	}
	perf := parseIORegDict(perfRaw)

	stat.Utilization = parseFloatOrDefault(perf["Device Utilization %"], 0)
	stat.MemoryUsed = ioregParseUint64(perf["In use system memory"])
	stat.MemoryTotal = ioregParseUint64(perf["Alloc system memory"])

	return stat
}

// parseIORegDict 解析 ioreg 的 dict 格式：{"key1"=val1,"key2"=val2}
func parseIORegDict(s string) map[string]string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	result := make(map[string]string)
	for pair := range strings.SplitSeq(s, ",") {
		rawKey, rawVal, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key := ioregUnquote(strings.TrimSpace(rawKey))
		val := strings.TrimSpace(rawVal)
		result[key] = val
	}
	return result
}

func ioregUnquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func ioregParseUint64(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

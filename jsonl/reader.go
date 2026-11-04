package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

// Cursor 沿单个 Store 的按日追加文件保存读取位置，零值可用，不支持并发调用。
// 仅消费以换行结尾的完整行；不支持已读文件的原地改写或替换。
type Cursor struct {
	date   string
	offset int64
}

// ScanSince 增量扫描保留窗口内的完整行，时间字段和过滤规则与 ScanSince 相同。
// 首次或跨日扫描先 Flush 当前写入缓冲，但不等待异步队列，也不触发采集。
// 缺失文件跳过，过期游标从 cutoff 所在日恢复；需要重读时使用新的 Cursor。
// handler 返回 false 停止全部扫描，该行已消费；取消或错误保留已消费位置。
// 回调中的 []byte 仅本次调用有效；调用方负责保存结果和清理过期数据。
func (c *Cursor) ScanSince(ctx context.Context, s *Store, cutoffUnix int64, tsField string, extra Filter, handler LineHandler) error {
	if s == nil {
		return errors.New("jsonl: nil store")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().Unix()
	if cutoffUnix > now {
		return nil
	}
	dates := daysInRange(cutoffUnix, now, s.opts.loc)
	if c.date != dates[len(dates)-1] {
		if err := s.Flush(); err != nil {
			return err
		}
	}
	if c.date < dates[0] || c.date > dates[len(dates)-1] {
		c.date, c.offset = dates[0], 0
	}
	filter := And(tsGTEFilter(tsField, cutoffUnix), extra)
	for _, date := range dates {
		if date < c.date {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if date != c.date {
			c.date, c.offset = date, 0
		}
		stopped, err := scanFile(s.FilePath(date), &c.offset, filter, handler, ctx.Err)
		if err != nil || stopped {
			return err
		}
	}
	return ctx.Err()
}

// DecodeSince 泛型封装，将匹配行反序列化为 []T，跳过坏行。
func (s *Store) DecodeSince[T any](cutoffUnix int64, tsField string, extra Filter) ([]T, error) {
	var out []T
	err := ScanSince(s, cutoffUnix, tsField, extra, func(line []byte) bool {
		var v T
		if err := json.Unmarshal(line, &v); err != nil {
			return true // 跳过解析失败的行
		}
		out = append(out, v)
		return true
	})
	return out, err
}

// DecodeSinceSampled 从 cutoffUnix 起扫描多天文件，并按时间桶降采样到约 sampleSize 个点。
// 同一时间桶内保留最后一条记录，返回顺序为从旧到新。
func (s *Store) DecodeSinceSampled[T any](cutoffUnix int64, tsField string, extra Filter, sampleSize int) ([]T, error) {
	if sampleSize <= 0 {
		return s.DecodeSince[T](cutoffUnix, tsField, extra)
	}

	window := max(time.Now().Unix()-cutoffUnix, 1)
	bucketSeconds := max((window+int64(sampleSize)-1)/int64(sampleSize), 1)
	type sampledLine struct {
		ts   int64
		line []byte
	}
	buckets := make(map[int64]sampledLine, sampleSize)
	maxBucket := int64(sampleSize - 1)
	err := ScanSince(s, cutoffUnix, tsField, extra, func(line []byte) bool {
		ts, ok := ParseTimestamp(line, tsField)
		if !ok {
			return true
		}
		bucket := min((ts-cutoffUnix)/bucketSeconds, maxBucket)
		prev, exists := buckets[bucket]
		if exists && prev.ts > ts {
			return true
		}
		dup := bytes.Clone(line)
		buckets[bucket] = sampledLine{ts: ts, line: dup}
		return true
	})
	if err != nil {
		return nil, err
	}

	out := make([]T, 0, len(buckets))
	for bucket := int64(0); bucket <= maxBucket; bucket++ {
		item, ok := buckets[bucket]
		if !ok {
			continue
		}
		var v T
		if json.Unmarshal(item.line, &v) == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

// DecodeTailDays 从当前日期向前回扫 days 天，倒序取行直到达到 limit。
// 适用于跨天查询最近 N 条记录（如计划任务执行历史）。
func (s *Store) DecodeTailDays[T any](days, limit int, filter Filter) ([]T, error) {
	if s == nil || days <= 0 || limit <= 0 {
		return nil, nil
	}
	out := make([]T, 0, limit)
	now := time.Now().In(s.opts.loc)
	for back := 0; back < days && len(out) < limit; back++ {
		date := now.AddDate(0, 0, -back).Format(dateLayout)
		path := s.FilePath(date)
		lines, err := tailLines(path, limit-len(out), filter)
		if err != nil {
			return nil, fmt.Errorf("jsonl: tail %s: %w", path, err)
		}
		out = append(out, decodeLines[T](lines)...)
	}
	return out, nil
}

// ScanFile 按行扫描文件，filter 为 nil 不过滤；handler 返回 false 提前结束。
// 缺失文件跳过，单行最大 1 MiB；保留普通扫描读取 EOF 无换行尾行的行为。
// 注意：handler 中的 []byte 仅在本次回调有效。
func ScanFile(path string, filter Filter, handler LineHandler) error {
	_, err := scanFile(path, nil, filter, handler, nil)
	return err
}

// ScanSince 从 cutoffUnix 起扫描多天文件，tsField 支持 Unix 秒和 RFC3339。
// extra 可为 nil；handler 返回 false 时停止所有日期的扫描。
func ScanSince(s *Store, cutoffUnix int64, tsField string, extra Filter, handler LineHandler) error {
	if s == nil {
		return errors.New("jsonl: nil store")
	}
	filter := And(tsGTEFilter(tsField, cutoffUnix), extra)
	for _, date := range daysInRange(cutoffUnix, time.Now().Unix(), s.opts.loc) {
		path := s.FilePath(date)
		stopped, err := scanFile(path, nil, filter, handler, nil)
		if err != nil {
			return fmt.Errorf("jsonl: scan %s: %w", path, err)
		}
		if stopped {
			break
		}
	}
	return nil
}

// DecodeTail 泛型封装，将匹配行反序列化为 []T。
func DecodeTail[T any](path string, n int, filter Filter) ([]T, error) {
	lines, err := tailLines(path, n, filter)
	if err != nil {
		return nil, err
	}
	return decodeLines[T](lines), nil
}

// ─── 辅助函数 ───

// scanFile 的 check 为 nil 时不取消；offset 非 nil 时按快照增量读取完整行。
func scanFile(path string, offset *int64, filter Filter, handler LineHandler, check func() error) (bool, error) {
	if check != nil {
		if err := check(); err != nil {
			return false, err
		}
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	var reader io.Reader = file
	if offset != nil {
		info, err := file.Stat()
		if err != nil {
			return false, err
		}
		if _, err := file.Seek(*offset, io.SeekStart); err != nil {
			return false, err
		}
		reader = io.LimitReader(file, max(info.Size()-*offset, 0))
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	if offset != nil {
		scanner.Split(scanCompleteLines)
	}
	stopped := false
	for scanner.Scan() {
		if check != nil {
			if err := check(); err != nil {
				return false, err
			}
		}
		line := scanner.Bytes()
		if offset != nil {
			*offset += int64(len(line) + 1)
			line = bytes.TrimSuffix(line, []byte{'\r'})
		}
		if len(line) == 0 || (filter != nil && !filter(line)) {
			continue
		}
		if !handler(line) {
			stopped = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return stopped, err
	}
	if check != nil {
		return stopped, check()
	}
	return stopped, nil
}

// scanCompleteLines 不消费未换行的末行，后续增量读取从该行起点继续。
func scanCompleteLines(data []byte, _ bool) (int, []byte, error) {
	if end := bytes.IndexByte(data, '\n'); end >= 0 {
		return end + 1, data[:end], nil
	}
	return 0, nil, nil
}

func decodeLines[T any](lines [][]byte) []T {
	out := make([]T, 0, len(lines))
	for _, line := range lines {
		var value T
		if json.Unmarshal(line, &value) == nil {
			out = append(out, value)
		}
	}
	return out
}

// tailLines 从文件末尾向前读最近 n 条匹配行，返回"由新到旧"。
// 以 32KB 块反向读取，避免全量加载。
func tailLines(path string, n int, filter Filter) ([][]byte, error) {
	if n <= 0 {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := stat.Size()
	if size == 0 {
		return nil, nil
	}

	const chunkSize = 32 * 1024
	buf := make([]byte, chunkSize)
	var carry []byte
	result := make([][]byte, 0, n)
	pos := size
	for pos > 0 && len(result) < n {
		readSize := min(pos, int64(chunkSize))
		pos -= readSize

		if _, err := f.ReadAt(buf[:readSize], pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		merged := make([]byte, 0, int(readSize)+len(carry))
		merged = append(merged, buf[:readSize]...)
		merged = append(merged, carry...)

		end := len(merged)
		for i, m := range slices.Backward(merged) {
			if m != '\n' {
				continue
			}
			line := merged[i+1 : end]
			end = i
			if len(line) == 0 {
				continue
			}
			if filter != nil && !filter(line) {
				continue
			}
			dup := bytes.Clone(line)
			result = append(result, dup)
			if len(result) >= n {
				return result, nil
			}
		}
		carry = bytes.Clone(merged[:end])

		if pos == 0 && len(carry) > 0 {
			line := carry
			carry = nil
			if filter == nil || filter(line) {
				dup := bytes.Clone(line)
				result = append(result, dup)
			}
		}
	}
	return result, nil
}

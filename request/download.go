package request

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cheggaaa/pb/v3"
)

func Download(url, target string, isGzip bool) (string, error) {
	return DownloadContext(context.Background(), url, target, isGzip)
}

// DownloadContext binds the request and body read to ctx. An optional client
// allows callers to configure transport and redirect policy without global changes.
func DownloadContext(ctx context.Context, url, target string, isGzip bool, clients ...*http.Client) (string, error) {
	client := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}

	req := &Client{Method: http.MethodGet, Url: url}
	resp, err := req.DoContext(ctx, client)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// 默认读取器
	reader := resp.Body

	// 显示下载进度
	bar := pb.StartNew(int(resp.ContentLength))
	bar.Set(pb.Bytes, true) // 自动换为合适的单位
	reader = bar.NewProxyReader(reader)
	defer reader.Close()
	defer bar.Finish()

	// 自动解压 gz 文件
	if isGzip || resp.Header.Get("Content-Encoding") == "gzip" {
		reader, err = gzip.NewReader(reader)
		if err != nil {
			return "", err
		}
	}

	// 返回文件的名称
	return SaveStream(reader, target)
}

func SaveStream(reader io.Reader, target string) (string, error) {
	var err error
	var writer *os.File

	// 创建目标文件
	if target != "" {
		writer, err = os.Create(target)
	} else {
		writer, err = os.CreateTemp("", "tmp-*")
	}
	if err != nil {
		return "", err
	}
	defer writer.Close()

	// 写入文件数据
	_, err = io.Copy(writer, reader)
	if err = errors.Join(err, writer.Close()); err != nil {
		os.Remove(writer.Name())
		return "", err
	}

	return writer.Name(), nil
}

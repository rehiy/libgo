package archive

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rehiy/libgo/filer"
)

// Zipper 归档服务
type Zipper struct{}

// NewZipper 创建归档服务
func NewZipper() *Zipper {
	return &Zipper{}
}

// Zip 创建归档文件
func (zs *Zipper) Zip(path string) error {
	if !filer.Exist(path) {
		return os.ErrNotExist
	}

	zipFilePath := filepath.Join(filepath.Dir(path), filepath.Base(path)+".zip")

	f, err := os.Create(zipFilePath)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(f)

	if filer.IsDir(path) {
		err = zipDir(zw, path)
	} else {
		err = zipFile(zw, path)
	}

	// 先关闭 writer 和 file，再删除（Windows 上未关闭的文件不可删除）
	zw.Close()
	f.Close()

	if err != nil {
		os.Remove(zipFilePath)
	}
	return err
}

// zipFile 将单个文件写入 zip
func zipFile(zw *zip.Writer, srcPath string) error {
	w, err := zw.Create(filepath.Base(srcPath))
	if err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// zipDir 将目录下所有文件递归写入 zip
func zipDir(zw *zip.Writer, srcPath string) error {
	baseName := filepath.Base(srcPath)
	return filepath.WalkDir(srcPath, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(srcPath, filePath)
		if err != nil {
			return err
		}
		// zip 规范要求内部路径使用正斜杠
		w, err := zw.Create(filepath.ToSlash(filepath.Join(baseName, relPath)))
		if err != nil {
			return err
		}
		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

// Unzip 解压归档文件
func (zs *Zipper) Unzip(path string) error {
	extractDir := filepath.Dir(path)
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, f := range reader.File {
		// 将 zip 内路径分隔符统一为系统分隔符，防止 Windows 下路径拼接异常
		name := filepath.FromSlash(f.Name)

		// 防止 Zip Slip 攻击
		destPath := filepath.Join(extractDir, name)
		rel, err := filepath.Rel(extractDir, destPath)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

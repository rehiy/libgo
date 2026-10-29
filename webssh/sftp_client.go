package webssh

import (
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/rehiy/libgo/filer"
)

// FileInfo SFTP 文件/目录信息
type FileInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModTime    int64  `json:"modTime"`
	IsDir      bool   `json:"isDir"`
	IsLink     bool   `json:"isLink"`
	LinkTarget string `json:"linkTarget,omitempty"`
}

// ListResult 目录列表结果（含实际路径）
type ListResult struct {
	Path  string      `json:"path"`
	Files []*FileInfo `json:"files"`
}

// sftpEntry 连接池中的一个缓存条目
type sftpEntry struct {
	conn     *sftp.Client
	lastUsed time.Time
}

// SFTPClient SFTP 客户端，内置连接池，按 user@addr 复用连接
type SFTPClient struct {
	mu          sync.Mutex
	entries     map[string]*sftpEntry
	idleTimeout time.Duration
	stopCh      chan struct{}
}

// NewSFTPClient 创建 SFTP 客户端，idleTimeout 为 0 时使用默认值 30s
func NewSFTPClient(idleTimeout time.Duration) *SFTPClient {
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Second
	}
	c := &SFTPClient{
		entries:     make(map[string]*sftpEntry),
		idleTimeout: idleTimeout,
		stopCh:      make(chan struct{}),
	}
	go c.evictLoop()
	return c
}

// Close 关闭客户端，释放所有连接
func (c *SFTPClient) Close() {
	close(c.stopCh)
	// 先在锁内取出所有连接，再在锁外关闭，避免持锁期间阻塞
	c.mu.Lock()
	conns := make([]*sftp.Client, 0, len(c.entries))
	for _, e := range c.entries {
		conns = append(conns, e.conn)
	}
	c.entries = make(map[string]*sftpEntry)
	c.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

// List 列出目录内容，dirPath 为空或 "~" 时使用远程 home 目录
func (c *SFTPClient) List(opt *SSHClientOption, dirPath string) (*ListResult, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return nil, err
	}

	if dirPath == "" || dirPath == "~" {
		if wd, err := conn.Getwd(); err == nil {
			dirPath = wd
		} else {
			dirPath = "/"
		}
	}

	entries, err := conn.ReadDir(dirPath)
	if err != nil {
		c.invalidate(opt)
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	files := make([]*FileInfo, 0, len(entries))
	for _, e := range entries {
		isDir := false
		var linkTarget string
		isLink := e.Mode()&os.ModeSymlink != 0
		if isLink {
			fullPath := path.Join(dirPath, e.Name())
			// 符号链接：使用 Stat 获取链接目标的信息来判断是否为目录
			if info, err := conn.Stat(fullPath); err == nil {
				isDir = info.IsDir()
			}
			// 使用 ReadLink 获取符号链接目标
			if target, err := conn.ReadLink(fullPath); err == nil {
				linkTarget = target
			}
		} else {
			// 非符号链接：直接使用 e.IsDir()
			isDir = e.IsDir()
		}
		files = append(files, &FileInfo{
			Name:       e.Name(),
			Size:       e.Size(),
			Mode:       e.Mode().String(),
			ModTime:    e.ModTime().Unix(),
			IsDir:      isDir,
			IsLink:     isLink,
			LinkTarget: linkTarget,
		})
	}
	return &ListResult{Path: dirPath, Files: files}, nil
}

// DirSize 递归计算目录大小（包含所有子目录和文件）
func (c *SFTPClient) DirSize(opt *SSHClientOption, dirPath string) (int64, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return 0, err
	}

	var totalSize int64
	walker := conn.Walk(dirPath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			c.invalidate(opt)
			return 0, fmt.Errorf("遍历目录失败: %w", err)
		}
		info := walker.Stat()
		if info == nil {
			continue
		}
		// 跳过符号链接，避免循环或重复计算
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		// 只累加文件大小，目录本身的大小（通常是 4096）不计入
		if !info.IsDir() {
			totalSize += info.Size()
		}
	}
	return totalSize, nil
}

// Download 下载远程文件到 dst（使用 WriteTo 优化传输）
func (c *SFTPClient) Download(opt *SSHClientOption, filePath string, dst io.Writer) error {
	conn, err := c.dial(opt)
	if err != nil {
		return err
	}

	f, err := conn.Open(filePath)
	if err != nil {
		conn.Close()
		return fmt.Errorf("打开文件失败: %w", err)
	}

	defer func() {
		f.Close()
		conn.Close()
	}()

	_, err = f.WriteTo(dst)
	return err
}

// Upload 上传 src 到远程 destPath（使用 ReadFrom 优化传输）
func (c *SFTPClient) Upload(opt *SSHClientOption, destPath string, src io.Reader) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}

	f, err := conn.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		// OpenFile 失败（如权限不足）不代表连接断开，不 invalidate
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()

	if _, err := f.ReadFrom(src); err != nil {
		// 写入失败（如磁盘满）不代表连接断开，不 invalidate
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// Remove 删除文件或目录；recursive=true 时递归删除目录
func (c *SFTPClient) Remove(opt *SSHClientOption, targetPath string, recursive bool) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}

	if recursive {
		if err := conn.RemoveAll(targetPath); err != nil {
			c.invalidate(opt)
			return fmt.Errorf("递归删除失败: %w", err)
		}
	} else {
		if err := conn.Remove(targetPath); err != nil {
			c.invalidate(opt)
			return fmt.Errorf("删除失败: %w", err)
		}
	}
	return nil
}

// Mkdir 递归创建目录
func (c *SFTPClient) Mkdir(opt *SSHClientOption, dirPath string) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.MkdirAll(dirPath); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("创建目录失败: %w", err)
	}
	return nil
}

// Rename 重命名/移动文件或目录
func (c *SFTPClient) Rename(opt *SSHClientOption, oldPath, newPath string) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Rename(oldPath, newPath); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("重命名失败: %w", err)
	}
	return nil
}

// Stat 获取文件/目录信息（跟随符号链接）
func (c *SFTPClient) Stat(opt *SSHClientOption, p string) (os.FileInfo, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return nil, err
	}
	info, err := conn.Stat(p)
	if err != nil {
		c.invalidate(opt)
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}
	return info, nil
}

// Lstat 获取文件信息（不跟随符号链接，返回链接本身的信息）
func (c *SFTPClient) Lstat(opt *SSHClientOption, p string) (os.FileInfo, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return nil, err
	}
	info, err := conn.Lstat(p)
	if err != nil {
		c.invalidate(opt)
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}
	return info, nil
}

// ReadLink 读取符号链接的目标路径
func (c *SFTPClient) ReadLink(opt *SSHClientOption, p string) (string, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return "", err
	}
	target, err := conn.ReadLink(p)
	if err != nil {
		c.invalidate(opt)
		return "", fmt.Errorf("读取符号链接失败: %w", err)
	}
	return target, nil
}

// Symlink 创建符号链接，newname 指向 oldname
func (c *SFTPClient) Symlink(opt *SSHClientOption, oldname, newname string) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Symlink(oldname, newname); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("创建符号链接失败: %w", err)
	}
	return nil
}

// Link 创建硬链接，newname 指向 oldname 的同一个 inode
func (c *SFTPClient) Link(opt *SSHClientOption, oldname, newname string) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Link(oldname, newname); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("创建硬链接失败: %w", err)
	}
	return nil
}

// Chmod 修改文件权限
func (c *SFTPClient) Chmod(opt *SSHClientOption, path string, mode os.FileMode) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Chmod(path, mode); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("修改权限失败: %w", err)
	}
	return nil
}

// Chown 修改文件所有者和组
func (c *SFTPClient) Chown(opt *SSHClientOption, path string, uid, gid int) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Chown(path, uid, gid); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("修改所有者失败: %w", err)
	}
	return nil
}

// Chtimes 修改文件访问时间和修改时间
func (c *SFTPClient) Chtimes(opt *SSHClientOption, path string, atime, mtime time.Time) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Chtimes(path, atime, mtime); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("修改时间失败: %w", err)
	}
	return nil
}

// PosixRename 重命名文件，如果目标存在则替换（使用 posix-rename@openssh.com 扩展）
func (c *SFTPClient) PosixRename(opt *SSHClientOption, oldname, newname string) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.PosixRename(oldname, newname); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("POSIX 重命名失败: %w", err)
	}
	return nil
}

// StatVFS 获取文件系统统计信息（需要服务器支持 statvfs@openssh.com 扩展）
func (c *SFTPClient) StatVFS(opt *SSHClientOption, path string) (*sftp.StatVFS, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return nil, err
	}
	stat, err := conn.StatVFS(path)
	if err != nil {
		c.invalidate(opt)
		return nil, fmt.Errorf("获取文件系统信息失败: %w", err)
	}
	return stat, nil
}

// RealPath 规范化路径（解析 ".."、相对路径等）
func (c *SFTPClient) RealPath(opt *SSHClientOption, path string) (string, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return "", err
	}
	realPath, err := conn.RealPath(path)
	if err != nil {
		c.invalidate(opt)
		return "", fmt.Errorf("解析真实路径失败: %w", err)
	}
	return realPath, nil
}

// Truncate 截断文件到指定大小
func (c *SFTPClient) Truncate(opt *SSHClientOption, path string, size int64) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}
	if err := conn.Truncate(path, size); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("截断文件失败: %w", err)
	}
	return nil
}

// IsTextFile 判断远程文件是否为文本文件（判断规则同 filer.IsTextBytes）
func (c *SFTPClient) IsTextFile(opt *SSHClientOption, filePath string) (bool, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return false, err
	}

	f, err := conn.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	buf, err := filer.ReadSniff(f)
	if err != nil {
		return false, fmt.Errorf("读取文件失败: %w", err)
	}
	return filer.IsTextBytes(buf), nil
}

// Glob 根据模式匹配文件路径（如 *.txt, /path/*.log）
// 返回匹配的文件路径列表
func (c *SFTPClient) Glob(opt *SSHClientOption, pattern string) ([]string, error) {
	conn, err := c.getConn(opt)
	if err != nil {
		return nil, err
	}

	matches, err := conn.Glob(pattern)
	if err != nil {
		c.invalidate(opt)
		return nil, fmt.Errorf("模式匹配失败: %w", err)
	}
	return matches, nil
}

// Move 移动文件或目录（实际上是重命名的封装，支持跨目录移动）
// 如果目标路径已存在，根据 overwrite 参数决定是否覆盖
func (c *SFTPClient) Move(opt *SSHClientOption, srcPath, dstPath string, overwrite bool) error {
	conn, err := c.getConn(opt)
	if err != nil {
		return err
	}

	// 检查目标是否存在
	if info, err := conn.Stat(dstPath); err == nil {
		if !overwrite {
			return fmt.Errorf("目标路径已存在: %s", dstPath)
		}
		// 如果目标是目录，且源也是目录，需要特殊处理
		if info.IsDir() {
			// 目标存在且是目录，可以将源移动到目标目录下
			dstPath = path.Join(dstPath, path.Base(srcPath))
		}
		// 如果目标存在且允许覆盖，先删除目标
		if err := conn.Remove(dstPath); err != nil {
			c.invalidate(opt)
			return fmt.Errorf("删除目标路径失败: %w", err)
		}
	}

	if err := conn.Rename(srcPath, dstPath); err != nil {
		c.invalidate(opt)
		return fmt.Errorf("移动文件失败: %w", err)
	}
	return nil
}

// ── 内部方法 ────────────────────────────────────────────────────────────────

// connKey 从 opt 派生连接池 key，格式为 user@addr
func connKey(opt *SSHClientOption) string {
	return opt.User + "@" + opt.Addr
}

// dial 建立一个新的独占 SSH+SFTP 连接
func (c *SFTPClient) dial(opt *SSHClientOption) (*sftp.Client, error) {
	sshClient, err := NewSSHClient(opt)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %w", err)
	}
	conn, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("SFTP 初始化失败: %w", err)
	}
	return conn, nil
}

// getConn 从连接池获取连接，不存在或已失效则新建
// 使用 CAS 风格：先取出旧连接探活，失败后用 dial 建新连接，
// 最后在锁内做 check-then-set，避免并发时 double-close 和连接泄漏。
func (c *SFTPClient) getConn(opt *SSHClientOption) (*sftp.Client, error) {
	key := connKey(opt)

	// 1. 取出当前缓存的连接（快速路径）
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()

	if ok {
		if _, err := e.conn.Getwd(); err == nil {
			c.mu.Lock()
			// 再次确认 entry 未被其他 goroutine 替换
			if cur, still := c.entries[key]; still && cur == e {
				e.lastUsed = time.Now()
			}
			c.mu.Unlock()
			return e.conn, nil
		}
		// 探活失败：在锁内确认仍是同一个 entry 再删除，防止重复 close
		c.mu.Lock()
		if cur, still := c.entries[key]; still && cur == e {
			delete(c.entries, key)
			c.mu.Unlock()
			e.conn.Close() // 锁外关闭，避免阻塞
		} else {
			c.mu.Unlock()
		}
	}

	// 2. 建立新连接
	conn, err := c.dial(opt)
	if err != nil {
		return nil, err
	}

	// 3. 写入连接池：若并发时已有其他 goroutine 写入，关闭多余连接
	c.mu.Lock()
	if existing, ok := c.entries[key]; ok {
		c.mu.Unlock()
		conn.Close() // 丢弃多余连接
		return existing.conn, nil
	}
	c.entries[key] = &sftpEntry{conn: conn, lastUsed: time.Now()}
	c.mu.Unlock()
	return conn, nil
}

// invalidate 主动移除某个连接的缓存（连接出错时调用）
func (c *SFTPClient) invalidate(opt *SSHClientOption) {
	key := connKey(opt)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.conn.Close()
		delete(c.entries, key)
	}
}

// evictLoop 定期清理空闲超时的连接
func (c *SFTPClient) evictLoop() {
	ticker := time.NewTicker(c.idleTimeout / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// 先在锁内收集过期连接并从 map 删除，再在锁外关闭，避免持锁期间阻塞
			var expired []*sftp.Client
			c.mu.Lock()
			for key, e := range c.entries {
				if time.Since(e.lastUsed) > c.idleTimeout {
					expired = append(expired, e.conn)
					delete(c.entries, key)
				}
			}
			c.mu.Unlock()
			for _, conn := range expired {
				conn.Close()
			}
		case <-c.stopCh:
			return
		}
	}
}

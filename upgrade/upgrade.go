package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/rehiy/libgo/logman"
	"github.com/rehiy/libgo/request"
)

// UpdateInfo 更新信息
type UpdateInfo struct {
	Type     string `json:"type,omitempty" note:"更新方式"`
	Error    string `json:"error,omitempty" note:"错误信息"`
	Message  string `json:"message,omitempty" note:"提示信息"`
	Release  string `json:"release,omitempty" note:"更新说明"`
	Version  string `json:"version,omitempty" note:"最新版本"`
	Package  string `json:"package,omitempty" note:"下载地址"`
	Checksum string `json:"checksum,omitempty" note:"SHA256 校验和"`
}

// ErrRollback 回滚错误
type ErrRollback struct {
	Err      error
	Rollback error
}

func (e *ErrRollback) Error() string {
	if e.Rollback == nil {
		return e.Err.Error()
	}
	return e.Err.Error() + "; rollback failed: " + e.Rollback.Error()
}

func (e *ErrRollback) Unwrap() error {
	return e.Err
}

// ErrNoUpdate 无需更新
var ErrNoUpdate = errors.New("no update")

// DownloadHandler is responsible for retrieving a package and materializing the local binary path.
// It receives the package URL and the default output path for the new binary.
// It may download a raw binary or an archive and return the final binary path.
type DownloadHandler func(pkgURL, outputPath string) (string, error)

// Updater handles remote update checks and local binary replacement.
type Updater struct {
	ctx        context.Context
	Server     string
	Version    string
	TargetPath string
	TargetMode os.FileMode
	Download   DownloadHandler // Custom downloads must honor the constructor context.
	HTTPClient *http.Client    // Optional client for checks and default downloads.
}

// NewUpdater creates an update manager using a background context.
func NewUpdater(server, version string) *Updater {
	return NewUpdaterContext(context.Background(), server, version)
}

// NewUpdaterContext creates an update manager bound to ctx for its lifetime.
func NewUpdaterContext(ctx context.Context, server, version string) *Updater {
	return &Updater{ctx: ctx, Server: server, Version: version}
}

// Check fetches update metadata using the constructor context.
func (u *Updater) Check() (*UpdateInfo, error) {
	info := &UpdateInfo{}
	if err := u.ctx.Err(); err != nil {
		return info, err
	}

	urlStr, err := u.buildUpdateURL()
	if err != nil {
		return info, err
	}

	client := u.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	req := &request.Client{Method: http.MethodGet, Url: urlStr}
	res, err := req.DoContext(u.ctx, client)
	if err != nil {
		return info, err
	}

	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return info, fmt.Errorf("update server returned HTTP %d", res.StatusCode)
	}

	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(info)
	if err != nil {
		return info, err
	}

	if info.Error != "" {
		return info, errors.New(info.Error)
	}
	if info.Package == "" {
		return info, ErrNoUpdate
	}

	return info, nil
}

// Apply checks and applies an update using the constructor context.
// A custom Download hook must handle cancellation; it cannot be forcibly interrupted.
func (u *Updater) Apply() error {
	logger := logman.Named("upgrade")
	logger.Info(
		"checking update",
		"version", u.Version,
		"url", u.Server,
	)

	info, err := u.Check()
	if err != nil {
		if errors.Is(err, ErrNoUpdate) {
			logger.Info("no update available", "info", info)
			return ErrNoUpdate
		}
		logger.Error("check update failed", "error", err)
		return err
	}

	if !strings.HasPrefix(info.Package, "https://") {
		logger.Info("no need to update", "info", info)
		return ErrNoUpdate
	}

	binaryUpdater := &BinaryUpdater{
		OldVersion: u.Version,
		NewVersion: info.Version,
		TargetPath: u.TargetPath,
		TargetMode: u.TargetMode,
	}
	if info.Checksum != "" {
		checksum, err := parseChecksum(info.Checksum)
		if err != nil {
			logger.Error("invalid checksum format", "error", err)
			return err
		}
		binaryUpdater.Checksum = checksum
	}
	if err := binaryUpdater.Init(); err != nil {
		logger.Error("prepare binary updater failed", "error", err)
		return err
	}

	defer os.Remove(binaryUpdater.NewBinary)
	if err := u.ctx.Err(); err != nil {
		return err
	}

	var output string
	if u.Download != nil {
		output, err = u.Download(info.Package, binaryUpdater.NewBinary)
	} else {
		output, err = request.DownloadContext(u.ctx, info.Package, binaryUpdater.NewBinary, true, u.HTTPClient)
	}

	if err != nil {
		logger.Error("download package failed", "error", err)
		return err
	}
	if output != "" {
		binaryUpdater.NewBinary = output
	}

	if err := u.ctx.Err(); err != nil {
		return err
	}
	if err = binaryUpdater.VerifyChecksum(); err != nil {
		logger.Error("verify checksum failed", "error", err)
		return err
	}

	if err := u.ctx.Err(); err != nil {
		return err
	}
	if err = binaryUpdater.CommitBinary(); err != nil {
		logger.Error("apply binary failed", "error", err)
		if _, ok := err.(*ErrRollback); ok {
			logger.Error("failed to rollback from bad update")
		}
		return err
	}

	return nil
}

// Restart restarts the current process.
func (u *Updater) Restart() error {
	if err := u.ctx.Err(); err != nil {
		return err
	}

	self := u.TargetPath
	if self == "" {
		var err error
		self, err = os.Executable()
		if err != nil {
			return err
		}
	}

	args, env := os.Args, os.Environ()

	if runtime.GOOS == "windows" {
		cmd := exec.Command(self, args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Env = env
		if err := cmd.Start(); err != nil {
			return err
		}
		os.Exit(0)
	}

	return syscall.Exec(self, args, env)
}

func (u *Updater) buildUpdateURL() (string, error) {
	if u.Server == "" {
		return "", errors.New("server url is required")
	}

	parsed, err := url.Parse(u.Server)
	if err != nil {
		return "", err
	}

	q := parsed.Query()
	q.Set("ver", u.Version)
	q.Set("os", runtime.GOOS)
	q.Set("arch", runtime.GOARCH)
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}

package webssh

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/rehiy/libgo/websocket"
)

const DefaultResizeControlPrefix = "\x00webssh:resize:"

type ConnectOptions struct {
	ResizeControlPrefix string
	InitialWidth        int
	InitialHeight       int
}

func Connect(ws *websocket.Conn, opt *SSHClientOption) error {
	return ConnectWithOptions(ws, opt, ConnectOptions{})
}

func ConnectWithResize(ws *websocket.Conn, opt *SSHClientOption) error {
	return ConnectWithOptions(ws, opt, ConnectOptions{
		ResizeControlPrefix: DefaultResizeControlPrefix,
	})
}

func ConnectWithOptions(ws *websocket.Conn, opt *SSHClientOption, connectOpt ConnectOptions) error {
	defer ws.Close()

	// 创建客户端

	client, err := NewSSHClient(opt)

	if err != nil {
		ws.Write([]byte("> " + err.Error() + "\r\n"))
		return err
	}

	defer client.Close()

	// 打开新会话

	session, err := client.NewSession()

	if err != nil {
		ws.Write([]byte(err.Error() + "\r\n"))
		return err
	}

	defer session.Close()

	// 绑定输入输出

	var resizeReader *terminalResizeReader
	if connectOpt.ResizeControlPrefix != "" {
		resizeReader = newTerminalResizeReader(ws, session, connectOpt.ResizeControlPrefix)
		session.Stdin = resizeReader
	} else {
		session.Stdin = ws
	}
	session.Stdout = ws
	session.Stderr = ws

	// 创建模拟终端

	fd := int(os.Stdin.Fd())
	width, height, _ := term.GetSize(fd)
	if connectOpt.InitialWidth > 0 {
		width = connectOpt.InitialWidth
	}
	if connectOpt.InitialHeight > 0 {
		height = connectOpt.InitialHeight
	}
	if width <= 0 && connectOpt.ResizeControlPrefix != "" {
		width = 120
	}
	if height <= 0 && connectOpt.ResizeControlPrefix != "" {
		height = 40
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm", height, width, modes); err != nil {
		ws.Write([]byte(err.Error() + "\r\n"))
		return err
	}
	if resizeReader != nil {
		resizeReader.Enable()
	}

	if err := session.Shell(); err != nil {
		ws.Write([]byte(err.Error() + "\r\n"))
		return err
	}

	if err := session.Wait(); err != nil {
		ws.Write([]byte(err.Error() + "\r\n"))
		return err
	}

	return nil
}

type terminalResizeReader struct {
	reader  *websocket.Conn
	session *ssh.Session
	prefix  string
	mu      sync.Mutex
	pending []byte
	cols    int
	rows    int
	ready   bool
}

func newTerminalResizeReader(reader *websocket.Conn, session *ssh.Session, prefix string) *terminalResizeReader {
	return &terminalResizeReader{reader: reader, session: session, prefix: prefix}
}

func (r *terminalResizeReader) Read(p []byte) (int, error) {
	for {
		if len(r.pending) > 0 {
			n := copy(p, r.pending)
			r.pending = r.pending[n:]
			return n, nil
		}

		buf := make([]byte, 64*1024)
		n, err := r.reader.Read(buf)
		if n > 0 {
			data := buf[:n]
			if cols, rows, ok := parseTerminalResize(data, r.prefix); ok {
				r.Resize(cols, rows)
				continue
			}
			copied := copy(p, data)
			if copied < len(data) {
				r.pending = append(r.pending[:0], data[copied:]...)
			}
			return copied, nil
		}
		return n, err
	}
}

func (r *terminalResizeReader) Enable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ready = true
	if r.cols > 0 && r.rows > 0 {
		_ = r.session.WindowChange(r.rows, r.cols)
	}
}

func (r *terminalResizeReader) Resize(cols, rows int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cols = cols
	r.rows = rows
	if !r.ready {
		return
	}
	_ = r.session.WindowChange(rows, cols)
}

func parseTerminalResize(data []byte, prefix string) (int, int, bool) {
	msg := string(data)
	if prefix == "" || !strings.HasPrefix(msg, prefix) {
		return 0, 0, false
	}
	size := strings.TrimPrefix(msg, prefix)
	colsText, rowsText, ok := strings.Cut(size, ":")
	if !ok {
		return 0, 0, true
	}
	cols, err := strconv.Atoi(colsText)
	if err != nil {
		return 0, 0, true
	}
	rows, err := strconv.Atoi(rowsText)
	if err != nil {
		return 0, 0, true
	}
	if cols < 10 || rows < 3 || cols > 1000 || rows > 1000 {
		return 0, 0, true
	}
	return cols, rows, true
}

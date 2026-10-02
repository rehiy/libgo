# Libgo - Go 辅助类库

一个功能丰富、模块化的 Go 语言辅助类库，封装了常用功能和第三方服务接口，帮助开发者快速构建应用程序。

## 特性

- 🚀 **开箱即用** - 简洁直观的 API 设计
- 🧩 **模块化** - 按需引入，无冗余依赖
- 📦 **功能丰富** - 涵盖数据库、日志、加密、系统监控、网络通信等
- ☁️ **云服务集成** - 支持阿里云、腾讯云、Cloudflare DNS API
- 📝 **代码完善** - 详细注释和类型定义

## 安装

```shell
go get -u github.com/rehiy/libgo
```

## 文档

详细 API 文档：[pkg.go.dev](https://pkg.go.dev/github.com/rehiy/libgo)

升级指南：[UPGRADE.md](./UPGRADE.md)

---

## 模块概览

### 云服务集成

| 模块 | 说明 |
|------|------|
| `cloud/alibaba` | 阿里云 API 客户端，支持 RPC 风格接口调用 |
| `cloud/tencent` | 腾讯云 API 客户端集成 |
| `cloud/cloudflare` | Cloudflare API 集成 |

### 数据库操作

| 模块 | 说明 |
|------|------|
| `dborm` | GORM 数据库封装，支持 MySQL、PostgreSQL、SQLite |

### 安全与加密

| 模块 | 说明 |
|------|------|
| `secure` | 加密工具集：Bcrypt 密码哈希、DES3-CBC 加密、MD5 哈希、SSH 密钥生成 |
| `certify` | SSL 证书自动管理（支持 DNS-01/HTTP-01 验证，通配符域名）及 PEM 证书解析 |

### 网络与通信

| 模块 | 说明 |
|------|------|
| `request` | HTTP 客户端，支持 JSON/表单请求、文件下载、进度显示 |
| `httpd` | HTTP 服务器（基于 Gin），含 Recovery 中间件、静态文件服务 |
| `websocket` | WebSocket 客户端/服务器封装，支持 Origin 验证和 ping 保活 |
| `relay` | 通用双向流转发，支持 WebSocket 到 TCP 转发 |
| `webssh` | SSH 客户端，支持密码和私钥认证 |

### 系统监控

| 模块 | 说明 |
|------|------|
| `psutil` | 系统信息采集：CPU、内存、硬盘、网络、主机信息、公网 IP |
| `gpu` | GPU 信息采集，支持 NVIDIA、AMD、Intel、Apple GPU |
| `command` | 系统命令执行工具，支持 Shell/BAT/PowerShell/EXEC 模式 |

### 日志与文件

| 模块 | 说明 |
|------|------|
| `logman` | 结构化日志管理（基于 slog），支持日志轮转、多级别输出 |
| `filer` | 文件操作：读写、列表、权限管理、软链接、嵌入文件支持 |
| `archive` | 归档工具：ZIP 压缩/解压，防 Zip Slip 攻击 |

### 工具函数

| 模块 | 说明 |
|------|------|
| `strutil` | 字符串处理：随机生成、UUID v7、编码转换、大小写转换 |
| `ttlcache` | 带过期时间的内存缓存，自动清理 |
| `signal` | 程序退出信号处理（SIGTERM/SIGINT） |
| `upgrade` | 应用自更新服务，支持校验、备份、回滚 |

---

## 使用示例

### 数据库连接

```go
import "github.com/rehiy/libgo/dborm"

db := dborm.Connect(&dborm.Config{
    Type:     "mysql",      // 支持 mysql、pgsql、sqlite
    Host:     "localhost:3306",
    User:     "root",
    Password: "password",
    DbName:   "mydb",
})

// 使用 GORM 操作
db.Create(&User{Name: "test"})
```

### HTTP 请求

```go
import "github.com/rehiy/libgo/request"

// 使用 Client 结构体
client := &request.Client{
    Method:  "POST",
    Url:     "https://api.example.com/data",
    Data:    `{"key": "value"}`,
    Headers: map[string]string{"Authorization": "Bearer token"},
    Timeout: 10 * time.Second,
}

// JSON 请求（自动设置 Content-Type）
body, err := client.JsonRequest()

// 表单请求
text, err := client.TextRequest()

// 快捷方法
body, err := request.Get("https://api.example.com/data", request.H{})
body, err := request.JsonPost("https://api.example.com/data", map[string]string{"key": "value"}, request.H{})
body, err := request.JsonPut("https://api.example.com/data", data, request.H{})
body, err := request.JsonPatch("https://api.example.com/data", data, request.H{})
body, err := request.Delete("https://api.example.com/data", request.H{})
```

### 文件下载

```go
import "github.com/rehiy/libgo/request"

// 下载文件（显示进度条）
filepath, err := request.Download("https://example.com/file.zip", "/tmp/file.zip", false)

// 下载并自动解压 gzip
filepath, err := request.Download("https://example.com/file.tar.gz", "/tmp/file.tar", true)

// 下载到临时文件
filepath, err := request.Download("https://example.com/file", "", false)
```

### 日志记录

```go
import "github.com/rehiy/libgo/logman"

// 创建命名日志器
logger := logman.Named("myapp")
logger.Info("Application started")
logger.Error("Operation failed", "error", err, "code", 500)

// 格式化输出
logger.Infof("User %s logged in", username)

// 设置输出文件（自动轮转）
logman.SetOutput("/var/log/myapp.log")

// 设置日志级别
logman.SetLevel(logman.LevelDebug)
```

### 系统监控

```go
import "github.com/rehiy/libgo/psutil"

// 系统概要信息
stat := psutil.Summary(true)  // true 表示获取公网 IP
fmt.Printf("主机: %s\nCPU: %.2f%%\n内存: %d/%d MB\n",
    stat.HostName,
    stat.CpuPercent[0],
    stat.MemoryUsed/1024/1024,
    stat.MemoryTotal/1024/1024)

// 系统详细信息（网络、磁盘分区等）
detail := psutil.Detail(true)
for _, iface := range detail.NetInterface {
    fmt.Printf("网卡 %s: 接收 %d 字节, 发送 %d 字节\n",
        iface.Name, iface.BytesRecv, iface.BytesSent)
}

// Go 运行时内存统计
memStat := psutil.GoMemory()
fmt.Printf("堆内存: %d MB, GC次数: %d\n",
    memStat.HeapInuse/1024/1024, memStat.NumGC)
```

### GPU 监控

```go
import "github.com/rehiy/libgo/gpu"

stats, err := gpu.GetGPUStats(ctx)
for _, stat := range stats {
    fmt.Printf("%s (%s): 利用率 %.1f%%, 显存 %d/%d MB, 温度 %d°C\n",
        stat.Name, stat.Vendor,
        stat.Utilization,
        stat.MemoryUsed/1024/1024, stat.MemoryTotal/1024/1024,
        stat.Temperature)
}
```

### 命令执行

```go
import "github.com/rehiy/libgo/command"

// 执行 Shell 脚本
output, err := command.RunScript(&command.ScriptPayload{
    ScriptType: "SHELL",
    Content:    "ls -la",
    WorkDir:    "/home",
    Timeout:    10,
})

// 执行 PowerShell（Windows）
output, err := command.RunScript(&command.ScriptPayload{
    ScriptType: "POWERSHELL",
    Content:    "Get-Process",
    Timeout:    30,
})

// 直接执行命令
output, err := command.RunCommand("ls", []string{"-la"}, "/home", 10)

// 获取系统默认 Shell
shell := command.DefaultShell()
```

### SSH 连接

```go
import "github.com/rehiy/libgo/webssh"

// 密码认证
client, err := webssh.NewSSHClient(&webssh.SSHClientOption{
    Addr:     "example.com:22",
    User:     "username",
    Password: "password",
})

// 私钥认证
client, err := webssh.NewSSHClient(&webssh.SSHClientOption{
    Addr:       "example.com:22",
    User:       "username",
    PrivateKey: "-----BEGIN RSA PRIVATE KEY-----...",
})
```

### WebSocket 客户端

```go
import "github.com/rehiy/libgo/websocket"

conn, err := websocket.NewClient("ws://example.com/ws", "", "http://example.com")
if err != nil {
    panic(err)
}

// 写入数据
conn.Write([]byte("hello"))

// 读取数据
buf := make([]byte, 1024)
n, err := conn.Read(buf)

// JSON 消息
conn.WriteJSON(map[string]string{"type": "ping"})
var msg Message
conn.ReadJSON(&msg)

conn.Close()
```

### WebSocket 服务器

```go
import "github.com/rehiy/libgo/websocket"

// 配置服务端
config := &websocket.ServerConfig{
    AllowedOrigins: []string{"https://example.com", "*.example.com"},
    ReadLimit:      1 << 20,
}

// 创建处理器
httpd.GET("/ws", config.Handler(func(conn *websocket.ServerConn) {
    defer conn.Close()
    
    for {
        buf := make([]byte, 1024)
        n, err := conn.Read(buf)
        if err != nil {
            break
        }
        conn.Write(buf[:n])
    }
}))
```

### 通用流转发

```go
import "github.com/rehiy/libgo/relay"

// 在两个 io.ReadWriter 之间双向转发
err := relay.Bridge(relay.NewReadWriter(left), relay.NewReadWriter(right))
```

### TCP 转发

```go
import "github.com/rehiy/libgo/relay"

// WebSocket 到 TCP 转发
config := &websocket.ServerConfig{
    AllowedOrigins: []string{"https://example.com"},
    ReadLimit:      1 << 20,
}

httpd.GET("/tcp", config.Handler(func(ws *websocket.ServerConn) {
    relay.TCPRelay(ws.Conn, &relay.TCPParam{
        TargetAddr: "localhost:22",
        BinaryMode: false,
    })
}))
```

### HTTP 服务器

```go
import "github.com/rehiy/libgo/httpd"

// 创建路由引擎
httpd.Engine(false)  // false 为非调试模式

// 添加路由
httpd.GET("/api/hello", func(c *gin.Context) {
    c.JSON(200, gin.H{"message": "hello"})
})

// 启动服务器（自动注册退出信号处理）
httpd.Server(":8080",
    httpd.WithReadTimeout(30*time.Second),
    httpd.WithWriteTimeout(30*time.Second),
)
```

### 退出信号处理

```go
import "github.com/rehiy/libgo/signal"

// 注册退出回调（支持多个）
signal.OnQuit(func() {
    fmt.Println("清理资源...")
    db.Close()
})

signal.OnQuit(func() {
    fmt.Println("保存状态...")
})
```

### SSL 证书管理

```go
import "github.com/rehiy/libgo/certify"

// 创建证书管理器
manager := certify.New("admin@example.com", certify.DirCache("/var/lib/certs"))

// 指定 ACME 目录（如使用 Buypass）
manager = certify.NewWithDirectory("admin@example.com", 
    certify.DirCache("/var/lib/certs"),
    certify.Buypass)

// 获取证书（自动申请/续期）
cert, err := manager.GetCertificateForDomains([]string{"example.com", "*.example.com"})

// TLS 配置（用于 HTTP 服务器）
tlsConfig := manager.TLSConfig()

// HTTP-01 验证处理器
handler := manager.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("ACME verification"))
}))
```

### PEM 证书解析

```go
import "github.com/rehiy/libgo/certify"

cert := certify.PEMParse(data) // data 为 PEM 格式的 []byte
if cert != nil {
    // 使用 cert.Subject、cert.DNSNames、cert.NotAfter 等证书信息
}
```

`PEMParse` 跳过私钥等其他 PEM block，解析第一个 `CERTIFICATE` block；内容缺失或该证书无效时返回 `nil`。

### 文件操作

```go
import "github.com/rehiy/libgo/filer"

// 列出目录文件（含权限和所有者信息）
files, err := filer.List("/path/to/dir")
for _, f := range files {
    fmt.Printf("%s: %d bytes, owner: %s, group: %s, mode: %s\n",
        f.Name, f.Size, f.Owner, f.Group, f.Mode)
}

// 获取文件信息（含内容）
info, err := filer.Info("/path/to/file", true)
fmt.Printf("内容: %s\n", string(info.Data))

// 写入文件（自动创建目录）
filer.Write("/path/to/file.txt", []byte("content"))

// 追加内容
filer.Append("/path/to/log.txt", []byte("new line\n"))

// 读取文件内容
content, err := filer.Read("/path/to/file.txt")
text, err := filer.ReadText("/path/to/file.txt")

// 判断文件状态
filer.Exist("/path/to/file")      // 是否存在
filer.NotExist("/path/to/file")   // 是否不存在
filer.IsDir("/path/to/dir")       // 是否目录
filer.IsLink("/path/to/link")     // 是否软链接
filer.Readlink("/path/to/link")   // 获取软链接目标
```

### ZIP 归档

```go
import "github.com/rehiy/libgo/archive"

zipper := archive.NewZipper()

// 压缩文件
err := zipper.Zip("/path/to/file")  // 生成 file.zip

// 压缩目录
err := zipper.Zip("/path/to/dir")   // 生成 dir.zip

// 解压文件
err := zipper.Unzip("/path/to/archive.zip")
```

### 密码哈希

```go
import "github.com/rehiy/libgo/secure"

// 生成密码哈希
hash, err := secure.BcryptHash("password123")

// 验证密码
valid := secure.BcryptVerify("password123", hash)

// 检查是否为 bcrypt 格式
isBcrypt := secure.IsBcrypt(hash)
```

### DES3 加密

```go
import "github.com/rehiy/libgo/secure"

// DES3-CBC 加密
encrypted, err := secure.Des3Encrypt("secret data", "password")

// DES3-CBC 解密
decrypted, err := secure.Des3Decrypt(encrypted, "password")
```

### MD5 哈希

```go
import "github.com/rehiy/libgo/secure"

// 计算字符串 MD5
hash := secure.MD5sum("content")

// 计算文件 MD5
hash, err := secure.FileMD5sum("/path/to/file")
```

### SSH 密钥生成

```go
import "github.com/rehiy/libgo/secure"

// 生成 RSA 4096 位密钥对
privateKey, publicKey, err := secure.NewSSHKeypair()

// 保存到文件
filer.Write("/path/to/id_rsa", []byte(privateKey))
filer.Write("/path/to/id_rsa.pub", []byte(publicKey))
```

### 随机字符串

```go
import "github.com/rehiy/libgo/strutil"

// 生成随机字符串（字母数字混合）
randStr := strutil.Rand(16)  // 如 "aB3xY9zK2mN7pQ1"

// 生成 UUID v7（时间排序）
uuid := strutil.NewString()
```

### 字符串转换

```go
import "github.com/rehiy/libgo/strutil"

// 字符串转整数
num := strutil.ToInt("123")   // 123

// 字符串转无符号整数
num := strutil.ToUint("123")  // 123

// 首字母大写
str := strutil.FirstUpper("hello")  // "Hello"

// 首字母小写
str := strutil.FirstLower("Hello")  // "hello"

// 编码转换 GB18030 -> UTF-8
utf8Str := strutil.Gb18030ToUtf8(gbStr)
```

### TTL 缓存

```go
import "github.com/rehiy/libgo/ttlcache"

// 创建缓存（过期时间 5 分钟）
cache := ttlcache.NewTimedCache(5 * time.Minute)

// 设置缓存值
cache.Set("key", "value")

// 获取缓存值（过期返回 nil, false）
value, ok := cache.Get("key")

// 手动清理过期项
cache.DeleteExpired()

// 停止自动清理
cache.StopCleanup()
```

### 应用自更新

```go
import "github.com/rehiy/libgo/upgrade"

// 检查更新
info, err := upgrade.CheckUpdate(&upgrade.UpdateParam{
    Server:  "https://update.example.com/check",
    Version: "1.0.0",
})

// 执行更新
err := upgrade.ApplyUpdate(&upgrade.UpdateParam{
    Server:  "https://update.example.com/check",
    Version: "1.0.0",
})

// 重启应用
err := upgrade.Restart()
```

### 云服务 API 调用

```go
import "github.com/rehiy/libgo/cloud/alibaba"

// 阿里云 API 请求
result, err := alibaba.Request(&alibaba.RequestParam{
    Service:   "ecs",           // 产品名称
    Version:   "2014-05-26",    // API 版本
    Action:    "DescribeInstances",
    RegionId:  "cn-hangzhou",
    SecretId:  "AccessKeyId",
    SecretKey: "AccessKeySecret",
    Query:     map[string]string{"PageNumber": "1"},
})
```

```go
import "github.com/rehiy/libgo/cloud/tencent"

// 腾讯云 API 请求
result, err := tencent.Request(&tencent.RequestParam{
    Service:   "cvm",           // 产品名称
    Version:   "2017-03-12",    // API 版本
    Action:    "DescribeInstances",
    Region:    "ap-guangzhou",
    SecretId:  "SecretId",
    SecretKey: "SecretKey",
    Payload:   map[string]int{"Offset": 0, "Limit": 10},
})
```

---

## etcd 精确读取

`Client.Get(ctx, key)` 保持原有行为：缺失和空字符串都返回空字符串。需要判断 key 是否存在时，使用 `Lookup`：

```go
value, exists, err := client.Lookup(ctx, key)
// err != nil：读取失败；exists == false：key 不存在；exists == true：value 可以为空。
```

`CompareAndPut(ctx, key, expected, value)` 用 etcd 事务条件写入：`expected == nil` 表示仅在 key 缺失时创建；非 nil 表示要求 key 存在且值相同，空切片表示已存在的空值。返回 `false, nil` 表示冲突。比较基于值，不检测值变更后又恢复的 ABA 情况。

### etcd 状态监听

`Watch(ctx, key)` 统一处理自动重连与最新状态同步：首次监听建立及重连后读取最新值，读取失败每秒重试，连续相同状态去重。返回事件和错误两个 channel；取消 context 后关闭。连接失败采用 1 秒至 30 秒的指数退避，认证过期立即重试一次。错误非阻塞发送，消费者较慢时可能丢弃错误通知。

```go
events, errs := client.Watch(ctx, key)
// events 仅含 PUT/DELETE；PUT 的 Value 可以为空，DELETE 表示 key 缺失。
// 调用方处理 events 和 errs，并通过 ctx 取消监听。
```

状态读取使用客户端超时，且受调用方 context 约束。该接口用于追踪最新状态，不保证逐条重放历史变更。

**兼容性变更**：`Watch` 不再发送 `SYNC`，首次建立和重连后由客户端直接读取并发送最新 PUT/DELETE 状态；相同状态不重复发送。调用方无需再收到提示后自行读取。原始事件中的中间状态可能被合并。

## 依赖项

| 库 | 用途 |
|----|------|
| [Gin](https://github.com/gin-gonic/gin) | Web 框架 |
| [GORM](https://gorm.io/) | ORM 库 |
| [gopsutil](https://github.com/shirou/gopsutil) | 系统监控 |
| [ghw](https://github.com/jaypipes/ghw) | GPU 硬件信息 |
| [Lumberjack](https://github.com/natefinch/lumberjack) | 日志轮转 |
| [libdns](https://github.com/libdns/libdns) | DNS Provider 抽象 |
| [openssl](https://github.com/go-think/openssl) | DES3 加密 |

---

## 版本要求

- Go 1.26+

---

## 许可证

Copyright (c) 2022 - 2026 OpenTDP

## POSIX shell 参数转义

`command.POSIXQuote(value)` 将字符串转义为一个 POSIX shell 参数，适用于 sh/bash/zsh，包括空字符串和包含单引号的参数；不适用于 cmd 或 PowerShell。

```go
script := "cat " + command.POSIXQuote(filePath)
```

## TOTP

`secure` 提供 `TOTPSecretGenerate()`、`TOTPCode(secret, counter)`、`TOTPValidate(secret, code)` 和 `TOTPURI(issuer, username, secret)`。采用 SHA1、6 位验证码、30 秒周期，校验允许前后一周期偏移。密钥使用无填充的 Base32 编码；`TOTPCode` 的 counter 为时间步，解析失败返回空字符串。`TOTPValidate` 会裁剪输入空白并将密钥转为大写。

```go
secret, err := secure.TOTPSecretGenerate()
if err != nil { return err }
uri := secure.TOTPURI("MyApp", username, secret)
valid := secure.TOTPValidate(secret, code)
```

## AES-GCM 与 AEAD

`secure.NewAESGCM(key)` 接收 16、24 或 32 字节的 AES 密钥。`AEADSeal(aead, plaintext, additionalData)` 随机生成 nonce，返回 `nonce || ciphertext || tag`；`AEADOpen(aead, payload, additionalData)` 校验并解密，截断或被篡改的数据返回错误。附加认证数据不写入结果，加解密时必须一致。调用方负责密钥管理和传入有效的 AEAD 实例。

```go
aead, err := secure.NewAESGCM(key)
if err != nil { return err }
payload, err := secure.AEADSeal(aead, plaintext, nil)
if err != nil { return err }
plaintext, err = secure.AEADOpen(aead, payload, nil)
```

# golinks

[English](README.md)

单机版 **go links** 服务（macOS）—— 把大公司内部的 `go/docs`、`go/gh`
玩法，复刻给一个人、一台 Mac 用。

在浏览器地址栏输 `go/gh`，直接跳转到 `https://github.com`；输 `go/`
打开管理页，自助增删改映射。无云端、无账号、无数据库服务：一个静态
Go 二进制 + 一份 JSON 文件 + 一个 launchd 常驻守护。

## 原理

```
go/gh
  → /etc/hosts 把 "go" 解析到 127.0.0.1
  → golinks 守护进程（只听回环地址，80/443）查表 "gh"
  → HTTP 302 跳到映射的网址
```

- **hosts 只负责命名，跳转是守护进程的事**：hosts 只能把名字指到 IP，
  `/gh` 对应哪个网址必须由服务查表回 302。
- **只用 302、绝不用 301**：301 会被浏览器硬缓存，改了映射也不生效。
- **只做精确匹配，没有后缀透传**：`go/gh/repo` 不会把 `repo` 拼到目标
  后面；未命中的路径会打开管理页并把 key 预填好，方便顺手创建。
- **只绑回环地址**（`127.0.0.1` / `::1`，永不绑 `0.0.0.0`）：所以管理页
  不需要登录 —— 局域网里其他设备根本连不进来。

## 监听模式（80/443 可选配置）

| 模式    | 80 | 443 | mkcert | 行为 |
|---------|----|-----|--------|------|
| `http`  | 开 | 关  | 不装   | 纯 HTTP，最简形态 |
| `both`  | 开 | 开  | 安装   | 80 只负责跳到 443，想用 HTTPS 选这个 |
| `https` | 关 | 开  | 安装   | 仅 HTTPS |

HTTPS 走 [mkcert](https://github.com/FiloSottile/mkcert) 本地 CA：根证书
只装进你自己的钥匙串，它为 `go` 签的证书在你的浏览器里无警告受信任。
不开 HTTPS 就完全不碰 mkcert。

非法配置在启动时直接报错退出（两个口都关、开了 HTTPS 但证书缺失等），
不静默降级。

## 安装（macOS）

```sh
make dist            # 或把发布二进制放进 dist/
bash scripts/install.sh   # 交互选择模式
bash scripts/install.sh --mode both   # 非交互
```

安装脚本依次做：装二进制到 `/usr/local/bin/golinks` →（HTTPS 模式才）
装 mkcert、建 CA、签 `go` 的证书 → 写
`/usr/local/etc/golinks/config.json` → 往 `/etc/hosts` 加
`127.0.0.1 go` 与 `::1 go`（已有则跳过）→ 注册 launchd 守护
`local.golinks`（开机自启、崩溃拉起）→ 健康检查
`http(s)://go/_admin/health`。

装完打开 `go/`（或 `https://go/`）加第一条链接即可。

### Clash / TUN 代理注意

开 TUN 模式的代理（Clash Verge Rev / Mihomo 等）时，它的 fake-ip DNS
可能抢在系统解析器读 hosts 之前把 `go` 劫走 —— 症状是 `go` 解析出
`198.18.x.x`、页面打不开。在代理的扩展（Merge）配置里加：

```yaml
dns:
  hosts:
    go: 127.0.0.1
  fake-ip-filter:
    - go
```

## 配置

`/usr/local/etc/golinks/config.json`，示例见 `config.example.json`：
`http_enabled`、`https_enabled`、`http_port`（80）、`https_port`（443）、
`cert_file`、`key_file`（仅 HTTPS）、`data_file`（映射存储，JSON 原子写）。

改模式 = 改这份配置（首次开 HTTPS 需补跑 mkcert 步骤）后重启守护：
`sudo launchctl kickstart -k system/local.golinks`，映射数据不受影响。

## 卸载

```sh
bash scripts/uninstall.sh                    # 保留配置与链接数据
bash scripts/uninstall.sh --purge            # 连配置、数据、hosts 一起删
bash scripts/uninstall.sh --purge --remove-ca  # 再卸掉 mkcert 本地 CA
```

## 开发

```sh
make test   # 单元测试
make vet
make dist   # 交叉编译 darwin arm64/amd64
```

仅用 Go 标准库，零依赖。

## 安全说明

- 守护进程以 root 运行（要绑 80/443），路由与存储逻辑都很短，装之前
  建议先扫一眼 `handlers.go`。
- mkcert 根 CA 私钥只存本机，它就是一把万能钥匙：绝不要同步到云盘或
  提交进任何仓库。本仓库 `.gitignore` 默认排除所有 `*.pem`。
- 不要使用任何泄露的公共 CA 私钥签证书 —— 那是冒用公共信任体系，
  且 2026 年 eMudhra 事件的涉事密钥已被吊销、被各大浏览器封禁。
  给 `go` 这类本地名字上 HTTPS，正道就是自建本地 CA。

## 许可证

MIT

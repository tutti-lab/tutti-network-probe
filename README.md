# Tutti Network Probe

一个独立于 Tutti 运行的轻量网络旁路探针。它每 10 秒并发请求 Google、OpenAI、Anthropic 和 E2B，用一条独立的时间线帮助判断 Tutti 故障发生时，整机的代理、出口或 TLS 是否也在异常。

## 一键安装并启动（Apple Silicon macOS）

```bash
curl -fsSL --retry 5 --retry-all-errors --retry-delay 1 https://tsh-runtime-artifacts.s3.amazonaws.com/tutti-network-probe/install.sh | bash
```

安装程序只支持 Apple Silicon Mac。它会下载并校验 SHA-256，安装到 `~/.local/bin/tutti-network-probe`，并注册、启动用户级 LaunchAgent `sh.tutti.network-probe`。状态和日志：

```bash
launchctl print gui/$(id -u)/sh.tutti.network-probe
tail -f ~/Library/Logs/Tutti/network-probe.jsonl
```

卸载程序但保留诊断日志：

```bash
curl -fsSL --retry 5 --retry-all-errors --retry-delay 1 https://tsh-runtime-artifacts.s3.amazonaws.com/tutti-network-probe/uninstall.sh | sh
```

## 观测范围

默认目标：

- `https://www.google.com/generate_204`
- `https://api.openai.com/v1/models`
- `https://api.anthropic.com/v1/models`
- `https://e2b.app`

HTTP 204、3xx、401、403 或 5xx 都表示 curl 已经完成 DNS、TCP、TLS 和 HTTP 交换，因此视为传输成功。只有 curl 自身无法完成传输时才标记为失败。

E2B 使用固定公共域名，不依赖 sandbox 处于 running 状态。它可以作为 E2B 域名的基础网络对照，但不能证明某个动态 `8082-<sandbox>.e2b.app` endpoint 当时可用。

每个目标都由独立 curl 进程请求，不复用连接，所以每轮都会重新经历代理/TCP/TLS 建连。

## 诊断日志

默认日志：

```text
~/Library/Logs/Tutti/network-probe.jsonl
```

日志不是只记失败。每轮四个结果会组成一条 `probe_cycle` JSONL 事件，另外还有 `session_start` 和 `session_stop`。这样可以证明目标时间段内探针是否持续运行，并观察异常前后的 TLS 基线。

每个 cycle 包含：

- schema、探针版本、session ID 和连续 sequence；
- UTC 纳秒级开始/完成时间与整轮耗时；
- 当轮实际选择的代理来源和已去除凭据的代理地址；
- 无论最终走哪条路径，都记录 `system_proxy_known`、`system_proxy_enabled`、类型、host 和 port；
- 四个目标各自的开始/完成时间和成功/失败状态；
- curl exit code、原始错误和失败分类；
- HTTP、代理 CONNECT、HTTP 协议版本和证书验证结果；
- 本地/远端 IP、端口和连接数；
- DNS、TCP connect、TLS ready、首字节和总耗时。

`local_port` 可以直接与 Clash 日志中的 source port 对齐。典型的 TLS/代理上游超时表现为 `connect_seconds > 0`、`tls_seconds = 0`，并被归类为 `tls_timeout`。

启动事件还记录二进制版本、curl 版本、OS/架构、hostname、PID、探测间隔、超时和目标配置。目标 URL 的凭据、query、fragment 以及代理凭据不会落盘。日志权限固定为 `0600`。

日志达到 25 MiB 时滚动，默认保留 7 份备份，最多约 200 MiB：

```text
network-probe.jsonl
network-probe.jsonl.1
...
network-probe.jsonl.7
```

## TLS 耗时

前台运行时每个成功请求会打印：

```text
2026-09-10T13:30:00.123+08:00 e2b ok: http=307 tls_ready=0.260s tls_phase=0.257s total=0.261s proxy=system:https system_proxy=https:127.0.0.1:7897
```

- `tls_ready` 是 curl 的 `time_appconnect`：从请求开始到 TLS 握手完成的累计时间，与此前排障使用的指标一致。
- `tls_phase` 是 `time_appconnect - time_connect`。直连时可近似理解为 TLS 握手耗时；经过 HTTP proxy 时还包含代理 CONNECT 和代理到上游的建连时间，不能视为纯 TLS 协议耗时。
- `total` 是完整请求耗时。

后台 LaunchAgent 会丢弃这些人类可读的成功输出，权威证据是结构化 JSONL；错误输出保存在 `~/Library/Logs/Tutti/network-probe-service.log`。

## 代理路径

默认 `--proxy auto`，每轮重新解析，优先级如下：

1. `HTTPS_PROXY` / `https_proxy` / `ALL_PROXY` / `all_proxy`
2. macOS `scutil --proxy` 中启用的 HTTPS 或 SOCKS 系统代理
3. 不显式传代理，让流量走普通直连或 Clash TUN

也可以固定路径：

```bash
tutti-network-probe --proxy http://127.0.0.1:7897  # Clash HTTP proxy
tutti-network-probe --proxy direct                 # 仍可能被 TUN 接管
tutti-network-probe --proxy system                 # 必须存在 macOS 系统代理
```

PAC 无法直接交给 curl；探针会发出警告，此时应显式提供实际代理 URL。标记为 `direct-or-tun` 的路径只能说明没有显式应用层代理，无法仅从进程内判断流量是否被 TUN 接管。

## 本地开发

需要 Go 1.22+ 和 curl：

```bash
make test
make build
./bin/tutti-network-probe --once
```

常用参数：

```text
--interval 10s
--connect-timeout 8s
--timeout 15s
--log-file /path/to/network-probe.jsonl
--max-log-bytes 26214400
--max-log-backups 7
--target name=https://example.com/path
```

重复提供 `--target` 后会替换四个默认目标。

## 发布

发布脚本会测试代码、构建 Apple Silicon macOS 二进制、生成校验和并上传版本化产物与稳定安装入口：

```bash
make release VERSION=vX.Y.Z
```

默认发布到 `s3://tsh-runtime-artifacts/tutti-network-probe/`；版本目录不可覆盖。

# Pi Web Task 1–4 人工测试文档

版本：`v1.9.0-rc.1`

适用分支：`feat/pi-web-overall-improvement`

适用提交：Task 1–4，当前代码 HEAD 以交接文档为准。

## 1. 测试范围

本轮只验证已经完成的改动：

- Task 1：CI/Chrome 107 兼容性契约、lint/typecheck、发布二进制。
- Task 2：Broker 原子订阅、FIFO、gap、断开清理和事件 ID。
- Task 3：按 session 持久 Pi runtime、RPC response、Cancel、shutdown、race 修复。
- Task 4：session snapshot/cursor、SSE 断线恢复、AG-UI 去重和旧请求隔离。

本轮**不测试/不应假设已实现**：slash commands、Project Trust、PTY、Git worktree、session fork/search/export、Models/Skills/Plugins 控制面和 Playwright E2E。

## 2. 测试环境

优先环境：

| 项目 | 要求 |
|---|---|
| 服务端 | 麒麟 V10 SP3，amd64 或 arm64 |
| 浏览器 | Chrome 107；可额外使用当前 Chromium 做对照 |
| 网络 | 浏览器与服务端同机 localhost，或受控内网 |
| Pi | 真实 Pi CLI 测试需要目标机已安装并可执行 `pi`；无 Pi 时使用 `--mock` |
| 额外运行时 | 不要求 Node.js、npm、Bun、gcc 或额外 PTY 库 |

准备临时测试目录，避免污染真实 workspace：

```bash
mkdir -p "$HOME/pi-web-manual/workspace-a" "$HOME/pi-web-manual/workspace-b"
printf 'manual-test-a\n' > "$HOME/pi-web-manual/workspace-a/README.md"
printf 'manual-test-b\n' > "$HOME/pi-web-manual/workspace-b/README.md"
```

## 3. 获取并校验 RC

进入 RC 目录：

```bash
cd /home/hyperion/projects/pi-web-offline/.worktrees/pi-web-overall-improvement/dist/release-candidate-v1.9.0-rc.1
sha256sum -c SHA256SUMS
file pi-web-v1.9.0-rc.1-linux-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
```

预期：

- checksum 全部显示 `成功`；
- amd64 显示 `x86-64`，arm64 显示 `ARM aarch64`；
- 显示 `statically linked`；
- 不出现动态库缺失。

如果 `uname -m` 不是 `x86_64` 或 `aarch64`，停止并记录，不要强行运行错误架构。

## 4. 启动方式

### 4.1 Mock 模式（推荐第一轮）

```bash
./pi-web-v1.9.0-rc.1-linux-amd64 \
  --host 127.0.0.1 --port 8732 --mock
```

arm64 使用对应的 `linux-arm64` 文件。

### 4.2 真实 Pi 模式

```bash
command -v pi
pi --version
./pi-web-v1.9.0-rc.1-linux-amd64 \
  --host 127.0.0.1 --port 8732
```

当前 RC 必须显式传 `--host 127.0.0.1`。CLI 默认 host 仍是 `0.0.0.0`，因此这版不能直接作为远程部署版本。

服务启动后先执行：

```bash
curl -fsS http://127.0.0.1:8732/api/health
```

预期：HTTP 200，返回健康状态；终端无启动 panic。

## 5. 测试记录规则

每个用例记录：

- 结果：PASS / FAIL / BLOCKED；
- 环境：麒麟版本、架构、Chrome 版本、mock/real Pi；
- 时间和步骤；
- 截图或录屏；
- 浏览器 Console、Network、服务端日志；
- FAIL 时保留最小复现步骤，不要只写“有问题”。

## 6. 功能测试用例

### A. 启动与静态资源

#### M-001 首次打开

1. 启动服务。
2. 浏览器打开 `http://127.0.0.1:8732/`。
3. 等待页面完成加载。

预期：

- 页面能打开，无空白页、模块加载错误或 404；
- 中文界面正常显示；
- Chrome 107 Console 无 `SyntaxError`、`ResizeObserver`、`AbortSignal.timeout` 或 `Array.at` 兼容性错误；
- 页面刷新后仍能打开。

#### M-002 登录与 cookie

1. 首次进入时创建管理员密码。
2. 退出并重新登录。
3. 关闭浏览器后重新打开页面。

预期：

- 未登录访问受保护页面时回到登录流程；
- 登录后页面正常恢复；
- cookie 为 HttpOnly；
- 不需要 localStorage 保存管理员密码。

### B. Workspace 与 ACL

#### M-003 管理员打开 workspace

1. 以管理员登录。
2. 添加 `workspace-a` 和 `workspace-b`。
3. 分别进入两个 workspace，确认 README 内容不同。

预期：两个 workspace 可正确打开，文件树、文件读取和 session 列表与目录对应。

#### M-004 普通用户隔离

1. 管理员创建普通用户 `manual-user`。
2. 只给该用户分配 `workspace-a`。
3. 使用无痕窗口登录 `manual-user`。
4. 尝试访问 `workspace-b` 页面、session 和文件接口。

预期：

- 普通用户只能看到 `workspace-a`；
- `workspace-b` 不出现在列表；
- 直接改 URL 或请求 ID 不能绕过 ACL；
- 服务端返回 403 或 404，不能返回文件/session 内容。

### C. Prompt 与持久 runtime

#### M-005 Mock 流式 Prompt

1. 使用 `--mock` 启动。
2. 创建一个新 session。
3. 发送一条普通中文 Prompt，例如：`请回复三行测试文本`。
4. 观察消息逐步出现。

预期：

- 用户消息只显示一次；
- assistant 内容按流式状态更新，不出现重复卡片；
- 最终状态回到 idle；
- 刷新页面后消息仍存在。

#### M-006 同一 session 连续 Prompt

1. 在同一个 session 发送 Prompt A。
2. 等待完成后发送 Prompt B。
3. 观察服务端日志和响应。

预期：

- A、B 都显示且顺序正确；
- 第二次 Prompt 不需要重新启动独立 runtime；
- 不出现 A 的结束事件污染 B；
- 不出现重复 assistant response 或永久 running。

真实 Pi 模式下，可在服务端观察同一 session 的 Pi 子进程是否持续存在；不要以进程 PID 永远不变作为唯一判断，runtime idle eviction 允许空闲回收。

#### M-007 不同 session 隔离

1. 创建 session A、session B。
2. 在 A、B 中分别发送不同 Prompt。
3. 交错观察两边的流式输出。

预期：A 的消息、状态和错误不会出现在 B；并发操作不会串 session。

#### M-008 Cancel/Steer/Compact

在真实 Pi 模式、可执行且允许长时间运行的 workspace 中执行：

1. 发起一个可持续数秒的 Prompt。
2. 在运行中执行取消。
3. 取消完成前不要重复点击；确认状态稳定后再发送下一条 Prompt。
4. 如 UI 提供 compact，运行 compact 后再发送 Prompt。

预期：

- 取消不会让 session 永久 busy；
- 新 Prompt 不会与旧执行重叠；
- 旧事件不会混入新 Prompt；
- abort/EOF/error 时 UI 能回到可操作状态；
- 服务端退出时不留下 Pi 子进程。

### D. SSE、snapshot 与断线恢复

#### M-009 刷新恢复

1. 在 session 中发送 Prompt，等待出现用户消息和部分 assistant 流。
2. 在 assistant 仍在输出时刷新页面。
3. 重复 3 次。

预期：

- 页面先恢复 snapshot，再继续事件流；
- 已完成消息不重复；
- 不出现从中间截断且无法继续的状态；
- status 与实际运行状态最终一致。

#### M-010 浏览器离线恢复

1. 打开 Chrome DevTools → Network。
2. 在 Prompt 流式输出期间切换 Offline，持续 2–5 秒。
3. 恢复 Online。
4. 观察 session 内容和 Network 请求。

预期：

- 断线后会重新加载 session snapshot；
- 重新建立 SSE，不把 `broker_gap` 当作用户消息显示；
- 消息和工具卡片不重复、不丢失；
- 最新状态最终可见。

#### M-011 停止并重启服务

1. 在 mock 或真实 Pi 流式期间停止服务进程。
2. 确认浏览器连接断开。
3. 使用相同参数重新启动服务。
4. 刷新页面。

预期：

- 页面能重新加载已有 session snapshot；
- 不因旧 `Last-Event-ID` 导致历史消息永久缺失；
- 不出现多个重复流；
- 服务端没有残留 Pi runtime 或无法退出的 goroutine 表现。

#### M-012 AG-UI 去重

1. 使用真实 Pi 模式触发一次 AG-UI/工具调用流程；如果当前环境无可用工具，记录 BLOCKED，不要用普通 Prompt 替代。
2. 在工具或 assistant 流程结束后观察页面。
3. 触发一次刷新或短暂断网恢复。

预期：同一工具调用、assistant 消息和结束状态只出现一次；恢复不会把已消费事件再次渲染。

### E. 输入安全与错误路径

#### M-013 中文、特殊字符和长输出

1. 发送包含中文、emoji、换行、引号和 Markdown 的 Prompt。
2. 让 workspace 中存在名称含 `<img src=x onerror=alert(1)>` 的测试文件（如文件系统允许）。
3. 在文件树、session、错误提示和流式输出中观察。

预期：

- 文本按文字显示，不执行 HTML/脚本；
- 不弹出 alert；
- 文件名、错误、路径和命令描述不会破坏布局；
- Chrome 107 页面不出现明显兼容性样式崩坏。

#### M-014 非法/过期连接

1. 登录后打开一个 session 页面。
2. 退出登录或让 cookie 失效。
3. 刷新 session 页面并观察 Network。

预期：受保护请求被拒绝并回到登录/错误状态；不会显示其他用户数据。

## 7. Kylin V10 SP3 运行环境检查

在麒麟机器记录：

```bash
cat /etc/os-release
uname -a
uname -m
command -v sh
command -v pi || true
```

预期：

- 架构与下载的二进制一致；
- `/bin/sh` 可用；
- 不需要安装 Node.js、npm、Bun、gcc 或额外库；
- mock 模式在无 Pi 时也能启动并完成 M-001、M-005、M-009–M-011；
- 真实 Pi 模式若失败，记录 Pi CLI 版本、错误日志和是否为 provider/auth 问题，不将其误判为 Web runtime 问题。

## 8. 建议人工测试顺序

1. M-001、M-002、M-003、M-004；
2. M-005、M-006、M-007；
3. M-009、M-010、M-011；
4. M-013、M-014；
5. 有真实 Pi 后再执行 M-008、M-012；
6. 最后在麒麟 amd64/arm64 各跑一轮关键用例。

## 9. 缺陷报告模板

```text
用例：M-xxx
结果：PASS / FAIL / BLOCKED
环境：Kylin V10 SP3 / amd64|arm64 / Chrome xxx / mock|real Pi
时间：
复现步骤：
预期结果：
实际结果：
浏览器 Console：
Network 请求/响应：
服务端日志：
截图/录屏：
是否稳定复现：
```

## 10. 当前已知限制

- RC 必须显式 `--host 127.0.0.1`，Task 7 才会修复 CLI 默认 host。
- 当前 release candidate 在临时源码树中嵌入了最新 Astro 资源；仓库 `static/` 未修改、未提交。
- 前端 coverage gate 延后到 Task 12/13；本轮不能据人工测试宣称 coverage 完成。
- Task 5–13 尚未执行。

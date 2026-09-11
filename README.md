<h1 align="center">JuunAI pi-web</h1>

<div align="center">

基于 `pi` coding agent 的离线 Web 控制台，面向本地开发与隔离网络环境。

预发行版本：`v1.10.0-alpha.1`

![pi-web web UI](./docs/assets/pi-web.png)

| Desktop |
| --- |
| ![Workspace session UI](docs/assets/screenshot.png) |

| Tablet | Mobile |
| --- | --- |
| ![Tablet workspace UI](docs/assets/tablet.webp) | ![Mobile file tree UI](docs/assets/mobile.webp) |

</div>

> 本项目是 [Epsilondelta-ai/pi-web](https://github.com/Epsilondelta-ai/pi-web) 的 JuunAI Fork。
> Fork 保留上游同步能力，同时独立维护中文默认界面、浅色蓝灰视觉和离线发行版本。

## 特性

- **离线单二进制**：Astro/React 前端嵌入 Go 服务端，无需在目标机安装 Node、Bun 或 npm。
- **Pi 会话控制**：浏览会话、创建会话、实时接收响应，并支持刷新后的状态恢复。
- **工作区管理**：管理员可以登记本地目录或克隆仓库；普通用户只能访问被授权的工作区。
- **用户与权限隔离**：首次启动创建管理员，账号、授权、应用设置和会话归属保存于 SQLite。
- **文件操作**：浏览、预览、编辑和保存工作区文件，执行工作区内的本地命令。
- **中文优先界面**：默认简体中文，并保留现有多语言切换机制。
- **浏览器兼容基线**：前端构建目标为 Chrome 107。

## 安装

当前预发行版本请显式指定版本号：

```bash
curl -fsSL https://raw.githubusercontent.com/HyperionD/pi-web/main/scripts/install.sh \
  | PI_WEB_VERSION=v1.10.0-alpha.1 sh
```

安装脚本默认安装 `pi`。如果已经自行安装，或只想安装 Web 控制台：

```bash
curl -fsSL https://raw.githubusercontent.com/HyperionD/pi-web/main/scripts/install.sh \
  | PI_WEB_INSTALL_PI=never sh
```

脚本支持 Linux/macOS 的 `amd64` 和 `arm64`。可通过环境变量自定义版本、安装目录和仓库：

```bash
curl -fsSL https://raw.githubusercontent.com/HyperionD/pi-web/main/scripts/install.sh \
  | PI_WEB_VERSION=v1.10.0-alpha.1 PI_WEB_INSTALL_DIR="$HOME/.local/bin" sh
```

## 启动

本机使用时建议显式绑定回环地址：

```bash
pi-web --host 127.0.0.1 --port 8732
# 打开 http://127.0.0.1:8732
```

启动后首次访问会进入管理员密码设置页。也可以使用 mock 模式验证 Web 界面，不执行真实的 `pi` 命令：

```bash
pi-web --host 127.0.0.1 --port 8732 --mock
```

`pi-web` 默认监听 `0.0.0.0`。服务端执行的 shell 命令继承进程权限；应用层权限控制不等同于操作系统或容器沙箱。暴露到网络前，请自行配置网络隔离、TLS 和访问控制。

## 用户级配置

应用配置默认位于 `~/.pi/agent/pi-web-config.json`；设置 `PI_CODING_AGENT_SESSION_DIR` 后，配置文件位于该 session 目录的父目录。当前支持：

```json
{
  "version": 1,
  "brand": "JuunAI / web",
  "workspace": {
    "allowedRoots": ["~/code", "/workspaces"],
    "maxUploadBytes": 104857600
  }
}
```

- `brand`：左上角应用名称。
- `workspace.allowedRoots`：限制可选择和访问的工作区根目录；缺失或空数组表示不限制。
- `workspace.maxUploadBytes`：工作区单文件上传上限，默认 100 MiB。

删除配置文件会恢复默认值。账号、工作区授权和会话归属保存在 `pi-web.db`，不与 Pi 的 `settings.json`、`auth.json` 或浏览器存储混用。

## 从上游同步

本 Fork 保留源项目关系，建议使用两个 remote：

```bash
git remote add upstream https://github.com/Epsilondelta-ai/pi-web.git
git fetch upstream
git switch main
git merge upstream/main
```

JuunAI 的定制修改应在自己的分支提交；同步上游后运行完整检查，再合并到 `main` 发布。

## 本地开发

要求：Go 1.26、Bun 1.3+、Node.js 22.12+，以及可执行的 `pi`（真实模式需要）。

```bash
bun install --frozen-lockfile
bun run dev        # Astro 开发服务器
bun run backend    # Go 后端
bun run lint
bun run typecheck
bun run test
bun run backend:test
bun run build:single
```

`bun run build:single` 会先构建 Astro，再刷新 `static/` 嵌入资源并生成 `dist/pi-web`。发布前应在干净工作区执行检查，并确认生成的静态资源已与代码同步。

更多说明：

- [仓库结构](docs/repository-structure.md)
- [后端布局](backend/README.md)
- [手动测试清单](docs/manual-testing-task1-4.md)
- [版本测试计划](docs/releases/v1.10.0-alpha.1-test-plan.md)

## License

MIT，详见 [LICENSE](LICENSE)。

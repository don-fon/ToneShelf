# ToneShelf

ToneShelf 是一个自托管的多设备音乐播放器服务，分为 Go 服务端和 React/TypeScript 网页客户端。服务器负责音乐、歌词、歌单、持久化、音频流和必要的转码，音频由访问服务的浏览器播放。

本机运行只是初期开发和调试方式，不是产品的最终定位。后续服务将部署到 Linux 服务器或 NAS，手机和电脑通过网页共同访问服务器上的音乐库、歌词和歌单，并分别在各自浏览器中播放音乐。

项目目前已有最小可运行版本，用于验证上传、转码、浏览器播放、歌单和歌曲级播放设置。它还不是完整初版。歌曲设置支持选择本地图片、预览并确认替换封面。

当前技术方向为 Go 服务端与 React/TypeScript 网页客户端。初期在 Windows 本机开发和调试，正式发布目标包括 Linux x64、Linux ARM64 与 NAS。

## 运行最小版本

需要 Go 1.27、Node.js 24（或当前 LTS）、npm，以及可从 `PATH` 调用的 FFmpeg/ffprobe。

Windows PowerShell：

```powershell
.\scripts\start.ps1
```

Linux / NAS：

```sh
./scripts/start.sh
```

脚本会检查 Go、Node.js/npm 和 FFmpeg/ffprobe，随后自动安装前端依赖、构建网页并启动服务。如果 PowerShell 的脚本执行策略阻止运行，可以使用 `powershell -ExecutionPolicy Bypass -File .\scripts\start.ps1`。

首次运行或升级后，先为原音乐库设置账号（在仓库目录执行）：

```sh
go run ./apps/server/cmd/music-player account add --data-dir ./.data --id default-user
```

命令输出随机生成的登录账号，只显示一次。随后启动服务并访问 <http://127.0.0.1:8080>，输入该账号登录。账号本身承担密码作用，登录框为普通文本输入；客户端不能注册。升级不会删除原歌曲和设置，也不会自动启用匿名登录。

开发启动脚本默认继续使用仓库根目录的 `.data`；提供 `--config`、`--data-dir`、`MUSIC_CONFIG` 或 `MUSIC_DATA_DIR` 时使用指定配置。服务端启动日志打印实际配置文件、数据目录、数据库和监听地址。部署到其他设备访问时，应显式设置监听地址；账号校验支持 HTTP，但传输加密仍需 HTTPS。

## 服务器账号管理

部署后直接使用服务程序，不需要 Go 开发环境。各命令均需指定与服务相同的 `--config` 或 `--data-dir`：

```sh
./music-player account add --data-dir /var/lib/music-player --id default-user
./music-player account add --data-dir /var/lib/music-player --id phone-user --name 手机用户
./music-player account list --data-dir /var/lib/music-player
./music-player account reset --data-dir /var/lib/music-player --id default-user
./music-player account disable --data-dir /var/lib/music-player --id phone-user
```

`add` 为尚未配置凭据的用户添加账号，新用户自动创建空默认歌单；`reset` 生成新账号并撤销全部旧会话，也可重新启用已停用用户。`disable` 撤销该用户凭据与会话，保留音乐库。账号不可从列表找回，遗失时使用 `reset`。添加 `--account-stdin` 可通过标准输入提供自己的账号，命令不回显该值；不要将账号写到命令行参数、脚本或版本库中。会话默认有效 30 天，退出登录会立即撤销当前会话。账号凭据、会话、稳定用户 ID 分开存储，后续可加入独立密码。

## 配置与数据目录

配置优先级为 **命令行 > 环境变量 > config.json > 平台默认值**。直接运行服务器时，Windows 默认使用 `%LOCALAPPDATA%/MusicPlayer/`，配置文件位于该目录的 `config.json`；Linux 默认读取 `/etc/music-player/config.json`，数据位于 `/var/lib/music-player/`。非系统安装可用 `--data-dir` 指向有写权限的用户目录。显式指定的配置文件不存在或格式无效时，启动会报错。

可复制 [config.example.json](config.example.json) 为自己的配置文件：

```powershell
.\scripts\start.ps1 --config .\config.json
# 也可以直接启动，命令行参数会覆盖配置文件和环境变量
go run ./apps/server/cmd/music-player --config ./config.json --data-dir ./.data --addr 127.0.0.1:8080
```

```sh
./scripts/start.sh --config ./config.json
./music-player --data-dir /var/lib/music-player --web-dir ./web
```

| JSON 字段 | 环境变量 | 命令行参数 |
| --- | --- | --- |
| 配置文件选择 | `MUSIC_CONFIG` | `--config` |
| `addr` | `MUSIC_ADDR` | `--addr` |
| `dataDir` | `MUSIC_DATA_DIR` | `--data-dir` |
| `database` | `MUSIC_DATABASE` | `--database` |
| `webDir` | `MUSIC_WEB_DIR` | `--web-dir` |
| `quotaBytes` | `MUSIC_QUOTA_BYTES` | `--quota-bytes` |
| `ffmpeg` / `ffprobe` | `MUSIC_FFMPEG` / `MUSIC_FFPROBE` | `--ffmpeg` / `--ffprobe` |
| `logLevel` | `MUSIC_LOG_LEVEL` | `--log-level` |
| `lrclibUrl` | `MUSIC_LRCLIB_URL` | `--lrclib-url` |

`database` 默认是 `data/music.db`，必须相对数据根目录，不能使用绝对路径或 `..` 越界。配置文件中的相对数据目录、网页目录和工具路径相对配置文件所在目录；环境变量和命令行中的相对路径相对启动工作目录。工具填 `ffmpeg` / `ffprobe` 这样的命令名时从 `PATH` 查找。空 `webDir` 自动查找可执行文件旁的 `web/` 和开发构建目录。日志等级支持 `debug`、`info`、`warn`、`error`，配额使用字节，默认 10 GiB。运行 `--help` 查看所有参数。

数据库中的媒体、封面、任务候选及待清理文件使用相对数据根目录的路径。旧库会通过版本化迁移转换路径；请先在旧数据目录完成一次升级，再停服整体移动数据目录并修改 `dataDir`。无需手改数据库，不会自动搬迁或删除旧目录。SQLite 数据库须位于本机文件系统或本地容器卷，不能放在 SMB/NFS 上。

开发网页时，可另开终端运行 `npm run dev`，Vite 会把 `/api` 请求转发到 Go 服务。

## 验证

提交前运行完整验证命令：

```powershell
npm run verify
```

该命令依次执行 Go 测试与静态检查、共享播放策略和网页回归测试、TypeScript 类型检查以及生产构建。安装了 FFmpeg/ffprobe 时，Go 集成测试还会实际生成 WAV、上传转码并验证 Range 播放链路。

## 当前已实现

- MP3、AAC/M4A、WAV、FLAC、Ogg/Opus、WMA、ALAC 等音频上传与元数据探测；网页优先播放时间轴可靠的原文件，格式不支持或解码失败时回退到后台生成的 MP3 兼容副本；裸 AAC 默认使用兼容副本。
- 默认歌单、创建歌单、向歌单添加音乐。
- 浏览器播放、Range 分段请求、单曲循环和歌单内加权随机播放；MP3 兼容副本带时间索引，旧副本在启动后由可恢复任务自动修复。
- 歌曲名称、歌手、随机权重、播放区间、单曲音量和歌词编辑；自定义名称和歌手在替换音频后保留。
- 使用元数据或简易文件名解析从 LRCLIB 搜索歌词候选，由用户选择后保存。
- 播放 LRC 时高亮并自动滚动当前行，支持点击歌词跳转；普通文本歌词按行展示。
- 多文件并发上传、逐文件上传进度与处理状态；刷新后从服务器恢复任务列表，完整接收的文件在服务重启后继续处理。
- 10 GiB 配额与原子空间预留；上传、转码和替换共享额度，统一显示已用及总容量（含处理中预留）；刷新后恢复当前标签页的播放会话。
- 失败候选文件自动清理，清理失败保留额度并重试；启动时清理托管媒体目录中的无主文件。
- 歌单重命名、删除、歌曲移除；列表按上传时间或文件名正序、逆序显示；删除普通歌单不会删除歌曲。
- 回收站查看与恢复；歌曲满 72 小时后由启动、定时和请求触发的清理器永久移除，失败会保留状态并重试。
- 在保留歌曲 ID、歌词、播放设置和歌单关系的前提下替换音频；切换成功后立即清理旧文件，失败则持久化等待重试。

独立密码、手动重试转码和完整部署方案仍待实现。

## 文档

- [产品需求规格](docs/product-requirements.md)
- [初步技术设计](docs/technical-design.md)
- [Linux 部署与维护](docs/linux-deployment.md)

## 初版范围摘要

- 网页客户端同时面向手机和电脑浏览器，访问同一个服务器音乐库。
- 账号由服务器管理员创建，网页登录后访问各自音乐库，不提供客户端注册。
- 上传音乐后自动加入默认歌单。
- 默认空间配额为 10 GiB，客户端显示已用容量。
- 用户可以创建和编辑歌单。
- 删除的歌曲先进入回收站，满 72 小时后由延迟清理触发器永久移除。
- 支持单曲循环和歌单内加权随机播放。
- 每首歌曲可以设置随机权重、播放片段和单曲音量。
- 支持为歌曲添加和编辑歌词。
- 尽量接受常见音频格式，并为浏览器不兼容的格式生成播放副本。
- 使用浏览器本地存储恢复意外刷新前的播放进度和随机会话。

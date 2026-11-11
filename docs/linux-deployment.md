# Linux 部署与维护

当前发布方式是 Linux 可执行程序与独立 `web/` 构建目录，由同一个 Go 进程提供网页、API 和音频；生产服务器不需要 Go、Node.js 或 Vite。服务器需安装 FFmpeg、ffprobe，使用 Nginx 提供访问入口。

## 目录与运行用户

| 路径 | 用途 |
| --- | --- |
| `/opt/music-player/releases/<版本>/` | 对应版本的 `music-player` 和 `web/`，由 root 管理 |
| `/opt/music-player/current` | 指向当前发布目录的符号链接 |
| `/etc/music-player/config.json` | 服务配置，由 root 管理，`music-player` 组可读 |
| `/var/lib/music-player/data/music.db` | SQLite 数据库及同目录的 WAL 文件 |
| `/var/lib/music-player/media/` | 原始音频、播放副本、封面和暂存文件 |

Go 进程使用独立的 `music-player` 系统用户，不使用 root。数据目录归该用户所有；数据库和媒体文件不放在网页目录中。数据库必须在本机文件系统上。配置字段及覆盖规则见 [README](../README.md#配置与数据目录)。

可使用 [systemd 模板](../scripts/deploy/music-player.service)，并保证目录与配置一致。网页目录显式设为 `/opt/music-player/current/web`，Go 监听 `127.0.0.1:8080`；公网不直接开放该后端端口。

## 访问入口

Nginx 转发到回环地址，账号校验由 Go 服务统一执行，音频 Range 请求仍由 Go 处理。[HTTP 模板](../scripts/deploy/nginx-http.conf) 适用于专用服务器，启用前需检查已有站点，不能直接覆盖其他应用的默认站点。

仅在明确选择未加密公网访问后使用该 HTTP 模板。它需要安全组放行 TCP 80，不能加密账号和会话，浏览器也不能通过这个 HTTP 地址使用悬浮歌词；使用 HTTPS 时，应单独配置证书与 HTTPS 站点。保持应用后端端口只在回环地址监听。

## 服务管理

```sh
sudo systemctl status music-player nginx
sudo journalctl -u music-player -n 100 --no-pager
sudo systemctl restart music-player
sudo nginx -t
```

两个服务均应启用自启。正式用户只能通过服务端管理员命令管理，命令始终指定实际配置，避免误操作另一个数据库：

```sh
sudo /opt/music-player/current/music-player account list --config /etc/music-player/config.json
```

其他账号命令见 [服务器账号管理](../README.md#服务器账号管理)。已有账号及登录校验值随数据库迁移，不能通过列表找回原始账号。

## 迁移、备份与更新

1. 等上传和转码结束，停止源服务，再复制一致的数据库与全部 `media/`。运行中的 SQLite 不能只复制主数据库文件；在线快照必须使用 SQLite 备份接口，并另外确保媒体文件版本一致。
2. 保留源数据冷备份。通过 SHA-256 校验媒体和程序，检查数据库完整性、外键、相对路径、文件存在性及容量统计后，才启动目标服务。
3. 在目标 Linux 上验证登录、匿名拒绝、歌曲与回收站、音频拖动、封面、歌词和重启恢复。上传转码等会修改数据的验证使用独立临时音乐库，不往正式歌单加入测试歌曲。
4. 迁移不重置回收站期限；已到期歌曲仍会按原规则清理。浏览器会话和 Cookie 按访问地址隔离，换服务器地址后需重新登录。
5. 后续以目标服务器为写入来源，避免同时修改源库和目标库。

日常备份包含整个数据目录与服务配置，备份文件应仅管理员可读，因为数据库包含账号校验值和会话信息。更新程序时，先备份数据，再放入新的版本目录，切换 `current` 并重启；保留旧版本。发生数据库迁移后，回滚不能只切换旧二进制，还需恢复与旧版本相匹配的数据备份。

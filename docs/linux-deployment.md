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

### Let's Encrypt 域名证书

域名的 A 记录须指向服务器公网 IP；存在 AAAA 记录时，对应 IPv6 也必须能访问该站点。域名解析与备案是独立步骤，备案通过不会自动添加 DNS 记录。

域名站点使用 [域名 HTTPS 模板](../scripts/deploy/nginx-domain-https.conf)，把 `SERVER_NAME` 替换为实际域名，安装到独立的 `/etc/nginx/sites-available/music-player-domain` 并链接至 `sites-enabled/`；保留原 IP 站点，不增加第二个 default_server。申请前先只启用模板的 80 端口 server，保留 ACME 校验 location，普通路径暂返回 503，不要提前启用引用未签发证书的 443 server。

通过 `nginx -t` 并重载后，在 ACME 目录放置临时文件，确认公网域名可直接读取，再先模拟签发、后正式签发：

```sh
sudo /opt/certbot/bin/certbot certonly --dry-run --non-interactive --agree-tos --register-unsafely-without-email --webroot -w /var/lib/letsencrypt --cert-name music-player-domain -d SERVER_NAME
sudo /opt/certbot/bin/certbot certonly --non-interactive --agree-tos --register-unsafely-without-email --webroot -w /var/lib/letsencrypt --cert-name music-player-domain -d SERVER_NAME
```

域名证书不使用 IP 证书的 `--required-profile shortlived`。签发后启用完整模板，先备份站点配置，执行 `nginx -t` 成功才重载，失败恢复原配置。播放器服务和数据库无需重启或改动。

已有 `music-player-certbot-renew.timer`、renew service 和 Nginx deploy hook 会续期所有已配置证书，无需新增定时器。验收域名证书及主机名匹配、HTTP 到同域名 HTTPS 的跳转、网页、健康接口、匿名访问拒绝，并执行续期模拟：

```sh
sudo /opt/certbot/bin/certbot renew --cert-name music-player-domain --dry-run --run-deploy-hooks --no-random-sleep-on-renew
```

换成域名访问后，原 IP 地址上的登录 Cookie 和浏览器播放会话不会跨域搬迁，需要在域名重新登录。音乐、歌词、标签和备注仍使用同一服务器数据库；IP 入口可继续沿用独立的 IP 证书与站点。

### Let's Encrypt IP 证书

公网 IPv4 地址可直接申请 Let's Encrypt 的 `shortlived` 证书，无需域名，有效期约 6 天。使用 Certbot 5.4 或更新版本的 webroot 模式，签发和续期都不停止 Nginx；系统软件源版本不满足时，可按 [Certbot 官方 pip 安装说明](https://certbot.eff.org/instructions?os=pip&ws=other) 安装到独立 `/opt/certbot` 环境。功能及版本要求见 [Let's Encrypt 官方说明](https://letsencrypt.org/2026/03/11/shorter-certs-certbot.html)。

1. 在 HTTP 站点中先加入 [HTTPS 模板](../scripts/deploy/nginx-https.conf) 中的 `/.well-known/acme-challenge/` location，将 `/var/lib/letsencrypt` 及其校验子目录设为 Nginx 可读。安全组保持 TCP 80 可达，备份配置并通过 `nginx -t` 后重载；不要在证书签发前启用引用证书文件的 443 站点。
2. 将下面的 `SERVER_IP` 换成服务器公网 IPv4 地址，先测试，再正式签发。命令使用无联系邮箱模式，证书及 ACME 私钥仅保留在服务器，不提交 Git。

```sh
sudo /opt/certbot/bin/certbot certonly --dry-run --non-interactive --agree-tos --register-unsafely-without-email --required-profile shortlived --webroot --webroot-path /var/lib/letsencrypt --ip-address SERVER_IP --cert-name music-player-ip
sudo /opt/certbot/bin/certbot certonly --non-interactive --agree-tos --register-unsafely-without-email --required-profile shortlived --webroot --webroot-path /var/lib/letsencrypt --ip-address SERVER_IP --cert-name music-player-ip
```

3. 用 HTTPS 模板替换该播放器站点配置，将其中全部 `SERVER_IP` 替换为证书覆盖的 IP，放行 TCP 443，检查配置后重载。HTTP 校验路径保持直出，其他 HTTP 请求以 308 跳转到固定 IP 的 HTTPS 地址；后端仍为 `127.0.0.1:8080`。代理传递 HTTPS 协议，应用据此设置 Secure 会话 Cookie。HTTP 与 HTTPS 的浏览器播放存储分开，切换后播放进度不会自动搬迁。
4. 将 [续期服务](../scripts/deploy/music-player-certbot-renew.service) 和 [定时器](../scripts/deploy/music-player-certbot-renew.timer) 放到 `/etc/systemd/system/`；将 [Nginx 重载脚本](../scripts/deploy/certbot-reload-nginx.sh) 安装为 `/etc/letsencrypt/renewal-hooks/deploy/music-player-reload-nginx`，权限 `0755`。这些文件由 root 管理。定时器每天检查两次，Certbot 决定何时实际续期，成功后先检查 Nginx 配置再平滑重载。

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now music-player-certbot-renew.timer
sudo /opt/certbot/bin/certbot renew --cert-name music-player-ip --dry-run --run-deploy-hooks --no-random-sleep-on-renew
sudo systemctl list-timers music-player-certbot-renew.timer --no-pager
sudo journalctl -u music-player-certbot-renew.service -n 100 --no-pager
sudo /opt/certbot/bin/certbot certificates
```

验收应包含公网证书链及 IP 匹配、HTTP 跳转、匿名 API 返回 401、Cookie 的 Secure 标志、续期模拟及重载钩子；不能仅检查 443 端口打开。定期检查续期任务日志与证书剩余有效期，维护 Certbot 环境更新。公网 IP 更换后需重新签发并更新 Nginx 跳转地址。悬浮歌词仍取决于浏览器是否支持 Document Picture-in-Picture。

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

### 使用 Workbench 一键更新

Windows 在仓库根目录运行：

```powershell
.\scripts\update.ps1 -InstanceId <实例ID>
```

实例 ID 使用 Workbench 查询结果中的 `instance_id`，不是公网 IP。地域默认为 `cn-hangzhou`；其他地域加 `-Region`，指定凭证 Profile 加 `-Profile`。Workbench 未在 PATH 中时使用 `-WorkbenchPath 'C:\工具目录\workbench.exe'`，不需要把凭据复制进仓库。命令使用 Workbench 的默认 root 免密通道；调用方式依据[官方 CLI 文档](https://help.aliyun.com/zh/ecs/user-guide/use-workbench-cli-to-manage-ecs-instances)。

本机需要 Git、Python 3.10+、Node.js/npm、Go 及已完成凭证配置的 Workbench CLI。远端需要 Python 3.10+、systemd、GNU cp 和已有的 FFmpeg/ffprobe；按本文布局安装，`current` 必须是版本目录的符号链接，`webDir` 必须为 `/opt/music-player/current/web`，后端监听回环地址。脚本核对 systemd 的程序与配置路径；不支持 `MUSIC_*` 环境覆盖或 EnvironmentFile，避免备份和实际运行的数据目录不一致。不能用于首次安装或迁移音乐库，也不会修改 Nginx、证书、账号或服务器配置。

仅构建并验证发布包、不连接 ECS：

```powershell
.\scripts\update.ps1 -PackageOnly
```

默认构建 Linux x64；ARM64/NAS 使用 `-Arch arm64`，服务器端会核对架构。Linux/macOS 开发机可使用同一入口的 Python 实现：

```sh
python3 scripts/update.py --instance-id <实例ID> --region cn-hangzhou
python3 scripts/update.py --package-only --arch arm64
```

流程：

1. 实际部署要求工作区干净；仅打包允许未提交改动。执行更新脚本测试、`npm ci`、前端测试/类型检查/构建、完整 Go 测试与 vet，再交叉编译服务器。
2. 将可执行程序和 `web/` 打成 `.cache/releases/<版本>/release.tar.gz`，附逐文件 SHA-256 清单。不会打包本地歌曲、数据库、账号、配置或日志。
3. 上传至远端独立的 `/var/tmp/toneshelf-update-<版本>/`；校验压缩包、清单、路径及 CPU 架构，准备版本目录。上传或校验失败不停止现有服务。Workbench 命令本身与返回 JSON 的远端 `exit_code` 都必须成功。
4. 持有服务器更新锁，停止并确认服务完全退出，检查数据库完整性与外键。有未结束的上传/后台任务时取消更新并重启原服务；运行前先等任务完成。
5. 对整个数据目录和配置做冷备份。默认数据目录的备份位于 `/var/lib/music-player-backups/<版本>/`，只有管理员可访问；其他数据目录在同级 `<目录名>-backups/`。备份完成标记为 `complete.json`，检查副本数据库；磁盘不足或备份失败不切换版本，并重启原服务。备份包含全部音频，需要足够空闲空间，停服时间取决于数据量与磁盘速度。
6. 原子切换 `current`，启动新版，检查 `/api/health`、网页及匿名 `/api/current-user` 返回 401。程序启动可能自动迁移数据库。若启动或检查失败，停止新版并保留完整冷备份、旧版本与新版本，返回失败；不自动降级数据库，不自动切回旧程序。

正常更新有短暂停服；浏览器可能需要刷新页面以加载新网页资源。脚本检查服务器本机应用，不把公网 DNS、Nginx 或 TLS 的状态等同于应用健康。版本目录、上传包和备份不自动删除，更新后可按需要保留、清理；备份中含登录凭据校验值和会话信息，不要公开下载。

失败恢复前先查看脚本输出和 `journalctl -u music-player`。若新版已启动过，停止服务，使用 `complete.json` 中记录的路径将当前数据目录另存，然后完整恢复该备份的 `data/`（包括媒体、数据库及 WAL），保留文件所有权；恢复对应配置，并将 `current` 切回记录的 `previous`，再启动检查。不能在服务运行时覆盖数据库。连接中断时也先检查服务、版本链接及备份标记，不要盲目重复更新；不带 `complete.json` 的目录不是可恢复的完整备份。

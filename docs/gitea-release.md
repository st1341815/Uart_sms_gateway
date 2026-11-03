# 本地 Gitea 发版流水线

> 目标：**一条命令发版**，镜像、发布包、Release 全部由本机 Gitea 自动产出，
> 不再依赖 GitHub / Docker Hub / GHCR。

## 1. 总览

```
             git push gitea master + vX.Y.Z tag
                          │
                          ▼
        ┌───────────────────────────────────────┐
        │  Gitea (127.0.0.1:3000, v28.3.0)      │
        │  └─ Actions: .gitea/workflows/release.yml
        │       └─ act_runner (容器, capacity=1) │
        │            └─ 作业容器 catthehacker/ubuntu:act-latest
        │                 ├─ node:22      → 前端构建（embed_dir）
        │                 ├─ golang:1.25  → 8 平台二进制 + tar.gz/zip
        │                 └─ buildx(host net) → amd64+arm64 镜像
        │                                       └→ Gitea 内置 registry
        │                 └─ scripts/ci-make-release.sh → Release + 附件
        └───────────────────────────────────────┘
```

产物：

| 产物 | 位置 |
|---|---|
| 多架构镜像 | `192.168.11.7:3000/st1341815/uart_sms_gateway:X.Y.Z` 与 `:latest`（linux/amd64 + linux/arm64） |
| 发布包 | Release 附件：`uart_sms_forwarder-X.Y.Z-<os>-<arch>.tar.gz`、Windows `.exe.zip`、`SHA256SUMS` |
| Release | `http://192.168.11.7:3000/st1341815/uart_sms_gateway/releases/tag/vX.Y.Z` |

## 2. 一次性准备（已在本机完成，换机时照抄）

### 2.1 Gitea 开 Actions

`gitea/gitea/conf/app.ini` 追加（改完重启 gitea 容器）：

```ini
[actions]
ENABLED = true
DEFAULT_ACTIONS_URL = self
```

`DEFAULT_ACTIONS_URL = self` 让 `uses: xxx` 从本机 Gitea 取，而不是去 GitHub 取。
（本流水线**不使用任何 action**，这项只是兜底。）

> ⚠️ `HTTP_PORT` 必须与对外映射端口一致（本机 3000）。曾误改为 3001，导致
> 所有仓库 push 报 `Internal Server Connection Error`（pre-receive hook 回调失败）。

### 2.2 act_runner

配置 `/vol1/1000/Docker/Gitea/act_runner/config.yaml` 关键项：

```yaml
runner:
  capacity: 1
  timeout: 30m
cache:
  enabled: true
container:
  network: "gitea_default"     # 关键：作业容器能解析 gitea:3000
  valid_volumes: ["**"]
docker_host: "unix:///var/run/docker.sock"
```

注册必须用**仓库级** token（用用户级 token 会抢别的仓库的作业）：

```sh
RT=$(curl -sS -X POST "$GITEA/api/v1/repos/st1341815/uart_sms_gateway/actions/runners/registration-token" \
      -H "Authorization: token $GITEA_TOKEN" | python3 -c 'import json,sys;print(json.load(sys.stdin)["token"])')
docker run --rm --network host --entrypoint act_runner \
  -v /vol1/1000/Docker/Gitea/act_runner:/data -w /data gitea/act_runner:latest \
  register --no-interactive --instance "http://127.0.0.1:3000" --token "$RT" \
  --name local-build --labels "ubuntu-latest:docker://catthehacker/ubuntu:act-latest"
```

注册后把 `/data/.runner` 里的 `address` 从 `http://127.0.0.1:3000` 改成
`http://gitea:3000`（runner 容器在 `gitea_default` 网络里，用服务名访问）。

启动：

```sh
docker run -d --name act_runner --restart always \
  --network gitea_default \
  -v /vol1/1000/Docker/Gitea/act_runner:/data \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -w /data gitea/act_runner:latest daemon --config /data/config.yaml
```

### 2.3 仓库 secret

工作流用仓库级 secret `RELEASE_TOKEN`（Gitea 拒收以 `GITEA_` / `GITHUB_` 开头的名字）：

```sh
curl -X PUT "$GITEA/api/v1/repos/st1341815/uart_sms_gateway/actions/secrets/RELEASE_TOKEN" \
  -H "Authorization: token $GITEA_TOKEN" -H 'Content-Type: application/json' \
  -d '{"data":"<token>"}'
```

token 权限至少 `write:repository`（建 Release、传附件）+ `write:package`（推镜像）。

### 2.4 宿主侧镜像

buildx 的 `docker-container` driver 要一个 buildkit 镜像。本机 docker.io 直连不通，
先从镜像源拉好再打 tag（**只需一次**）：

```sh
sudo docker pull docker.fnnas.com/moby/buildkit:buildx-stable-1
sudo docker tag  docker.fnnas.com/moby/buildkit:buildx-stable-1 moby/buildkit:buildx-stable-1
```

## 3. 日常发版

```sh
# 1) 改号（同时改 web/src/lib/version.ts / internal/version/version.go / CHANGELOG.md）
make bump-version V=1.2.1
$EDITOR CHANGELOG.md          # 把占位节写成真实内容
git commit -am "chore: 版本号升到 1.2.1"

# 2) 一条命令发版（推 master + 推 tag + 等流水线 + 打印结果）
GITEA_TOKEN=<token> ./scripts/release-gitea.sh
```

`scripts/release-gitea.sh` 会：

1. 检查工作区是否干净、`check_version.py` 三处是否一致；
2. `git push` master；
3. 打 annotated tag（省略参数时用当前版本号）并推 tag；
4. 轮询 `actions/runs`，等这次 tag 的 run 跑完（失败会打印日志尾巴并以退出码 3 结束）；
5. 打印 `docker pull` 地址、Release 链接、附件清单。

**也可以用 Git 命令手动触发**（脚本挂了时的退路）：

```sh
git push gitea master
git push gitea v1.2.1        # 推 tag 即触发
```

## 4. 流水线每一步做了什么

| 步骤 | 说明 |
|---|---|
| 环境检查 | 打印 tag / 镜像地址 / docker、buildx、node 版本 |
| 检出代码（裸 git） | **不用 `actions/checkout`**：act_runner 无 DinD，拉 GitHub action 会 i/o timeout。改为 `git init` + `git fetch <tag>`（仓库公开则匿名拉，转私有后自动带 token） |
| 版本号一致性 | `python3 scripts/check_version.py` |
| 构建前端 | `node:22-bookworm` 里 `npm ci && npm run build`，产物覆盖 `web/embed_dir` |
| 多平台二进制 | `golang:1.25` 里 `scripts/build-release.sh`（8 平台 + tar.gz/zip + SHA256SUMS） |
| 多架构镜像 | `buildx`（docker-container driver，`network=host`）→ amd64 + arm64 清单，推到 Gitea registry |
| 建 Release | `scripts/ci-make-release.sh` 走 Gitea Release API（附件的字段是 multipart `attachment`，与 GitHub 不同），幂等 |
| 汇总 | 写入 job summary |

### 为什么镜像 tag 用 `127.0.0.1:3000`

buildkit 容器是 `network=host`，能解析的只有宿主自己的地址；作业容器里的
`127.0.0.1` 是它自己，所以「登录 registry」这一步被换成**直接写
`~/.docker/config.json`**（buildx 会把凭证带给 buildkit）。

Gitea registry 的仓库路径与访问域名无关，因此推 `127.0.0.1:3000/...` 与用户侧
`192.168.11.7:3000/...` 指向**同一个镜像**；Release 正文里展示的是后者。

## 5. 踩过的坑（改之前先看）

1. **别用任何 GitHub action**：`actions/checkout` / `setup-go` / `setup-node` /
   `softprops/action-gh-release` 在 act_runner 下要么超时、要么调 api.github.com 失败。
2. **`.github/workflows` 会被 Gitea 一起读**：同一份工作流会跑两遍（Gitea + GitHub 版），
   GitHub 版必挂还占住 `capacity=1` 的队列。所以 GitHub 版已挪到
   `docs/github-workflows/`，要恢复见那里的 README。
3. **buildkit 拉 `docker.io` 会 EOF**：`scripts/buildkitd.toml` 配了镜像源
   （`docker.1ms.run`）。buildx 是客户端读取该文件后内嵌，所以仓库相对路径即可。
4. **多架构必须 `docker-container` driver**：dockerd 默认 `docker` driver 不支持
   manifest list。
5. **`HTTP_PORT` 与映射端口不一致** → push 全挂。
6. **secret 名字不能以 `GITEA_` / `GITHUB_` 开头** → 用 `RELEASE_TOKEN`。
7. **贴纸**：`docker buildx rm <name>` 在作业容器里执行没问题（builder 属于宿主 dockerd），
   流水线会先 `rm` 再 `create`，保证幂等。

## 6. 排障

```sh
# runner 是否在线
GITEA_TOKEN=xxx curl -sS "$GITEA/api/v1/repos/st1341815/uart_sms_gateway/actions/runners" \
  -H "Authorization: token $GITEA_TOKEN" | python3 -m json.tool | grep -E 'name|status'

# 某次 run 的结论
curl -sS "$GITEA/api/v1/repos/st1341815/uart_sms_gateway/actions/runs?limit=5" \
  -H "Authorization: token $GITEA_TOKEN" | python3 -m json.tool | grep -E '"id"|head_branch|conclusion'

# 日志（zip）
curl -sS "$GITEA/api/v1/repos/st1341815/uart_sms_gateway/actions/runs/<id>/logs" \
  -H "Authorization: token $GITEA_TOKEN" -o /tmp/run.logs.zip && unzip -o /tmp/run.logs.zip -d /tmp/run.logs

# 镜像清单
sudo docker buildx imagetools inspect 127.0.0.1:3000/st1341815/uart_sms_gateway:X.Y.Z
```

runner 容器里的网络自检：

```sh
docker exec act_runner wget -qO- http://gitea:3000/api/v1/version
```

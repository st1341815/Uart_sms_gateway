# GitHub 版 workflow（归档）

这里的三个文件是 v1.1.3 之前的 GitHub Actions 工作流，**内容原样保留**：

| 文件 | 作用 |
| --- | --- |
| `ci.yml` | push/PR 跑 go vet + go test + 前端 eslint + build |
| `release.yml` | push tag `v*` 时用 make 构 8 平台二进制并发 GitHub Release |
| `docker-build.yml` | push tag `v*` 时推 Docker Hub（13uncle）与 GHCR 多架构镜像 |

## 为什么从 `.github/workflows/` 挪走

本地 Gitea 的 act_runner **同时**读 `.github/workflows/` 和 `.gitea/workflows/`。
两套 Release 工作流并存 → 每次推 tag 都跑两遍，而 GitHub 版依赖
`actions/checkout`，本机 runner 拉不到这个 action（内外网拓扑原因），
每次都白跑 30 秒超时，把并发为 1 的队列堵住。

## 什么时候拷回去

要恢复 GitHub Actions 时：

```sh
cp docs/github-workflows/*.yml .github/workflows/
```

前提：GitHub 仓库里配好 `DOCKERHUB_USERNAME` / `DOCKERHUB_PASSWORD` secrets
（没配时 `docker-build.yml` 会自动跳过 Docker Hub，只推 GHCR）。

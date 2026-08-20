# 分支同步与镜像构建说明

本文档用于说明当前仓库的分支用途、与上游同步的方法、手动构建 Docker 镜像的方法，以及服务器端如何使用自定义镜像。

## 当前分支约定

- `main`
  - 用途：尽量保持跟随上游 `upstream/main`
  - 允许保留的 fork 自定义内容：`.github/workflows/manual-image-build.yml`
  - 不建议放 UI 定制代码

- `my`
  - 用途：存放你自己的页面和 UI 改动
  - 构建镜像时应优先选择这个分支

## 当前远程约定

- `origin`: 你的 fork
  - `https://github.com/xiaohuihuitop/sub2api.git`

- `upstream`: 上游原仓库
  - `https://github.com/Wei-Shaw/sub2api.git`

## 为什么 `Manual Docker Image Build` 要放在 `main`

GitHub Actions 的 `workflow_dispatch` 手动工作流，必须存在于默认分支，才会在 GitHub Actions 页面中显示，并提供 `Run workflow` 按钮。

因此：

- `manual-image-build.yml` 需要存在于 `main`
- 实际构建时，可以在 GitHub 页面里选择 `my` 分支

这样可以同时满足：

- `main` 保持接近上游
- `my` 保留你自己的 UI 改动
- GitHub 页面可手动触发镜像构建

## 同步 `main` 的推荐方式

推荐使用 `merge` 或 `rebase`，不要使用 `reset --hard upstream/main`。

### 方式一：rebase

```bash
git checkout main
git fetch upstream
git rebase upstream/main
git push origin main
```

### 方式二：merge

```bash
git checkout main
git fetch upstream
git merge upstream/main
git push origin main
```

## 同步时 workflow 会不会丢

正常情况下不会丢。

只要你使用的是：

- `git merge upstream/main`
- `git rebase upstream/main`

Git 会保留你自己在 `main` 上新增的 `.github/workflows/manual-image-build.yml`。

只有以下情况才可能把它弄没：

- 使用 `git reset --hard upstream/main`
- 手动删除 `.github/workflows/manual-image-build.yml`
- 用强制推送覆盖掉包含该文件的提交

## `my` 分支跟进最新 `main`

当 `main` 同步完上游后，建议让 `my` 也跟上最新 `main`：

```bash
git checkout my
git rebase main
git push origin my --force-with-lease
```

说明：

- `my` 是你的定制分支，通常会有自己的提交历史
- 使用 `rebase` 可以让 `my` 保持在最新 `main` 之上
- 因为 `rebase` 会改写提交历史，所以推送时一般要用 `--force-with-lease`

如果你不想改写历史，也可以使用 `merge`：

```bash
git checkout my
git merge main
git push origin my
```

## GitHub 上手动构建镜像

前提：

- `.github/workflows/manual-image-build.yml` 已经在 `main`
- 你的 UI 改动已经推送到 `my`

### 推送 `my` 分支

```bash
git checkout my
git push origin my
```

### 进入 GitHub Actions 页面

打开：

- `https://github.com/xiaohuihuitop/sub2api/actions`

然后：

1. 在左侧找到 `Manual Docker Image Build`
2. 点击进入
3. 点击右上角 `Run workflow`
4. 在分支里选择 `my`
5. 填写构建参数

### 参数说明

- `image_tag`
  - 镜像标签，例如：`ui-warm-v1`

- `push_latest`
  - 是否额外推送 `latest`
  - 一般可设为 `true`

## 构建完成后的镜像地址

镜像会推送到 GHCR：

```text
ghcr.io/xiaohuihuitop/sub2api:<image_tag>
```

例如：

```text
ghcr.io/xiaohuihuitop/sub2api:ui-warm-v1
```

如果 `push_latest=true`，还会额外生成：

```text
ghcr.io/xiaohuihuitop/sub2api:latest
```

## 服务器使用方法

### 拉取镜像

```bash
docker pull ghcr.io/xiaohuihuitop/sub2api:latest
```

或者：

```bash
docker pull ghcr.io/xiaohuihuitop/sub2api:ui-warm-v1
```

### docker compose 使用

把原来的 `image` 改成你的 GHCR 镜像，例如：

```yaml
image: ghcr.io/xiaohuihuitop/sub2api:latest
```

然后重启：

```bash
docker compose up -d
```

## 一套推荐操作顺序

如果你后面要继续长期维护，推荐每次按这个顺序做：

1. 同步上游到 `main`

```bash
git checkout main
git fetch upstream
git rebase upstream/main
git push origin main
```

2. 让 `my` 跟上 `main`

```bash
git checkout my
git rebase main
git push origin my --force-with-lease
```

3. 在 `my` 上继续做 UI 修改

4. 推送 `my`

```bash
git push origin my
```

5. 到 GitHub Actions 手动构建镜像

6. 服务器拉取新镜像并重启容器

## 备注

- 如果只是 UI 改动，优先只改 `my`
- 如果是给 GitHub Actions 用的 workflow，优先放 `main`
- 不建议在 `main` 混入大量自定义 UI 提交，否则后续同步上游会越来越乱

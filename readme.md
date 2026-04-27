# bumpbot

fork from `gentoo-zh-drafts/bumpbot`

当前能力：
- 根据 `nvcmp --json --newer` 传入的 `name/newver/oldver` 创建或更新 overlay 中的 nvchecker issue
- 从 `overlay.toml` 读取 `github_account`，在 issue 中附加 `CC`
- 从 `overlay.toml` 读取 `gentoo_deps_*` 字段，在 `gentoo-deps` 仓库创建或更新 deps request issue

## 用法

最小调用：

```bash
GITHUB_REPOSITORY="owner/overlay" \
GITHUB_TOKEN="ghp_xxxx" \
./bumpbot --file overlay.toml --name "dev-util/shfmt" --newver "3.13.1" --oldver "3.13.0"
```

带 `gentoo-deps` issue 联动：

```bash
GITHUB_REPOSITORY="owner/overlay" \
GITHUB_TOKEN="ghp_overlay_xxxx" \
GENTOO_DEPS_REPOSITORY="owner/gentoo-deps" \
GENTOO_DEPS_GITHUB_TOKEN="ghp_deps_xxxx" \
./bumpbot --file overlay.toml --name "dev-util/shfmt" --newver "3.13.1" --oldver "3.13.0"
```

参数说明：
- `--file`: `overlay.toml` 路径
- `--name`: 包名，例如 `dev-util/shfmt`
- `--newver`: 新版本
- `--oldver`: 旧版本，可为空

环境变量：
- `GITHUB_REPOSITORY`: 当前 overlay 仓库，例如 `GENTOO/overlay`
- `GITHUB_TOKEN`: 在 overlay 仓库创建/更新 nvchecker issue 用的 token
- `GENTOO_DEPS_REPOSITORY`: `gentoo-deps` 仓库，例如 `GENTOO/gentoo-deps`
- `GENTOO_DEPS_GITHUB_TOKEN`: 在 `gentoo-deps` 仓库创建/更新 issue 用的 token

说明：
- 只有当包配置了 `gentoo_deps_*` 且未设置 `gentoo_deps_disabled = true` 时，才会创建 `gentoo-deps` issue
- `GENTOO_DEPS_REPOSITORY` 和 `GENTOO_DEPS_GITHUB_TOKEN` 只在需要创建 `gentoo-deps` issue 时才必须设置


### gentoo-deps 字段

```toml
["demo/example"]
gentoo_deps_lang = "golang"
gentoo_deps_tag = "v{{newver}}"
gentoo_deps_p = "shfmt-{{newver}}"
gentoo_deps_repo = "mvdan/sh"
gentoo_deps_workdir = ""
gentoo_deps_vendordir = ""
gentoo_deps_source_url = "git@github.com:demo/example.git" # must end with .git
gentoo_deps_disabled = false
```

字段说明：
- `gentoo_deps_lang`: 必填，支持 `golang`、`javascript`、`javascript(pnpm)`、`rust`、`dart`
- `gentoo_deps_tag`: 必填，传给 `gentoo-deps` 的源码 tag
- `gentoo_deps_p`: 必填，传给 `gentoo-deps` 的 `P`
- `gentoo_deps_repo`: 可选，GitHub 仓库 `owner/name`
- `gentoo_deps_source_url`: 可选，非 GitHub 平台时使用的 git clone URL
- `gentoo_deps_workdir`: 可选，相对源码子目录
- `gentoo_deps_vendordir`: 可选，golang vendor 输出目录
- `gentoo_deps_disabled`: 可选，默认 `false`；显式设为 `true` 时不创建 `gentoo-deps` issue

约束：
- `gentoo_deps_repo` 和 `gentoo_deps_source_url` 至少要有一个
- 如果 `source = "github"` 且未设置 `gentoo_deps_repo`，默认回退到 `github = "owner/name"`

## 模板变量

这些字段支持简单模板替换：
- `gentoo_deps_lang`
- `gentoo_deps_tag`
- `gentoo_deps_p`
- `gentoo_deps_repo`
- `gentoo_deps_source_url`
- `gentoo_deps_workdir`
- `gentoo_deps_vendordir`

支持的变量：
- `{{name}}` / `{{package}}`
- `{{category}}`
- `{{pn}}`
- `{{newver}}`
- `{{oldver}}`

示例：

```toml
gentoo_deps_tag = "v{{newver}}"
gentoo_deps_p = "{{pn}}-{{newver}}"
```

## 生成结果

bumpbot 会：
1. 在 overlay 仓库创建或更新标题为 `[nvchecker] <pkg> can be bump to <newver>` 的 issue
2. 如果配置了 `gentoo_deps_*`，在 `gentoo-deps` 仓库创建或更新标题为 `[deps] <pkg> -> <newver>` 的 issue
3. 将 `gentoo-deps` issue URL 回填到 overlay issue body 中

## 与 gentoo-deps 的配合

推荐流程：
1. overlay CI 运行 bumpbot
2. bumpbot 创建 overlay issue 和 `gentoo-deps` issue
3. 在 `gentoo-deps` issue 中人工检查参数
4. 评论 `/approve`
5. `gentoo-deps` CI 生成 tarball，回帖 release 链接并关闭 issue

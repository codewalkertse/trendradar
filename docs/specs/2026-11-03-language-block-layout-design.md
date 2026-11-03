# 榜单文档按「请求语言」分块 + all 特殊块（方案）

- **状态**：已实施（待发布）
- **实施情况**：代码已改并全量测试通过（`go test ./...` + `contract` 22/22），已验证真实 GitHub 抓取产出；尚未发版、未合并
- **作者**：AI Agent（配合 morehao）
- **评审人**：morehao
- **拍板人**：morehao
- **最后更新**：2026-11-03
- **关联**：[架构总览](../architecture.md) · 现状 v0.8.4（commit `64d48e0`）

---

## 1. 背景与问题

现状：插件把 `cfg.trending.languages`（如 `[all, typescript, python, go, rust]`）传给核心；核心据此逐语言抓取 `github.com/trending`，产出 `digests/<period>/<bucket>/<name>.md`（原文见 `internal/render/markdown.go`）。用户在插件里回看这份文档时，发现三个「对不上」的现象：

1. **分区和页面选择对不上**：只选了 5 个语言，文档里却出现 `## c++`、`## shell`、`## c` 等没选的块。
2. **meta 语言列表失真**：文档头 `languages: [all, python, go, typescript, rust, c++, shell, c]`，比设置多了 3 个。
3. **同一份榜单混了「全局榜」和「分语言榜」**，与用户心中「某个语言的真实 Trending」不一致。

### 1.1 根因（已核实到具体函数）

| # | 现象 | 根因 | 位置 |
|---|---|---|---|
| A | 出现没选的 `## c++/shell/c` | 渲染按**仓库自身主语言**分块；`all` 抓的是全局页，混入各种语言的仓库，各自成块 | `internal/render/markdown.go` `groupByLanguage` / `orderedRepoLanguages` |
| B | meta `languages` 多出语言 | `languagesOf` 从**实际抓到的仓库**反推 languages，总是以 `all` 开头再追加每个仓库语言 | `internal/app/run.go` `languagesOf` |
| C | 全局榜与分语言榜混排 | 抓取是「`all` 全局页 + 各分语言页」并发；`parse.Merge` 按 fullName **全局去重**后按 rank 重排，语言归属取决于哪页 rank 更小被保留 | `internal/parse/parse.go` `ParseAll`→`Merge`；`internal/fetch/trending.go` `trendingPath` |

关键事实：**当前 `RepoSnapshot` 只记录「仓库自己的主语言」，不记录「它来自哪个请求页」**（`contract/contract.gen.go` `RepoSnapshot.Language`）。因此无法「按请求页分块」，必须补上「来源页」信息。

---

## 2. 目标与非目标

### 2.1 目标（已与用户确认口径）

1. 产出文档**按页面所选的编程语言分块**：`## <请求页语言>`，一个请求页一块。
2. **`all` 特殊处理**：`all` 页命中的仓库统一进**单独一块 `## all`**，不再按各自仓库主语言拆散。
3. **空块保留**：某请求语言本次 0 条（如 `rust`），仍输出 `## rust`（空表），让「选了什么、抓过什么」和设置对得上。
4. **跨页重复允许**：同一仓库既在 `all` 页又在 `python` 页时，两块**都列出**，不做跨页去重（按用户明确选择）。
5. **meta 如实回显请求语言**：`languages:` 直接等于请求列表（`cfg.trending.languages` 原序），不再从仓库反推。

### 2.2 非目标

- 不改抓取范围、不改 GitHub 页面解析选择器、不引入新数据源。
- 不引入「按 spoken_language / 话题」等新维度。
- 不改摘要生成、不改 commit/PR 流程、不改产物目录布局（`digests/<period>/<bucket>/`）。
- 不动 `contentHash` 的**算法**（仍是 `jcs-sha256-v1`），但 `languages` 基线的**取值口径**会变（见 §6）。

---

## 3. 方案选型：「来源页」信息怎么带

分块需要「仓库 → 来源页」的映射。有两条路：

### 方案 A（推荐）：旁路映射，不进契约

在采集/解析阶段建立一个 `fullName → sourcePage` 的旁路 map，一路带到渲染层；`Digest`/`RepoSnapshot` 的对外字段**不变**。

- 优点：**不动契约包** `@morehao/trendradar-contract`，`digestSchema` 仍为 2，插件与 Web 零改动；`contentHash` 结构不变。
- 代价：渲染接口需要多带一个 `map[string]string` 参数（内部函数签名调整）；`--input` 重放路径（`run.go` 读 JSON snapshots）没有来源页信息，需给个回退口径（见 §5.4）。

### 方案 B：给 `RepoSnapshot` 加 `sourcePage` 字段

- 优点：信息随数据走，`--input` 重放也保留。
- 代价：改契约包 → 升 `contract` 版本 → 重新生成 Go/TS 类型与 ajv → 插件依赖升级 → `contentHash` 基线变化。**重，且把「渲染口径」污染成对外契约**。

**结论：选方案 A。** 「按哪个页分块」是渲染层展示口径，不是数据语义，不该进 wire 契约。

---

## 4. 目标输出格式（示例）

请求 `languages: [all, python, go, typescript, rust, c++, shell, c]`（示例，含空块与重复）：

```markdown
---
date: 2026-11-03
period: daily
languages: [all, python, go, typescript, rust, c++, shell, c]   # = 请求列表原样
count: 62
degraded: false
contentHash: sha256:...
coreVersion: 0.8.4
generatedAt: 2026-11-03T03:14:08Z
---

# 2026-11-03 趋势摘要

## all
（全局页 /trending 命中的仓库，按 rank 排序；其中语言五花八门，但都归在本块，不再拆）
| 仓库 | Stars | 今日新增 | 摘要 |
| --- | ---: | ---: | --- |
| [some/global-repo](...) | ... | +... | ... |

## python
（/trending/python 页的仓库）
...

## go
...

## typescript
...

## rust
| 仓库 | Stars | 今日新增 | 摘要 |
| --- | ---: | ---: | --- |
（本次无上榜，空块保留）

## c++
...

## shell
...

## c
...
```

**块顺序** = 请求列表顺序（`all` 通常排第一，保持原序即可，不强制置顶）。**块内排序** = 该页 rank 升序。

> 注意：`all` 块与分语言块之间、以及分语言块之间**可能重复**同一仓库（用户选择的「允许重复」）。这是显式取舍，文档里出现重复属预期。

---

## 5. 改动清单

### 5.1 `internal/parse/parse.go` —— 产出「来源页」

- `ParseTrending(body, pageLang)` 当前 pageLang 只当语言回退。改为：解析出的每个 `RepoSnapshot` 之外，同时返回/登记 `fullName → pageLang`（pageLang 即请求页 key，`all` 就是 `"all"`）。
- `ParseAll(pages)` 遍历各页时，累积 `sourcePage := map[string]string`。**注意同 fullName 出现在多页**：因为是「允许重复」，`sourcePage` 应保留**全部**来源，而不是覆盖。建议结构改为 `map[string][]string`（fullName → 出现过的页列表），或直接按「页→仓库」保留二维结构（见 5.3）。
- 推荐在 `ParseAll` 直接返回 `(repos []RepoSnapshot, byPage map[string][]RepoSnapshot, report, error)`：`byPage[pageLang]` 天然表达「每页有哪些仓库」，且天然允许跨页重复。**这比维护 fullName→pages 更直观，也最贴合「按页分块」的目标。**

### 5.2 `internal/ingest/ingest.go` —— 透传 byPage

- `Ingest` 目前只返回 `Repos []RepoSnapshot`。扩为同时返回 `ByPage map[string][]RepoSnapshot`（以及降级信息不变）。
- `fallback`（search API 路径）没有「页」的概念：此时 `ByPage` 只放一个键（`"all"` 或首个请求语言），或退化为「全部归 all 块」。**降级路径的块划分需在 §5.4 明确。**

### 5.3 `internal/render/markdown.go` —— 按页分块

- `MarkdownWithBanners(d, banners)` 签名扩展为 `MarkdownWithBanners(d, byPage, banners)`（或用一个渲染选项结构 `RenderOptions{ByPage, Banners}`）。
- 新分组逻辑 `groupByPage(d, byPage)`：
  - 遍历 `d.Languages`（= 请求列表，顺序即块顺序）；
  - 对每个 `lang`：取 `byPage[lang]`（**all 页的仓库全进 `## all`**，不再看仓库主语言）；**即使为空也输出该块**（空块保留）；
  - 每块内按 rank 升序；
  - 仓库在主语言字段上仍保留原值（`RepoSnapshot.Language` 不变，JSON 不变），只是**分块 key 换成来源页**。
- 旧的 `groupByLanguage` / `orderedRepoLanguages` 若不再使用则删除（或保留给其它调用方——需 grep 确认，目前仅 markdown.go 内部用）。
- `render.JSON(d)` **保持不变**（wire 不含 byPage，契约不变）。

### 5.4 降级路径与 `--input` 重放的块口径

| 场景 | 没有来源页信息 | 建议口径 |
|---|---|---|
| HTML 部分降级（`html_partial`） | 有 byPage（失败的页不在 byPage） | 失败语言块输出为空块 + banner 已说明「抓取失败」；正常块照常 |
| 全量搜索兜底（`search_fallback`） | 无 byPage | 全部归 `## all` 单块；或按 `d.Languages` 里非 all 的顺序尽力用仓库主语言归类但不保证。**建议：统一归 `all`，并在 banner 说明「降级数据，未按语言分块」** |
| `--input` 重放（读 JSON snapshots） | 无 byPage | 退化为按**仓库主语言**分块（等价旧行为 `groupByLanguage`），并在文档头标注 `layout: by-repo-language (input replay)` 之类，或干脆保持旧行为不做特殊块。**建议：重放路径保持旧行为（按主语言分块），因为输入本就不带页信息，不臆造。** |

### 5.5 `internal/app/run.go` —— meta 回显请求语言 + 接线

- `Inputs` 增加字段 `Requested []string`（请求语言列表，来自 `cfg.Trending.Languages`；`--input` 重放时缺省为空）；增加 `ByPage map[string][]digest.RepoSnapshot`（来自 ingest）。
- `Digest.Languages` 改为：优先 `in.Requested`；为空（重放/无 cfg）时回退旧的 `languagesOf(in.Repos)`。`languagesOf` 保留为回退函数。
- 调用 `render.BuildWithBanners(...)` 时把 `byPage` 传入。
- `mergeDigests`（历史合并）只是按日期键去重，不重渲染，无需改；但 `BuildWithBanners` 内部对**今天**这份用 byPage，历史份不涉及（历史份是已落盘 md 的索引，不重新渲染正文）。

### 5.6 `cmd/trendradar/run.go` —— 填空 `Requested`

- `ingest.Ingest(...)` 返回后：`in.Requested = cfg.Trending.Languages`，`in.ByPage = res.ByPage`。
- `--input` 重放分支：`in.Requested` 留空 → 走回退，保持旧行为。

---

## 6. 对契约与 hash 的影响

- **契约**：方案 A 不加字段，`@morehao/trendradar-contract` **不升级**，`digestSchema=2` 不变，插件/Web 无改动。
- **`contentHash`**：算法不变。但 `languages` 是 hash basis 的一部分（`ContentHashJCS` 的 basis 含 `languages`）。口径从「实际抓到的语言」改为「请求列表」后，**同一天重跑产生的 hash 会与旧版本不同**——这是修正导致的预期变化，历史已落盘文件不受影响（不会重算旧文件）。
- **语义变更备案**：`Digest.Languages` 语义从「本次实际抓到的语言集合」变为「本次请求的语言列表」。虽不破坏 schema，但属对外语义变化，需在 `contract` 的 README/CHANGELOG 记一笔，并通知插件侧（插件只透传 + 复制 .md，无需改代码，但文档说明该字段含义变化）。
- **JSON 正文不变**：`.json` 里 `languages` 值会随 §5.5 变化（这是上面说的语义变化），但 `repos[].language` 仍为仓库主语言，结构不变。

---

## 7. 测试计划（离线）

1. **渲染分块单测**（`internal/render/render_test.go`）：
   - 构造 `byPage{all:[A(C++语言),B(Go)], python:[C], rust:[]}` + `d.Languages=[all,python,rust]`；
   - 断言输出含 `## all`（A、B 都在，且不出现 `## c++`）、`## python`（C）、`## rust`（空表）；
   - 断言 `languages:` 行等于请求列表。
2. **跨页重复**：A 同时在 `all` 与 `python` 页 → 两块都出现 A。
3. **meta 回显**：`in.Requested` 非空时 `Digest.Languages == Requested`；空时回退 `languagesOf`。
4. **降级路径**：`search_fallback` → 单块 `## all`。
5. **`--input` 重放**：走旧的分主语言分块（或按 §5.4 约定），加断言。
6. **golden**：更新 `internal/render/testdata/digest.md`（`go test ./internal/render -update`）。
7. **契约测试**：`cd contract && npm ci && npm test` 应保持绿（方案 A 不改 schema）。
8. **AC 冒烟**：`make test`（`go vet ./... && go test ./...`）全绿。

---

## 8. 兼容性与发布

- 核心出 **minor 版本**（如 v0.8.5 或 v0.9.0，按语义变更——`languages` 语义变了，偏 minor 更合适；具体版本档位发布时再定）。
- 插件侧：`scripts/set-core-version.mjs` 升 `dsh.core.version`，走 `check:naming` 同源校验；**插件代码零改动**。
- 老历史 md（旧版分主语言分块）与新 md（按请求页分块）会共存于同一仓库的 `digests/` 下——**格式不冲突**（都是 Markdown + YAML front matter + `## 语言` 块），只是分块口径不同。`index.md` 只列期数，不受影响。此点需在方案评审时确认可接受。

---

## 9. 风险与开放问题

| 风险 / 问题 | 说明 | 倾向 |
|---|---|---|
| `all` 块与分语言块重复 | 用户已确认「允许重复」；但重复可能让文档变长、同一仓库出现两次 | 接受（用户拍板）；若后续嫌乱，可加开关 |
| `all` 块内容庞杂 | 全局榜 25 条语言混杂，`## all` 一块信息密度不均 | 接受；`all` 本就是「全局」特殊类目 |
| `--input` 重放无页信息 | 只能退化旧行为或全归 all | 建议重放保持旧行为，不臆造 |
| `languages` 语义变化 | 外部消费者若依赖「实际抓到语言」会受影响 | 记 CHANGELOG + 通知插件；目前已知消费方仅插件（透传） |
| 空块是否噪音 | 选了 N 个语言就出 N 块，可能有多个空块 | 用户明确要「保留空块」，接受 |

---

## 10. 实施步骤（评审通过后）

1. 开分支 `feat/language-block-layout`（核心仓，禁直推 main）。
2. 按 §5.1→§5.6 顺序改：parse（byPage）→ ingest（透传）→ render（groupByPage/空块/all）→ app（Requested 回显 + 接线）→ cmd（填空）。
3. 补 §7 测试，`go test ./...` 与 `contract` 测试全绿。
4. 用 `TRENDING_DIGEST_CORE` 指向本地构建的核心，插件侧跑一次真实预览，肉眼比对分块/metadata。
5. `docs/architecture.md` 专题方案索引表加一行；本文状态改「已实施」。
6. 插件仓单独 PR：`set-core-version` 升版本 + 插件侧冒烟。

---

## 11. 实施记录（2026-11-03）

分支 `feat/language-block-layout`。实际落地与 §5 的对应：

| 设计项 | 落地位置 | 说明 |
|---|---|---|
| 产出 byPage（§5.1） | `internal/parse/parse.go` `ParseAll` | 返回值改为 `([]RepoSnapshot, map[string][]RepoSnapshot, ParseReport, error)`；`byPage[pageLang]` 保留页序、不做跨页去重；扁平 `repos` 仍走 `Merge`（wire 不变）。 |
| 透传（§5.2） | `internal/ingest/ingest.go` `Result.ByPage` | `Ingest` 透传；`fallback`（search API）保持 `ByPage=nil`。 |
| 按页分块（§5.3） | `internal/render/markdown.go` | 新增 `buildSections` / `sortByRank` 替换旧的直接分组；`MarkdownWithBanners(d, byPage, banners)`；`buildSections` 有 byPage 时按 `d.Languages` 逐页出一块（含空块），nil 时回退 `groupByLanguage`。`render.JSON` 未改。 |
| meta 回显（§5.4/5.5） | `internal/app/run.go` `digestLanguages` / `digestByPage` | `Languages` 优先回显 `in.Requested`，空则回退 `languagesOf`；`digestByPage` 把快照视图映射到带摘要的 `RepoDigest`。 |
| 接线（§5.6） | `cmd/trendradar/run.go` | ingest 后填 `in.Requested = cfg.Trending.Languages`、`in.ByPage = res.ByPage`；`--input` 重放分支不填，走旧布局。 |

**验证**：
- `go vet ./...` 干净；`go test ./...` 全绿；`contract` 22/22 通过（schema 未动）。
- 新增测试：`TestMarkdownSectionsFollowRequestedPages`（分块跟随请求页、all 不拆分、空块保留、meta 回显）、`TestMarkdownAllowsCrossPageDuplication`（跨页重复）、`TestMarkdownNilByPageFallsBackToRepoLanguage`（nil 回退）、app 侧 `TestDigestLanguages*` / `TestDigestByPageCarriesSummaries`。
- **真实抓取端到端**：以 `languages=[all, python, rust]` 实跑，产出 `languages: [all, python, rust]`、`## all`(15)/`## python`(13)/`## rust`(11) 三块，C++/Shell/C 仓库（Hyprland、mattpocock/skills、colibri）全部并入 `## all`，不再出现 `## c++/## shell/## c`；D4Vinci/Scrapling 同时出现在 `## all` 与 `## python`（跨页重复符合预期）。

**尚未做**：版本号档位（发布时定）、合并 PR、插件仓 `set-core-version` 联动。

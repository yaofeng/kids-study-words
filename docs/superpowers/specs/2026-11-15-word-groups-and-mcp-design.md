# 少儿背单词 — 生词分组、回忆范围与 MCP 访问设计

日期：2026-11-15
状态：待确认

## 1. 背景与意图

现在一个生词本里的词只能整体回忆。家长想按课本单元、主题或临时圈定的范围练，孩子也需要在开始前清楚这一轮会练哪些词。这个设计增加「生词本内的分组」，并让回忆在开始时可以选择范围。

同时，家长希望外部 Agent 能帮忙管理和分析：管理生词本、分组和单词，并读取原始回忆记录，由 Agent 自己完成学习成效分析。系统只提供数据查询，不做内置分析。

两件事共用同一份分组数据，因此放在一份设计里，分阶段实施。

## 2. 目标与非目标

### 目标

- 在生词本详情页管理分组：新建、改名、删除、勾选本内单词。
- 一个词可以同时属于多个分组；分组不跨生词本。
- 回忆开始前选择范围：全部、低于指定星级、一个或多个分组。
- 范围在一轮内保持不变；「下一个」只从范围内抽词。
- 小结页「再来一轮」沿用上一轮范围，「换个范围」回到范围选择页。
- 导出/导入带上分组，旧版 v1 文件仍可导入。
- 设置页生成一个 MCP 访问令牌，每个账号只能有一个，随机生成，可重新生成或撤销。
- MCP 通过同一 HTTP 服务的 `/mcp` 路径提供 Streamable HTTP；使用 Bearer 令牌鉴权。
- MCP 工具覆盖：生词本（查、建、改名）、单词（查、加、移出）、分组（查、建、改名、删、加词、移词）、回忆记录查询。
- 回忆记录查询支持时间范围、单词、生词本、分组、熟练度、认识/不认识、复习次数和分页。
- 所有 MCP 操作只作用于令牌对应的账号。

### 非目标

- 不做跨生词本的分组。
- 不通过 MCP 删除生词本；删除仍在网页端输入密码完成。
- 不内置学习分析算法、总览、薄弱词榜或对比报表；只提供原始记录。
- 不实现 OAuth 2.1 授权服务器；使用个人访问令牌。
- 不做令牌分权（只读/读写）；一个令牌拥有该账号的完整 MCP 能力。
- 不改变现有回忆抽词的加权随机算法，只是把候选词集合换成范围内词。

### 反目标

- 不因为分组或 MCP 破坏现有导出/导入、多账号隔离和单容器部署。
- 不把令牌明文写入数据库、日志、导出文件或 URL。
- 不让 MCP 绕过账号归属校验。

## 3. 用户与场景

- **按课本单元练**：家长在「动物」生词本里建「Unit 3」「Unit 4」两个分组，各勾一部分词；孩子回忆时只选「Unit 3」。
- **临时范围**：考前家长建一个「易错词」分组，把几个词放进去，孩子只练这一组。
- **弱项回顾**：家长让 Agent 查询「最近 30 天、熟练度低于 3 星、回答过不认识的记录」，Agent 自己总结并给出复习建议。
- **批量整理**：家长让 Agent 把一组词加入某个分组，或从生词本里移出已掌握的词。

## 4. 约束

- Go 1.26，SQLite（`modernc.org/sqlite`），单二进制、单容器。
- 前端是服务端模板 + htmx + Tailwind，不引入前端框架。
- 回忆轮次状态保存在服务端内存（`internal/recall`），进程重启会结束当前一轮。
- 时间按配置的 `TZ` 分天；数据库时间统一存 UTC。
- MCP SDK：`github.com/modelcontextprotocol/go-sdk` v1.8.x（`mcp`、`auth` 包）。
- 现有 `/mcp` 路径必须避开 Cookie 会话与 CSRF 中间件，改由 Bearer 令牌鉴权。
- 现有部署方式和端口不变；反向代理需要额外放行 `/mcp`。

## 5. 方案选择

### 5.1 分组存储：独立关联表

选择：

```sql
word_groups(id, notebook_id, name, created_at)
word_group_members(group_id, word_id)
```

- 一个词多行成员关系，天然支持「一个词属于多个组」。
- 外键级联：删词、删本、删组都自动清理成员关系。
- 查询、计数、导出都直接走 SQL。

放弃的方案：

- 在 `words` 上加 `group_id`：无法表达多组。
- 在 `words` 上存 JSON 数组：无法用外键保证词和组属于同一生词本；计数和导出更麻烦。

### 5.2 回忆范围：本轮存条件，每次抽词重新取集合

选择：`recall.State` 里存 `store.RecallScope`（全部 / 熟练度上限 / 组 ID 列表），每次抽词按条件查询当前候选词。

- 与现有实现一致（现在每次 `next` 都重新读取本内单词）。
- 中途加词、删词、改组会自然反映到后续抽词。
- 内存占用小。

放弃的方案：开始时把词 ID 快照进本轮。范围更确定，但已删除词要额外处理，中途新增词也不可见，和现有行为不一致。

### 5.3 范围选择页：独立一步

选择：点「开始回忆」先进入 `/recall/{id}/start`，默认选中「全部」，选好再点「开始回忆」。生词本详情页和回忆选本页的按钮都指向这里。

放弃的方案：在回忆页顶部放范围面板、或「一键全部 + 按范围」。前者让回忆页多一个状态；后者容易让孩子永远只用默认。

### 5.4 MCP 传输与挂载

选择：同一 HTTP 服务，路径 `/mcp`，MCP Streamable HTTP，`Stateless: true`、`JSONResponse: true`。

- 不新增端口和容器，Docker 部署不变。
- Stateless 是新版协议（2026-07-28）的要求，也避免反向代理缓冲长连接。
- `JSONResponse` 让单消息 POST 直接返回 JSON，减少代理对流式响应的兼容问题。
- 设置 `DisableLocalhostProtection: true`：容器/反代常以 localhost 连接但带外部 Host 头，默认的 DNS 重绑定检查会误拒；真正的保护是 Bearer 令牌。

放弃的方案：另开端口（部署复杂）、stdio（无法从外部 Agent 访问）、有状态会话（重启丢会话，代理兼容性差）。

### 5.5 MCP 令牌：哈希存储、只显示一次

选择：

- 生成 32 字节随机串，加 `mcp_` 前缀，Base64URL 编码。
- 数据库只存 SHA-256 哈希；设置页生成后显示一次明文，可复制。
- 每个账号一行（`user_id` 主键）；重新生成即覆盖旧行，旧令牌立即失效；撤销即删除行。
- 修改密码和 CLI 重置密码都在同一事务里撤销 MCP 令牌。

放弃的方案：存明文以便随时查看（泄露面更大）、允许多个令牌（用户明确要求只能有一个）。

### 5.6 分析：只提供原始记录查询

选择：只提供一个 `query_recall_logs` 工具，返回原始回忆记录和筛选条件，分析逻辑交给 Agent。

放弃的方案：内置 `learning_summary`、`weak_words`、`performance_breakdown` 等工具。用户明确要求系统只提供原始记录。

## 6. 数据模型

在 `internal/store/schema.sql` 增加：

```sql
CREATE TABLE IF NOT EXISTS word_groups (
  id          INTEGER PRIMARY KEY,
  notebook_id INTEGER NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  UNIQUE (notebook_id, name COLLATE NOCASE)
);

CREATE INDEX IF NOT EXISTS word_groups_notebook
  ON word_groups(notebook_id, name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS word_group_members (
  group_id INTEGER NOT NULL REFERENCES word_groups(id) ON DELETE CASCADE,
  word_id  INTEGER NOT NULL REFERENCES words(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, word_id)
);

CREATE INDEX IF NOT EXISTS word_group_members_word
  ON word_group_members(word_id);

CREATE TABLE IF NOT EXISTS mcp_tokens (
  user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  last_used_at TEXT
);
```

说明：

- `schema.sql` 全部是 `CREATE TABLE IF NOT EXISTS`，现有数据库启动时自动补齐新表，无需迁移脚本。
- 分组成员必须与分组属于同一生词本，SQLite 外键无法表达跨表约束，由 store 层校验。
- `mcp_tokens` 每用户最多一行，天然满足「只能有一个」。
- MCP 令牌不进入导出文件。

### 6.1 RecallScope

在 `internal/store`（或 `internal/recall`，需避免循环依赖；`recall` 已依赖 `store`，因此放 `store`）定义：

```go
type RecallScopeKind int

const (
    ScopeAll RecallScopeKind = iota
    ScopeProficiency
    ScopeGroups
)

type RecallScope struct {
    Kind           RecallScopeKind
    MaxProficiency int     // ScopeProficiency：包含 proficiency < MaxProficiency 的词
    GroupIDs       []int64 // ScopeGroups：并集
}
```

- `MaxProficiency` 取值范围 2–10，对应界面「低于 1 星」到「低于 5 星」。
- `GroupIDs` 去重、排序后使用；空列表视为非法范围。
- `ScopeAll` 忽略其他字段。

## 7. 分组管理

### 7.1 数据访问（`internal/store/groups.go`）

```go
type WordGroup struct {
    ID         int64
    NotebookID int64
    Name       string
    CreatedAt  time.Time
    WordCount  int
}

func (s *Store) ListWordGroups(ctx, notebookID) ([]WordGroup, error)
func (s *Store) WordGroupByID(ctx, userID, groupID) (WordGroup, error) // JOIN notebooks 校验归属
func (s *Store) CreateWordGroup(ctx, notebookID, name) (WordGroup, error)
func (s *Store) RenameWordGroup(ctx, userID, groupID, name) (WordGroup, error)
func (s *Store) DeleteWordGroup(ctx, userID, groupID) error
func (s *Store) GroupWordIDs(ctx, userID, groupID) ([]int64, error)
func (s *Store) SetGroupWords(ctx, userID, groupID, wordIDs []int64) error
func (s *Store) GroupMemberships(ctx, notebookID) (map[int64][]WordGroup, error)
```

- `ListWordGroups` 按名称排序，附带 `COUNT(members)`。
- `CreateWordGroup` / `RenameWordGroup` 对 `(notebook_id, name COLLATE NOCASE)` 去重，重复返回 `ErrDuplicate`。
- `SetGroupWords`：
  - 校验分组属于当前用户；
  - 校验所有 `word_id` 都属于该分组的生词本；
  - 在一个事务里删除不在新集合里的成员、插入新增成员；
  - 空集合允许，表示清空分组。
- `GroupMemberships` 返回 `word_id -> []WordGroup`，用于生词本页词卡上的分组标记。
- 删词、删本、删组都依赖外键级联，不需要应用层清理。

### 7.2 生词本详情页

`handleNotebookDetail` 额外加载：

- `Groups []store.WordGroup`
- 每个词的 `Groups []store.WordGroup`（用视图包装 `store.Word`，避免模板里做复杂索引）

页面在排序标签下方加「分组」区域：

```
分组                                    [+ 新建]
[动物 5] [食物 3] [易错词 2]
```

- 新建：一个内联表单，输入名称，1–40 个字符；提交到 `POST /notebooks/{id}/groups`。
- 每个分组是一个链接，进入组页面。
- 没有分组时显示「还没有分组」提示。
- 词卡在释义下方显示所属分组的小徽章（没有分组则不显示）。
- 分组区域即使生词本为空也可以使用。

### 7.3 组页面

路由：`GET /notebooks/{id}/groups/{groupID}`

页面内容：

- 返回生词本、分组名称、词数。
- 改名表单（可折叠）和删除按钮。
- 本生词本全部单词的列表，每行一个复选框；已在该组的词默认勾选。
- 「保存」按钮提交到 `POST /notebooks/{id}/groups/{groupID}/words`。
- 删除按钮使用 `data-confirm`：「删除分组「X」？单词不会被删除。」不需要密码。

保存逻辑：

- 收集所有 `word_id` 表单值；
- `SetGroupWords` 替换成员关系；
- 保存后回到组页面并显示 flash「分组已更新」。

### 7.4 路由与校验

```text
POST /notebooks/{id}/groups                         新建
POST /notebooks/{id}/groups/{groupID}/rename        改名
POST /notebooks/{id}/groups/{groupID}/delete        删除
GET  /notebooks/{id}/groups/{groupID}               组页面
POST /notebooks/{id}/groups/{groupID}/words         保存成员
```

- 所有路由先校验 `{id}` 是当前用户的生词本，再校验 `{groupID}` 属于该生词本。
- 名称规则：去首尾空白后 1–40 个字符；同一生词本内不区分大小写唯一。
- 重复名称、非法名称重新渲染当前页面并显示错误，不重定向。
- 删除分组后回到生词本详情页，flash「分组已删除」。

## 8. 回忆范围

### 8.1 范围页

路由：`GET /recall/{id}/start`（选择页）、`POST /recall/{id}/start`（开始本轮）。

页面结构：

```
练哪些词？

( • ) 全部                    20 个
(   ) 低于 [ 3 星 ▾ ] 的单词    8 个
(   ) 分组
      [ ] 动物 (5)   [ ] 食物 (3)   [ ] 易错词 (2)

                [ 开始回忆 ]
```

- 「全部」默认选中，显示生词本总词数。
- 熟练度下拉选项为 2–10 级，显示为「低于 1 星」「低于 1.5 星」……「低于 5 星」，每项后面显示符合条件的词数。切换到熟练度时默认选中第 6 级（低于 3 星）。
- 分组选项列出该本全部分组及词数；没有分组时该项禁用并提示「还没有分组」。
- 计数在服务端一次性算出：读取本内全部单词，按 `proficiency` 统计各阈值；分组计数来自 `ListWordGroups`。
- 生词本没有单词时显示空状态，链接去查词。
- 表单带 CSRF。

### 8.2 开始一轮

`POST /recall/{id}/start`：

1. 解析表单 `scope`（`all` / `proficiency` / `groups`）。
2. 熟练度：解析 `max`，必须在 2–10。
3. 分组：解析重复的 `group_id` 表单值，去重；所有分组必须属于该生词本。
4. 用 `ListRecallWords` 计算候选词；为空则重新渲染范围页并提示「这个范围里还没有单词」。
5. `recall.Start(userID, notebookID, scope)` 开始新一轮（清空计数、重新计时）。
6. 重定向到 `GET /recall/{id}`。

### 8.3 轮次状态

`internal/recall.State` 增加：

```go
Scope store.RecallScope
```

`Manager` 增加：

```go
func (m *Manager) Start(userID, notebookID int64, scope store.RecallScope) State
```

- `Start` 总是创建新轮次。
- 现有 `Begin(userID, notebookID)` 保持签名：若同一生词本已有活跃轮次则沿用；否则以 `ScopeAll` 开始。
- `Snapshot`、`SetLastWord`、`Answer`、`UndoAnswer`、`Reset` 行为不变。
- `Answer` 仍按 `NotebookID` 校验，范围不需要参与。

### 8.4 抽词

`internal/store/recall.go` 或新文件增加：

```go
func (s *Store) ListRecallWords(ctx, notebookID, scope RecallScope) ([]Word, error)
```

- `ScopeAll`：本内全部词。
- `ScopeProficiency`：`proficiency < MaxProficiency`。
- `ScopeGroups`：`word_group_members` 与 `words` 联接，`group_id IN (...)`，`DISTINCT`。
- 结果仍交给现有 `recall.Pick` 做加权随机；抽词算法不变。

页面、答题、下一个：

- `GET /recall/{id}`：读取活跃轮次；没有则以 `ScopeAll` 开始。按 `State.Scope` 取候选词并抽一个。
- `POST /recall/{id}/next`：按 `State.Scope` 取候选词，排除上一张。
- `POST /recall/{id}/answer`：不变，只更新熟练度和记录。
- 范围内没有词时，卡片显示「这个范围里还没有单词」和「换个范围」链接。

### 8.5 范围在页面上的显示

- 回忆页标题下方显示「本轮范围：全部」/「本轮范围：低于 3 星」/「本轮范围：动物、食物」。
- 分组名从 `ListWordGroups` 映射；超过 3 个时显示「A、B 等 N 组」。
- `recallView` 增加 `ScopeLabel string`。

### 8.6 小结与再来一轮

- `POST /recall/{id}/end`：快照后生成小结链接，范围编码进查询参数：

```text
/recall/{id}/summary?known=3&unknown=1&scope=groups&group_id=1&group_id=2
```

- 小结页显示本轮范围，并提供：
  - 「再来一轮」→ `/recall/{id}/restart?scope=...`
  - 「换个范围」→ `/recall/{id}/start?scope=...`
- `GET /recall/{id}/restart`：按参数重建范围；范围为空则回到范围页并提示；否则 `Start` 并重定向到回忆页。
- `GET /recall/{id}/start` 支持同样的查询参数，用于预选范围。
- 范围编码规则：
  - `scope=all`
  - `scope=proficiency&max=6`
  - `scope=groups&group_id=1&group_id=2`

### 8.7 入口调整

- 生词本详情页的「开始回忆」链接改为 `/recall/{id}/start`。
- 回忆选本页的「开始回忆」链接改为 `/recall/{id}/start`。
- 直接访问 `/recall/{id}` 仍然可用，默认按「全部」开始，保持旧链接和旧测试兼容。

## 9. 导出与导入

### 9.1 格式版本

- `exportVersion` 从 1 升到 2。
- `ValidateExport` 接受版本 1 和 2；其他版本仍拒绝。
- 版本 1 文件没有 `groups` 字段，导入时按空分组处理。
- 版本 2 的 `ExportNotebook` 增加：

```go
type ExportNotebook struct {
    Name      string        `json:"name"`
    IsDefault bool          `json:"is_default"`
    Words     []ExportWord  `json:"words"`
    Groups    []ExportGroup `json:"groups"`
}

type ExportGroup struct {
    Name  string   `json:"name"`
    Words []string `json:"words"`
}
```

分组里的词用词名（不区分大小写），不使用内部 ID，保证跨设备可导入。

### 9.2 导出

- `ExportAll`：每个生词本带上它的分组和成员词名，按名称排序。
- `ExportNotebook`：只带该生词本的分组。
- 已删除生词本留下的回忆记录仍按现有逻辑处理。
- MCP 令牌不导出。

### 9.3 导入校验

- 版本 2 的分组：
  - 组名去空白后 1–40 个字符；
  - 每个生词本最多 100 个分组；
  - 同一生词本内组名不区分大小写唯一；
  - 分组里的词名必须出现在该生词本的 `words` 数组里（不区分大小写），否则整体拒绝；
  - 同一分组内词名不重复。
- 版本 1 即使出现 `groups` 字段也忽略（保持旧文件兼容）。

### 9.4 导入语义

- **合并**：
  - 按生词本名称匹配；
  - 分组按名称匹配，没有就新建；
  - 分组成员取并集，不删除本地已有成员；
  - 不删除文件里没有的本地分组。
- **覆盖**：
  - 清空现有生词本、单词、回忆记录（分组随生词本级联删除）；
  - 按文件重建生词本、单词、分组和成员关系。
- `ImportStats` 增加 `GroupsCreated`、`GroupMembershipsAdded`。
- `importSummary` 增加「新建 X 个分组」「恢复 Y 个分组词」。

## 10. MCP 访问

### 10.1 令牌生成与存储

- 新文件 `internal/store/mcp_tokens.go`：

```go
type MCPToken struct {
    UserID     int64
    CreatedAt  time.Time
    LastUsedAt *time.Time
}

func (s *Store) MCPTokenByUser(ctx, userID) (MCPToken, error) // ErrNotFound 表示没有
func (s *Store) ReplaceMCPToken(ctx, userID, hash string) (MCPToken, error)
func (s *Store) RevokeMCPToken(ctx, userID) error
func (s *Store) UserByMCPToken(ctx, hash string) (User, error)
func (s *Store) TouchMCPToken(ctx, userID int64, at time.Time) error // 节流更新
```

- `internal/auth` 增加：

```go
func NewPrefixedToken(prefix string, n int) (raw, hash string, err error)
```

- 令牌格式：`mcp_` + 32 字节随机数的 Base64URL（无填充），约 47 字符。
- 数据库存 `auth.HashToken(raw)`（SHA-256 hex）。
- `ReplaceMCPToken` 用 `INSERT ... ON CONFLICT(user_id) DO UPDATE` 覆盖，保证每用户一行。
- `UserByMCPToken` 联表 `users`，返回账号；找不到返回 `ErrNotFound`。
- `TouchMCPToken` 只在 `last_used_at` 为空或早于 5 分钟前时更新，避免每次 MCP 请求都写库。
- `UpdatePassword` 改为事务：更新密码 + `DELETE FROM mcp_tokens WHERE user_id = ?`。这样网页改密码和 CLI `reset-password` 都会撤销 MCP 令牌。

### 10.2 设置页

新增「MCP 访问」区块：

- 没有令牌：
  - 说明「用访问令牌让支持 MCP 的 Agent 管理生词本、分组和单词，并读取原始回忆记录。」
  - 表单 `POST /settings/mcp-token/generate`，按钮「生成访问令牌」。
- 已有令牌：
  - 显示「已生成 · 创建于 YYYY-MM-DD」，若有用过再显示「最近使用 YYYY-MM-DD HH:MM」。
  - 按钮「重新生成」（确认弹窗：旧令牌会立即失效）和「撤销令牌」。
  - 不显示令牌明文。
- 生成成功后直接渲染设置页（不重定向），显示：
  - 一行只读令牌文本；
  - 「复制」按钮（`navigator.clipboard`，失败时提示手动复制）；
  - 醒目提示「令牌只显示这一次，请现在复制；如果泄露，请立即重新生成。」
- 生成响应的 `Cache-Control: no-store`；设置页也设置 `no-store`，避免浏览器缓存明文。
- 路由：
  - `POST /settings/mcp-token/generate`
  - `POST /settings/mcp-token/revoke`
- 两个路由都校验 CSRF。
- 撤销后 flash「MCP 令牌已撤销」。

### 10.3 路由与中间件

- `web.New` 内构造 MCP handler（`internal/mcpserver.NewHandler`），存入 `Server.mcp`，在 `routes` 中挂载：

```go
mux.Handle("/mcp", s.mcp)
```

- `/mcp` 视为公开路径：
  - `publicPath("/mcp") == true`，跳过 Cookie 会话与首次启动检查；
  - `csrfMiddleware` 对 `/mcp` 直接放行，因为 MCP 不使用 Cookie，鉴权由 Bearer 令牌完成；
  - 日志、恢复、安全头中间件仍然生效。
- MCP handler 外层使用 `github.com/modelcontextprotocol/go-sdk/auth.RequireBearerToken`：
  - verifier 用 `store.UserByMCPToken` 查账号；
  - 成功返回 `TokenInfo{UserID: 数字ID字符串, Extra: {"user_id": 数字ID}}`；
  - `AllowMissingExpiration: true`（令牌不过期，只能撤销/重新生成）；
  - 找不到令牌返回 `auth.ErrInvalidToken`，数据库错误原样返回（500）。
- 每次成功鉴权后异步/节流更新 `last_used_at`；失败只记日志，不影响请求。
- 不记录 `Authorization` 头或令牌明文。

### 10.4 MCP 服务器

`internal/mcpserver` 包：

```go
type Config struct {
    Store    *store.Store
    Dict     Dictionary // Lookup + Examples 两个方法
    Location *time.Location
    Version  string
    Now      func() time.Time
}

func NewHandler(cfg Config) http.Handler
```

- `NewHandler` 组合 Bearer 中间件和 `mcp.NewStreamableHTTPHandler`。
- 使用 stateless 模式；`getServer` 从 `auth.TokenInfoFromContext(r.Context())` 取用户 ID，返回该用户专属的 `*mcp.Server`，并用 `sync.Map` 缓存。
- 每个用户的 server 在注册工具时闭包捕获 `userID`，工具内部不再接受 `user_id` 参数，也无法访问其他账号数据。
- Server：

```go
mcp.NewServer(&mcp.Implementation{
    Name: "study-words", Version: cfg.Version,
}, &mcp.ServerOptions{
    Instructions: "所有操作都作用于当前访问令牌对应的账号。时间按服务器时区。需要学习分析时，请用 query_recall_logs 获取原始记录后自行计算。",
})
```

- 工具用 `mcp.AddTool[In, Out]` 注册；输入输出结构体带 `json` 和 `jsonschema` 标签。
- 工具错误用中文 `error` 返回；MCP 客户端会看到 `isError` 和错误文本。内部数据库错误包装成用户能理解的提示，服务端记录原始错误。

### 10.5 工具清单

所有工具都只操作当前账号的数据。

#### 生词本

| 工具 | 输入 | 输出 |
| --- | --- | --- |
| `list_notebooks` | 无 | `{notebooks: [{id, name, is_default, word_count, created_at, updated_at}]}` |
| `create_notebook` | `{name}` | `{notebook}` |
| `rename_notebook` | `{notebook_id, name}` | `{notebook}` |

- 名称 1–40 个字符，同一账号内不区分大小写唯一。
- 不提供 `delete_notebook`。

#### 单词

| 工具 | 输入 | 输出 |
| --- | --- | --- |
| `list_words` | `{notebook_id, group_id?, sort?, limit?, offset?}` | `{words, total, has_more, next_offset}` |
| `add_word` | `{notebook_id, word}` | `{word}` |
| `remove_word` | `{notebook_id, word}` | `{removed, word, groups}` |

`list_words`：

- `sort`：`added`（默认）、`proficiency`、`reviewed`。
- `group_id` 可选，只返回该组内的词；必须属于同一生词本。
- `limit` 默认 200，最大 500；`offset` 默认 0。
- 每个词包含：`id`、`notebook_id`、`word`、`phonetic`、`translation`、`definition`、`examples`、`proficiency`、`added_at`、`last_reviewed_at`、`review_count`、`groups: [{id, name}]`。

`add_word`：

- 先校验生词本归属。
- 用离线词典 `Lookup` 查词；词典没有则返回错误（保持「只能加入词典里的词」的现有约束）。
- 音标、释义、英文释义取自词典。
- 例句优先用 AI 缓存（`store.CachedExamples`），否则取本地例句（`dict.Examples`，最多 2 条），与网页「加入生词本」行为一致。
- 重复词返回错误「「X」已在这个生词本里」。
- 新词熟练度为默认 5。

`remove_word`：

- 按词名在生词本内不区分大小写查找；找不到返回错误。
- 删除词并级联删除分组成员关系；回忆记录保留（现有行为）。
- 返回被删除的词名和它当时所在的分组。

#### 分组

| 工具 | 输入 | 输出 |
| --- | --- | --- |
| `list_groups` | `{notebook_id}` | `{groups: [{id, notebook_id, name, word_count, created_at}]}` |
| `get_group` | `{group_id, limit?, offset?}` | `{group, words, total, has_more, next_offset}` |
| `create_group` | `{notebook_id, name}` | `{group}` |
| `rename_group` | `{group_id, name}` | `{group}` |
| `delete_group` | `{group_id}` | `{deleted, name}` |
| `add_group_words` | `{group_id, words: [词名]}` | `{added, already_present, not_found}` |
| `remove_group_words` | `{group_id, words: [词名]}` | `{removed, not_present, not_found}` |

- 分组名 1–40 个字符，同一生词本内唯一。
- `get_group` 的 `limit` 默认 200，最大 500。
- `add_group_words` / `remove_group_words` 每次最多 200 个词名。
- 词名在分组所属生词本内不区分大小写解析；不存在于该生词本的词进入 `not_found`，不报整体失败。
- 删除分组不删除单词。

#### 回忆记录查询

| 工具 | 输入 | 输出 |
| --- | --- | --- |
| `query_recall_logs` | 见下 | `{logs, total, returned, has_more, next_offset}` |

输入：

```go
type QueryRecallLogsInput struct {
    From              string `json:"from,omitempty"`               // YYYY-MM-DD 或 RFC3339
    To                string `json:"to,omitempty"`                 // YYYY-MM-DD 或 RFC3339
    Word              string `json:"word,omitempty"`               // 精确匹配，不区分大小写
    NotebookID        int64  `json:"notebook_id,omitempty"`
    GroupID           int64  `json:"group_id,omitempty"`
    Known             *bool  `json:"known,omitempty"`              // 省略表示认识/不认识都要
    ProficiencyMin    int    `json:"proficiency_min,omitempty"`    // 答后熟练度，1–10
    ProficiencyMax    int    `json:"proficiency_max,omitempty"`
    ReviewCountMin    int    `json:"review_count_min,omitempty"`
    ReviewCountMax    int    `json:"review_count_max,omitempty"`
    Order             string `json:"order,omitempty"`              // "desc"（默认）或 "asc"
    Limit             int    `json:"limit,omitempty"`               // 默认 200，最大 1000
    Offset            int    `json:"offset,omitempty"`
}
```

每条记录：

```go
type RecallLogRow struct {
    ID               int64     `json:"id"`
    ReviewedAt       time.Time `json:"reviewed_at"`
    Word             string    `json:"word"`
    WordID           *int64    `json:"word_id"`            // 词已删除时为 null
    NotebookID       *int64    `json:"notebook_id"`        // 本已删除时为 null
    NotebookName     string    `json:"notebook_name"`      // 快照
    Known            bool      `json:"known"`
    ProficiencyAfter int       `json:"proficiency_after"`
    ReviewCount      int       `json:"review_count"`
}
```

语义：

- `from`、`to` 可选；不传表示不限。`YYYY-MM-DD` 按 `TZ` 解释：`from` 取当天 00:00，`to` 包含当天整天；RFC3339 按绝对时间解析。
- `word` 精确匹配 `recall_logs.word`（不区分大小写），能查到已删除词的快照。
- `notebook_id` 过滤 `recall_logs.notebook_id`；生词本删除后记录仍保留，但 `notebook_id` 为 null，因此不会再被本过滤命中。
- `group_id` 通过当前 `word_group_members` 联表过滤；已删除的词不会命中分组过滤。分组必须属于当前账号。
- `known` 省略表示两者都要。
- `proficiency_min/max` 过滤答后熟练度 `proficiency_after`，闭区间。
- `review_count_min/max` 过滤累计回忆次数，闭区间。
- `order` 按 `reviewed_at` 排序，相同时间按 `id` 稳定排序；默认倒序。
- 分页用 `limit`/`offset`；`total` 是当前筛选条件下的总条数；`has_more` 和 `next_offset` 便于继续翻页。
- 只读，不修改任何数据。

### 10.6 客户端配置示例

README 增加：

```json
{
  "mcpServers": {
    "study-words": {
      "type": "http",
      "url": "https://your-host/mcp",
      "headers": {
        "Authorization": "Bearer mcp_..."
      }
    }
  }
}
```

Claude Code 示例：

```bash
claude mcp add --transport http study-words https://your-host/mcp \
  --header "Authorization: Bearer mcp_..."
```

说明：

- 令牌是完整账号凭证，只通过 HTTPS 使用，不要提交到仓库或分享。
- 反向代理需要放行 `/mcp`，并允许 POST；不需要 Cookie。
- 若代理会缓冲响应，`JSONResponse` 已避免长连接；仍建议关闭对 `/mcp` 的响应缓冲。
- 重新生成令牌后，旧配置会立即 401，需要在客户端更新。

## 11. 界面与交互

### 11.1 生词本详情页

```text
← 所有生词本
动物  20 个单词                     [去查词] [开始回忆]
[加入时间] [熟练度] [最近回忆]

分组                                  [+ 新建]
[动物 5] [食物 3] [易错词 2]

apple  /ˈæpl/                       ★★★☆☆
n. 苹果                             动物 · 易错词
```

- 分组区域放在排序标签和词列表之间。
- 分组徽章使用现有 `badge` 样式。
- 移动端分组芯片自动换行。

### 11.2 组页面

```text
← 动物
Unit 3   8 个单词              [改名] [删除]
[✓] apple    n. 苹果          ★★★☆☆
[ ] pear     n. 梨            ★★☆☆☆
[✓] cat      n. 猫            ★★★★☆
                           [保存]
```

- 复选框列表可直接滚动；保存后停留本页。
- 改名表单可折叠，减少干扰。

### 11.3 范围页

见 8.1。移动端三个选项纵向排列，按钮全宽。

### 11.4 设置页 MCP 区块

```text
MCP 访问
用访问令牌让支持 MCP 的 Agent 管理生词本、分组和单词，并读取原始回忆记录。
已生成 · 创建于 2026-11-15 · 最近使用 2026-11-15 14:20
[重新生成] [撤销令牌]

（刚生成时）
令牌只显示这一次，请现在复制：
mcp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx  [复制]
```

## 12. 安全与隐私

- MCP 令牌是完整账号凭证：数据库只存哈希，日志和导出文件都不含明文。
- 生成响应和设置页使用 `Cache-Control: no-store`。
- 修改密码 / CLI 重置密码在同一事务里撤销 MCP 令牌。
- MCP 所有工具都按令牌对应的 `user_id` 过滤；分组、生词本、单词操作都做归属校验。
- 分组保存时校验每个词属于分组的生词本，防止跨本注入。
- `/mcp` 不使用 Cookie 会话，也不接受 Cookie 鉴权；只接受 `Authorization: Bearer`。
- 建议部署在 HTTPS 下；令牌泄露等同于账号泄露。
- 不记录请求头中的 `Authorization`；`loggingMiddleware` 只记录方法、路径、状态和耗时。
- 令牌为 256 位随机数，不需要额外登录限流；鉴权失败返回 401，可记日志。
- MCP 不提供删除生词本工具，破坏性最大的操作仍留在网页端密码确认。

## 13. 测试策略

### 13.1 store

- 分组：创建、列表、改名、删除；同本重名拒绝；不同本同名允许；名称大小写不敏感；词数统计。
- 成员：设置/清空成员；跨生词本的词 ID 拒绝；删词/删本/删组级联清理。
- `GroupMemberships` 返回正确映射。
- `ListRecallWords`：全部、熟练度阈值、单组、多组并集、空结果。
- MCP 令牌：每用户一行、替换、撤销、按哈希查用户、密码更新撤销令牌。
- 导入/导出：v2 分组往返；v1 兼容；合并并集；覆盖重建；非法分组名/重复组名/不存在的词拒绝。

### 13.2 recall manager

- `Start` 总是新轮次并保存范围。
- `Begin` 沿用同本活跃轮次；切本重开；没有活跃轮次时使用 `ScopeAll`。
- 答题、撤销、重置在带范围时行为不变。

### 13.3 web

- 生词本页显示分组区、词卡徽章、创建/删除/改名/勾选成员。
- 组页面保存后成员正确；空分组可保存。
- 范围页显示各选项计数；默认全部；无分组时禁用分组项。
- 按熟练度开始后只抽到符合条件的词（用单候选词做确定性断言）。
- 按分组开始后只抽到组内词；多组并集。
- 空范围会提示，不开始。
- 「下一个」保持在范围内；小结/再来一轮保留范围；「换个范围」回到范围页。
- 直接访问 `/recall/{id}` 仍按全部开始。
- 设置页生成令牌只显示一次；重新生成后旧令牌失效；撤销后 401。
- 修改密码撤销 MCP 令牌。
- `/mcp` 不带令牌返回 401；带有效令牌可用；无 Cookie/CSRF 也能 POST。

### 13.4 MCP

- 用 `httptest.NewServer` + SDK 客户端（自定义 `http.Client.Transport` 注入 `Authorization`）测试。
- 无令牌、错误令牌、撤销后的令牌 → 401。
- `list_notebooks` / `create_notebook` / `rename_notebook`。
- `list_words` 排序、分组过滤、分页、字段完整。
- `add_word`：词典命中、词典未命中、重复词、生词本不存在。
- `remove_word`：按词名大小写不敏感删除；删除后分组成员关系消失。
- 分组完整 CRUD；`add_group_words` / `remove_group_words` 的 added/already/not_found 分类。
- `query_recall_logs`：
  - 时间范围（日期和 RFC3339）；
  - word 精确匹配；
  - notebook、group；
  - known true/false/省略；
  - proficiency 闭区间；
  - review_count 闭区间；
  - asc/desc 排序；
  - limit/offset、total、has_more、next_offset；
  - 已删除词的记录仍可按 word 查到，但 `word_id` 为 null；
  - 分页不重复、不遗漏。
- 用户隔离：另一个用户的 token 看不到本用户数据（通过直接构造两个用户/令牌测试）。
- 分组归属：不能操作其他生词本的分组。

### 13.5 验证命令

```bash
gofmt -l .
go vet ./...
go test ./...
make css
```

`make css` 后提交更新后的 `web/static/app.css`。

## 14. 实施顺序

1. **分组数据层**：schema、`internal/store/groups.go`、单元测试。
2. **分组网页**：生词本页分组区、组页面、路由、模板、样式、测试。
3. **回忆范围**：`RecallScope`、`ListRecallWords`、manager `Start`、范围页、范围感知的回忆/小结/重来、测试。
4. **导出 v2**：分组导入导出、兼容 v1、统计、测试。
5. **MCP 令牌**：`mcp_tokens` 表、auth 生成函数、store 方法、设置页生成/撤销、密码修改联动、测试。
6. **MCP 服务器**：引入 SDK、`internal/mcpserver` 骨架、Bearer 鉴权、`/mcp` 路由与中间件豁免、测试。
7. **MCP 工具**：生词本、单词、分组、`query_recall_logs`，逐个工具测试。
8. **文档与收尾**：README MCP 章节、设置页文案、`make css`、全量测试。

分组（1–4）先完成并可用；MCP（5–7）依赖分组数据，随后实现。

## 15. 留给实现者的决定

- 分组区、组页面、范围页的具体 Tailwind 类名和响应式细节。
- 组页面是否增加「全选 / 清空」辅助按钮；不影响数据和接口。
- `query_recall_logs` 的 `total` 是否每次都用独立 COUNT 查询；个人规模下可以接受。
- MCP 工具输出是否额外附带一段人类可读文本；当前设计以结构化 JSON 为准。
- `mcpserver` 内部 helper 的命名和文件拆分。
- 是否在 README 之外补充一份 MCP 工具参考文档；设计只要求 README 有一个可用的接入示例。

## 16. 兼容与迁移

- 现有数据库启动时自动补建 `word_groups`、`word_group_members`、`mcp_tokens`，不改动已有表。
- 现有 v1 导出文件继续可导入；新导出版本为 2。
- 现有 `/recall/{id}` 链接继续可用，默认全部范围。
- 现有网页功能、账号隔离和 Docker 部署方式不变。
- MCP 默认不可用，直到在设置页生成令牌；没有令牌时 `/mcp` 返回 401。
- 版本号和镜像发布不在本设计内；发布时按现有流程修改 `.version`。

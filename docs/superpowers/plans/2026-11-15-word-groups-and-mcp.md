# 生词分组、回忆范围与 MCP 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在生词本内增加分组，让回忆开始前按全部 / 低于指定星级 / 多个分组选择范围；同时增加只依赖 Bearer 令牌的 MCP 端点，提供生词本、单词、分组管理和原始回忆记录查询。

**Architecture:** 分组用 `word_groups` + `word_group_members` 两张表表达多对多关系；回忆轮次在内存 `recall.State` 中保存 `store.RecallScope`，每次抽词按条件重新查询候选词。MCP 复用同一个 HTTP 服务，在 `/mcp` 挂载 stateless Streamable HTTP，用设置页生成的 `mcp_` 令牌鉴权，每个令牌绑定一个账号。

**Tech Stack:** Go 1.26、`modernc.org/sqlite`、`html/template` + htmx + Tailwind 3.4.17、`github.com/modelcontextprotocol/go-sdk` v1.8.0（`mcp`、`auth` 包）。

**Spec:** `docs/superpowers/specs/2026-11-15-word-groups-and-mcp-design.md`

## Global Constraints

- Go 1.26.0；SQLite 使用 `modernc.org/sqlite`；不新增端口、容器或前端框架。
- MCP SDK 固定为 `github.com/modelcontextprotocol/go-sdk v1.8.0`；使用 `mcp` 与 `auth` 包。
- `schema.sql` 只用 `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`，现有数据库启动时自动补表，不写迁移脚本。
- 时间统一以 UTC 存入 SQLite；按配置的 `TZ` 解释和展示。
- 分组名去首尾空白后 1–40 个字符；同一生词本内不区分大小写唯一；不同生词本可同名。
- 一个词可属于多个分组；分组不跨生词本；分组成员必须属于分组的生词本。
- 回忆范围在一轮内不变；`ScopeAll` 是零值；`MaxProficiency` 取值 2–10；`GroupIDs` 去重排序，空列表非法。
- 导出格式版本升为 2；`ValidateExport` 同时接受 1 和 2；v1 文件忽略 `groups` 字段。
- MCP 令牌格式为 `mcp_` + 32 字节随机数的无填充 Base64URL，总长约 47 字符；数据库只存 `auth.HashToken(raw)`（SHA-256 hex）；明文只显示一次，不写入日志、导出文件或 URL。
- 每个账号最多一个 MCP 令牌；重新生成覆盖旧令牌；修改密码 / CLI 重置密码在同一事务内撤销令牌。
- `/mcp` 是精确路径：不走 Cookie 会话、不走 CSRF、不走首次启动跳转；只接受 `Authorization: Bearer <token>`。
- MCP 工具只操作令牌对应账号的数据；不提供 `delete_notebook`；不提供内置分析工具。
- `list_words` limit 默认 200、最大 500；`get_group` 相同；`query_recall_logs` limit 默认 200、最大 1000；`add_group_words` / `remove_group_words` 每次最多 200 个词名。
- `TouchMCPToken` 只在 `last_used_at` 为空或早于 5 分钟前时更新。
- 现有 `/recall/{id}` 直接访问仍按「全部」开始；现有 v1 导入文件仍可用。
- UI 任务把新样式写进 `web/static/input.css`；`make css` 只在 Task 17 运行一次并提交 `web/static/app.css`。

## Review Focus

1. **当前轮次的分组被删除、或组内词被删空**：下一次抽词不能 500；卡片显示「这个范围里还没有单词」并给出「换个范围」入口。测试落在 Task 8（`TestRecallGroupDeletedMidRoundShowsEmptyScope`）。
2. **伪造或跨生词本的 `group_id`**（URL / 表单）：组页面必须 404；开始范围必须重新渲染并提示，不能把别的生词本的词混进来。测试落在 Task 4（`TestGroupPageRejectsForeignGroup`）和 Task 8（`TestRecallStartRejectsForeignGroup`）。
3. **导入 v2 时分组引用不存在的词、同本组名重复、超过 100 组**：整体拒绝，事务不写入任何数据。测试落在 Task 10（`TestImportRejectsUnknownGroupWordAndRollsBack`）。
4. **`query_recall_logs` 的 `YYYY-MM-DD` `to`**：必须包含本地当天整天；`from > to`、`limit > 1000`、负 `offset` 要有确定行为。测试落在 Task 16（`TestQueryRecallLogsDateOnlyToIncludesWholeDay`、`TestQueryRecallLogsClampsLimitAndOffset`）。
5. **重新生成或撤销 MCP 令牌后**：旧令牌的下一次请求立即 401（stateless，无会话残留）。测试落在 Task 12（`TestRegenerateInvalidatesOldToken`）和 Task 13（`TestMCPRejectsRevokedToken`）。

---

## File Structure

**新增 store 文件**

- `internal/store/groups.go`：分组 CRUD、成员关系、词卡分组映射。
- `internal/store/recall_scope.go`：`RecallScope` 类型与 `ListRecallWords`。
- `internal/store/mcp_tokens.go`：MCP 令牌的存取、按哈希查用户、节流更新。
- `internal/store/recall_query.go`：`RecallLogFilter` 与 `QueryRecallLogs`。

**新增 web 文件**

- `internal/web/group_handlers.go`：分组区域之外的所有分组路由与组页面数据。
- `internal/web/recall_scope.go`：范围解析、编码、标签、范围页。
- `internal/web/mcp_token_handlers.go`：设置页生成 / 撤销令牌。
- `web/templates/group.html`、`web/templates/recall_start.html`：新页面。

**新增 MCP 文件**

- `internal/mcpserver/server.go`：`Config`、`NewHandler`、Bearer 校验、每用户 server 缓存。
- `internal/mcpserver/types.go`：MCP 输入输出结构体与 store→MCP 的转换 helper。
- `internal/mcpserver/tools_notebooks.go`、`tools_words.go`、`tools_groups.go`、`tools_recall.go`：按领域注册工具。
- `internal/mcpserver/testhelp_test.go`：测试用 store、令牌、MCP 客户端、结果解码。

**修改**

- `internal/store/schema.sql`、`errors.go`、`export.go`、`import.go`、`users.go`、`words.go`、`notebooks_test.go`
- `internal/auth/auth.go`
- `internal/recall/manager.go`
- `internal/web/server.go`、`middleware.go`、`word_handlers.go`、`notebook_handlers.go`（如需要）、`recall_handlers.go`、`settings_account.go`、`settings_handlers.go`、`import_handlers.go`
- `web/templates/notebook.html`、`recall_select.html`、`recall.html`、`partials.html`、`summary.html`、`settings.html`
- `web/static/input.css`、`web/static/app.js`、`web/static/app.css`
- `README.md`、`go.mod`、`go.sum`

---

## Phase 1：分组、回忆范围、导出 v2

### Task 1: 分组表与分组 CRUD

**Files:**
- Modify: `internal/store/schema.sql`
- Create: `internal/store/groups.go`
- Create: `internal/store/groups_test.go`

**Interfaces:**
- Consumes: `rowQuerier`、`formatTime`、`scanTime`、`mapNoRows`、`ErrNotFound`、`ErrDuplicate`（均在 `internal/store` 已有）。
- Produces:
  ```go
  type WordGroup struct {
      ID         int64
      NotebookID int64
      Name       string
      CreatedAt  time.Time
      WordCount  int
  }
  func (s *Store) ListWordGroups(ctx context.Context, notebookID int64) ([]WordGroup, error)
  func (s *Store) WordGroupByID(ctx context.Context, userID, groupID int64) (WordGroup, error)
  func (s *Store) CreateWordGroup(ctx context.Context, notebookID int64, name string) (WordGroup, error)
  func (s *Store) RenameWordGroup(ctx context.Context, userID, groupID int64, name string) (WordGroup, error)
  func (s *Store) DeleteWordGroup(ctx context.Context, userID, groupID int64) error
  ```
- 行为：`CreateWordGroup` / `RenameWordGroup` 去首尾空白，空名返回普通 error；同本重名返回 `ErrDuplicate`；`WordGroupByID`、`RenameWordGroup`、`DeleteWordGroup` 对别的账号返回 `ErrNotFound`。

- [ ] **Step 1: Write the failing test**

```go
func TestWordGroupCRUD(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateNotebook(ctx, u.ID, "另一本")
	if err != nil {
		t.Fatal(err)
	}

	g, err := st.CreateWordGroup(ctx, nb.ID, " 动物 ")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID == 0 || g.NotebookID != nb.ID || g.Name != "动物" || g.CreatedAt.IsZero() || g.WordCount != 0 {
		t.Fatalf("group=%+v", g)
	}
	if _, err := st.CreateWordGroup(ctx, nb.ID, "动物"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := st.CreateWordGroup(ctx, other.ID, "动物"); err != nil {
		t.Fatalf("same name in another notebook should be allowed: %v", err)
	}

	groups, err := st.ListWordGroups(ctx, nb.ID)
	if err != nil || len(groups) != 1 || groups[0].Name != "动物" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	got, err := st.WordGroupByID(ctx, u.ID, g.ID)
	if err != nil || got.ID != g.ID || got.NotebookID != nb.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := st.WordGroupByID(ctx, u.ID+1, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign lookup: %v", err)
	}

	renamed, err := st.RenameWordGroup(ctx, u.ID, g.ID, "食物")
	if err != nil || renamed.Name != "食物" {
		t.Fatalf("renamed=%+v err=%v", renamed, err)
	}
	if _, err := st.RenameWordGroup(ctx, u.ID+1, g.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rename: %v", err)
	}
	if err := st.DeleteWordGroup(ctx, u.ID+1, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if err := st.DeleteWordGroup(ctx, u.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WordGroupByID(ctx, u.ID, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestWordGroupCascadesWithNotebook(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	nb, _ := st.CreateNotebook(ctx, u.ID, "临时本")
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	if err := st.DeleteNotebook(ctx, u.ID, nb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WordGroupByID(ctx, u.ID, g.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("group should cascade: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run 'TestWordGroup' -v`
Expected: FAIL，`st.CreateWordGroup` 等未定义。

- [ ] **Step 3: Implement schema and `internal/store/groups.go`**

在 `schema.sql` 末尾追加，SQL 与 spec §6 完全一致：

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
```

`ListWordGroups` 的 SQL（按名称、再按 id 排序；词数用相关子查询）：

```sql
SELECT g.id, g.notebook_id, g.name, g.created_at,
       (SELECT COUNT(*) FROM word_group_members m WHERE m.group_id = g.id)
FROM word_groups g
WHERE g.notebook_id = ?
ORDER BY g.name COLLATE NOCASE, g.id
```

`WordGroupByID` 必须 JOIN notebooks 校验账号：

```sql
SELECT g.id, g.notebook_id, g.name, g.created_at,
       (SELECT COUNT(*) FROM word_group_members m WHERE m.group_id = g.id)
FROM word_groups g
JOIN notebooks n ON n.id = g.notebook_id
WHERE g.id = ? AND n.user_id = ?
```

`CreateWordGroup`：`name = strings.TrimSpace(name)`，空名返回 `errors.New("group name is required")`；插入 `formatTime(TimeNow())`；`strings.Contains(strings.ToLower(err.Error()), "unique")` → `ErrDuplicate`。`RenameWordGroup` 先 `WordGroupByID` 校验归属，再 `UPDATE word_groups SET name = ? WHERE id = ?`，unique 错误映射 `ErrDuplicate`，成功后重新读取返回。`DeleteWordGroup` 先 `WordGroupByID`，再 `DELETE`，`RowsAffected()==0` → `ErrNotFound`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run 'TestWordGroup' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/store/schema.sql internal/store/groups.go internal/store/groups_test.go
git commit -m "feat(store): add word groups schema and CRUD"
```

---

### Task 2: 分组成员关系

**Files:**
- Modify: `internal/store/groups.go`
- Modify: `internal/store/groups_test.go`

**Interfaces:**
- Consumes: Task 1 的 `WordGroup`、`WordGroupByID`、`CreateWordGroup`。
- Produces:
  ```go
  func (s *Store) GroupWordIDs(ctx context.Context, userID, groupID int64) ([]int64, error)
  func (s *Store) SetGroupWords(ctx context.Context, userID, groupID int64, wordIDs []int64) error
  func (s *Store) GroupMemberships(ctx context.Context, notebookID int64) (map[int64][]WordGroup, error)
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestSetGroupWordsReplacesMembers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	cat, _ := st.AddWord(ctx, nb.ID, Word{Word: "cat"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")

	if err := st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID, apple.ID, apple.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err := st.GroupWordIDs(ctx, u.ID, g.ID)
	if err != nil || !slices.Equal(ids, []int64{apple.ID, pear.ID}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if err := st.SetGroupWords(ctx, u.ID, g.ID, []int64{cat.ID}); err != nil {
		t.Fatal(err)
	}
	ids, _ = st.GroupWordIDs(ctx, u.ID, g.ID)
	if !slices.Equal(ids, []int64{cat.ID}) {
		t.Fatalf("replace failed: %v", ids)
	}
	if err := st.SetGroupWords(ctx, u.ID, g.ID, nil); err != nil {
		t.Fatal(err)
	}
	ids, _ = st.GroupWordIDs(ctx, u.ID, g.ID)
	if len(ids) != 0 {
		t.Fatalf("clear failed: %v", ids)
	}
}

func TestSetGroupWordsRejectsForeignWord(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	other, _ := st.CreateNotebook(ctx, u.ID, "另一本")
	foreign, _ := st.AddWord(ctx, other.ID, Word{Word: "dog"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	if err := st.SetGroupWords(ctx, u.ID, g.ID, []int64{foreign.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign word: %v", err)
	}
}

func TestGroupMembershipsMapsWordsToGroups(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	animals, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	food, _ := st.CreateWordGroup(ctx, nb.ID, "食物")
	_ = st.SetGroupWords(ctx, u.ID, animals.ID, []int64{apple.ID, pear.ID})
	_ = st.SetGroupWords(ctx, u.ID, food.ID, []int64{apple.ID})

	m, err := st.GroupMemberships(ctx, nb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m[apple.ID]) != 2 || len(m[pear.ID]) != 1 {
		t.Fatalf("memberships=%+v", m)
	}
}

func TestGroupCascadesWithWordAndGroup(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})

	if err := st.DeleteWord(ctx, nb.ID, apple.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := st.GroupMemberships(ctx, nb.ID)
	if len(m) != 0 {
		t.Fatalf("word delete should cascade: %+v", m)
	}

	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID})
	if err := st.DeleteWordGroup(ctx, u.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	ids, _ := st.GroupWordIDs(ctx, u.ID, g.ID)
	if len(ids) != 0 {
		t.Fatalf("group delete should cascade: %v", ids)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store -run 'TestSetGroupWords|TestGroupMemberships|TestGroupCascades' -v`
Expected: FAIL，方法未定义。

- [ ] **Step 3: Implement membership methods**

`SetGroupWords` 的顺序很重要：`s.db` 只允许一个连接（`SetMaxOpenConns(1)`），所以先调用 `WordGroupByID`（拿到 `NotebookID` 并校验账号），**再** `BeginTx`；事务内不要调用 `s.db`。

1. `g, err := s.WordGroupByID(ctx, userID, groupID)`。
2. 去重排序 `ids`；若非空，事务内 `SELECT COUNT(*) FROM words WHERE notebook_id = ? AND id IN (...)`，数量不等于 `len(ids)` → `ErrNotFound`。
3. `DELETE FROM word_group_members WHERE group_id = ?`。
4. 对每个 id `INSERT OR IGNORE INTO word_group_members (group_id, word_id) VALUES (?, ?)`。
5. `Commit`。

`GroupWordIDs` 先 `WordGroupByID` 校验，再 `SELECT word_id FROM word_group_members WHERE group_id = ? ORDER BY word_id`，返回 `[]int64`。空结果返回空切片。

`GroupMemberships` 的 SQL：

```sql
SELECT m.word_id, g.id, g.notebook_id, g.name, g.created_at,
       (SELECT COUNT(*) FROM word_group_members m2 WHERE m2.group_id = g.id)
FROM word_group_members m
JOIN word_groups g ON g.id = m.group_id
WHERE g.notebook_id = ?
ORDER BY g.name COLLATE NOCASE, g.id
```

按 `word_id` 聚合为 `map[int64][]WordGroup`；没有成员的词不出现在 map 里。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run 'TestSetGroupWords|TestGroupMemberships|TestGroupCascades' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/store/groups.go internal/store/groups_test.go
git commit -m "feat(store): manage word group membership"
```

---

### Task 3: 生词本页分组区、创建分组、词卡徽章

**Files:**
- Modify: `internal/web/word_handlers.go`
- Create: `internal/web/group_handlers.go`
- Modify: `internal/web/server.go`
- Modify: `web/templates/notebook.html`
- Modify: `web/static/input.css`
- Create: `internal/web/group_handlers_test.go`

**Interfaces:**
- Consumes: `Store.ListWordGroups`、`Store.CreateWordGroup`、`Store.GroupMemberships`（Task 1–2）。
- Produces:
  ```go
  type notebookWord struct {
      store.Word
      Groups []store.WordGroup
  }
  type notebookDetailData struct {
      Base
      Notebook   store.Notebook
      Words      []notebookWord
      Groups     []store.WordGroup
      Sort       string
      GroupError string
  }
  func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request)
  func (s *Server) renderNotebookDetail(w http.ResponseWriter, r *http.Request, nb store.Notebook, sortKey, groupErr string)
  func validGroupName(name string) bool
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestNotebookPageShowsAndCreatesGroups(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	path := fmt.Sprintf("/notebooks/%d/groups", tc.notebook.ID)
	resp := tc.postForm(path, FormValues("name", "动物"))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	for _, want := range []string{"分组", "动物", "新建分组"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestCreateGroupDuplicateShowsError(t *testing.T) {
	tc := newAuthedTestClient(t)
	path := fmt.Sprintf("/notebooks/%d/groups", tc.notebook.ID)
	if resp := tc.postForm(path, FormValues("name", "动物")); resp.Code != http.StatusSeeOther {
		t.Fatalf("first code=%d", resp.Code)
	}
	resp := tc.postForm(path, FormValues("name", "动物"))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "已经有同名的分组了") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestNotebookPageShowsWordGroupBadges(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	g, err := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	if err := tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{w.ID}); err != nil {
		t.Fatal(err)
	}
	body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "word-groups") || !strings.Contains(body, "动物") {
		t.Fatalf("badge missing: %s", body)
	}
}

func TestCreateGroupRejectsForeignNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/notebooks/99999/groups", FormValues("name", "动物"))
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d", resp.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'TestNotebookPageShowsAndCreatesGroups|TestCreateGroupDuplicate|TestNotebookPageShowsWordGroupBadges|TestCreateGroupRejectsForeign' -v`
Expected: FAIL，路由 404 或字段不存在。

- [ ] **Step 3: Implement**

`word_handlers.go`：

- 把 `notebookDetailData.Words` 改为 `[]notebookWord`，新增 `Groups []store.WordGroup`、`GroupError string`。
- `handleNotebookDetail` 提取为 `renderNotebookDetail`，顺序：
  ```go
  sortKey = validSort(sortKey)
  words, _ := s.store.ListWords(ctx, nb.ID, sortKey)
  groups, _ := s.store.ListWordGroups(ctx, nb.ID)
  members, _ := s.store.GroupMemberships(ctx, nb.ID)
  out := make([]notebookWord, len(words))
  for i, w := range words { out[i] = notebookWord{Word: w, Groups: members[w.ID]} }
  base, _ := s.pageData(w, r, nb.Name, "notebooks")
  data := notebookDetailData{Base: base, Notebook: nb, Words: out, Groups: groups, Sort: sortKey, GroupError: groupErr}
  s.renderer.page(w, http.StatusOK, "notebook", data)
  ```
- 错误处理：`ListWords`/`ListWordGroups`/`GroupMemberships` 失败走 `s.fail`；`pageData` 失败直接返回。

`group_handlers.go`：

- `const maxGroupNameRunes = 40`
- `validGroupName(name string) bool`：`strings.TrimSpace` 后 `utf8.RuneCountInString` 为 1–40。
- `handleCreateGroup`：
  1. `nb, ok := s.loadNotebookForGroup(w, r)`（新增 helper：`pathID` + `NotebookByID`，`ErrNotFound` → 404）。
  2. `r.ParseForm()`；`name := strings.TrimSpace(r.PostForm.Get("name"))`；非法 → `renderNotebookDetail(w, r, nb, "added", "名称需为 1–40 个字符。")`。
  3. `CreateWordGroup(ctx, nb.ID, name)`；`ErrDuplicate` → `renderNotebookDetail(..., "已经有同名的分组了。")`。
  4. 成功：`setFlash(w, "分组已创建。")`，303 到 `/notebooks/{nb.ID}`。
- `loadNotebookForGroup` 与 `recall_handlers.go` 的 `loadRecallNotebook` 类似；为避免重复，可以在 `notebook_handlers.go` 新增通用的 `loadNotebook(w, r) (store.Notebook, bool)`，并让 `handleNotebookDetail` 与分组 handler 共用。二选一，但不要复制三份。

`server.go` 增加：

```go
mux.HandleFunc("POST /notebooks/{id}/groups", s.handleCreateGroup)
```

`notebook.html` 在 `.sort-tabs` 之后、`{{if .Words}}` 之前插入：

```html
<section class="group-section">
  <div class="group-head">
    <h2 class="group-title">分组</h2>
  </div>
  {{if .GroupError}}<p class="alert alert-error" role="alert">{{.GroupError}}</p>{{end}}
  <form method="post" action="/notebooks/{{.Notebook.ID}}/groups" class="group-create">
    {{template "csrf" .}}
    <input class="input" type="text" name="name" maxlength="40" placeholder="新分组名称" required>
    <button class="btn" type="submit">新建分组</button>
  </form>
  {{if .Groups}}
  <ul class="group-chips">
    {{range .Groups}}
    <li><a class="badge group-chip" href="/notebooks/{{$.Notebook.ID}}/groups/{{.ID}}">{{.Name}} <span class="group-count">{{.WordCount}}</span></a></li>
    {{end}}
  </ul>
  {{else}}
  <p class="muted-text">还没有分组。</p>
  {{end}}
</section>
```

词卡在 `{{FirstLine .Translation}}` 之后插入：

```html
{{if .Groups}}
<p class="word-groups">{{range .Groups}}<span class="badge">{{.Name}}</span>{{end}}</p>
{{end}}
```

`input.css` 增加（Task 17 再统一 `make css`）：

```css
.group-section { @apply mb-5 flex flex-col gap-3; }
.group-head { @apply flex items-center justify-between gap-2; }
.group-title { @apply text-lg font-extrabold; }
.group-create { @apply flex flex-col gap-2 sm:flex-row; }
.group-chips { @apply flex flex-wrap gap-2; }
.group-chip { @apply gap-1 text-sm text-ink; }
.group-count { @apply text-xs text-muted; }
.word-groups { @apply mt-1 flex flex-wrap gap-1; }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'TestNotebookPageShowsAndCreatesGroups|TestCreateGroupDuplicate|TestNotebookPageShowsWordGroupBadges|TestCreateGroupRejectsForeign' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/word_handlers.go internal/web/group_handlers.go internal/web/server.go \
  internal/web/group_handlers_test.go web/templates/notebook.html web/static/input.css
git commit -m "feat(web): show and create word groups on notebook page"
```

---

### Task 4: 组页面、改名、删除、勾选成员

**Files:**
- Modify: `internal/web/group_handlers.go`
- Modify: `internal/web/server.go`
- Create: `web/templates/group.html`
- Modify: `internal/web/group_handlers_test.go`
- Modify: `web/static/input.css`

**Interfaces:**
- Consumes: Task 1–2 的 `WordGroupByID`、`RenameWordGroup`、`DeleteWordGroup`、`GroupWordIDs`、`SetGroupWords`。
- Produces:
  ```go
  type groupPageData struct {
      Base
      Notebook store.Notebook
      Group    store.WordGroup
      Words    []groupWord
      Error    string
  }
  type groupWord struct {
      store.Word
      Checked bool
  }
  func (s *Server) handleGroupPage(w http.ResponseWriter, r *http.Request)      // GET
  func (s *Server) handleRenameGroup(w http.ResponseWriter, r *http.Request)    // POST
  func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request)    // POST
  func (s *Server) handleSaveGroupWords(w http.ResponseWriter, r *http.Request) // POST
  func (s *Server) loadGroup(w http.ResponseWriter, r *http.Request, nb store.Notebook) (store.WordGroup, bool)
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestGroupPageShowsWordsAndSavesMembers(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	b := tc.seedWord(tc.notebook.ID, "pear", "n. 梨")
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	path := fmt.Sprintf("/notebooks/%d/groups/%d", tc.notebook.ID, g.ID)

	body := tc.get(path).Body.String()
	if !strings.Contains(body, "apple") || !strings.Contains(body, "pear") || !strings.Contains(body, "保存") {
		t.Fatalf("page=%s", body)
	}
	resp := tc.postForm(path+"/words", url.Values{"word_id": {fmt.Sprint(a.ID), fmt.Sprint(b.ID)}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	ids, _ := tc.store.GroupWordIDs(context.Background(), tc.user.ID, g.ID)
	if !slices.Equal(ids, []int64{a.ID, b.ID}) {
		t.Fatalf("ids=%v", ids)
	}
	body = tc.get(path).Body.String()
	if strings.Count(body, "checked") < 2 {
		t.Fatalf("checkboxes not checked: %s", body)
	}
}

func TestGroupPageRejectsForeignGroup(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	g, _ := tc.store.CreateWordGroup(context.Background(), other.ID, "动物")
	base := fmt.Sprintf("/notebooks/%d/groups/%d", tc.notebook.ID, g.ID)

	if resp := tc.get(base); resp.Code != http.StatusNotFound {
		t.Fatalf("GET %s code=%d", base, resp.Code)
	}
	for _, path := range []string{base + "/rename", base + "/delete", base + "/words"} {
		resp := tc.postForm(path, url.Values{})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("POST %s code=%d", path, resp.Code)
		}
	}
}

func TestRenameGroupAndDuplicate(t *testing.T) {
	tc := newAuthedTestClient(t)
	g1, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	if _, err := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "食物"); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/notebooks/%d/groups/%d/rename", tc.notebook.ID, g1.ID)
	resp := tc.postForm(path, FormValues("name", "食物"))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "已经有同名的分组了") {
		t.Fatalf("duplicate code=%d body=%s", resp.Code, resp.Body.String())
	}
	resp = tc.postForm(path, FormValues("name", "宠物"))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("rename code=%d", resp.Code)
	}
	got, _ := tc.store.WordGroupByID(context.Background(), tc.user.ID, g1.ID)
	if got.Name != "宠物" {
		t.Fatalf("name=%q", got.Name)
	}
}

func TestDeleteGroupKeepsWords(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{w.ID})
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/groups/%d/delete", tc.notebook.ID, g.ID), url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if _, err := tc.store.WordByID(context.Background(), tc.notebook.ID, w.ID); err != nil {
		t.Fatalf("word should survive: %v", err)
	}
}

func TestSaveGroupWordsRejectsForeignWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	foreign, _ := tc.store.AddWord(context.Background(), other.ID, store.Word{Word: "dog"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/groups/%d/words", tc.notebook.ID, g.ID),
		url.Values{"word_id": {fmt.Sprint(foreign.ID)}})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'TestGroupPage|TestRenameGroup|TestDeleteGroup|TestSaveGroupWords' -v`
Expected: FAIL，页面/路由不存在。

- [ ] **Step 3: Implement**

`loadGroup`：
1. `groupID, ok := parseID(r.PathValue("groupID"))`；失败 404。
2. `g, err := s.store.WordGroupByID(ctx, currentUser(r).ID, groupID)`。
3. `ErrNotFound` 或 `g.NotebookID != nb.ID` → 404「分组不存在。」
4. 其他错误 → `s.fail`。

`groupPageData` 构造 helper：
```go
func (s *Server) groupPageView(w, r, nb store.Notebook, g store.WordGroup, errMsg string) (groupPageData, bool)
```
读取 `ListWords(ctx, nb.ID, "added")`、`GroupWordIDs(ctx, userID, g.ID)`，把成员 ID 放进 `map[int64]bool`，构造 `[]groupWord`；`Base` 用 `pageData(w, r, g.Name, "notebooks")`。

`handleGroupPage`：`loadGroup` + `groupPageView` + `renderer.page("group")`。

`handleRenameGroup`：
- `loadGroup`；`ParseForm`；`validGroupName`。
- 非法 → `groupPageView(..., "名称需为 1–40 个字符。")`。
- `RenameWordGroup`；`ErrDuplicate` → `groupPageView(..., "已经有同名的分组了。")`；`ErrNotFound` → 404。
- 成功：flash「分组已改名。」303 到组页面。

`handleDeleteGroup`：
- `loadGroup`；`DeleteWordGroup`；`ErrNotFound` → 404。
- flash「分组已删除。」303 到 `/notebooks/{nb.ID}`。

`handleSaveGroupWords`：
- `loadGroup`；`ParseForm`。
- `for _, raw := range r.PostForm["word_id"] { id, ok := parseID(raw); if !ok { 400 "单词参数不对。" } ids = append(ids, id) }`。
- `SetGroupWords`；`ErrNotFound` → 404「有单词不属于这个生词本。」。
- flash「分组已更新。」303 到组页面。

`server.go` 增加：

```go
mux.HandleFunc("GET /notebooks/{id}/groups/{groupID}", s.handleGroupPage)
mux.HandleFunc("POST /notebooks/{id}/groups/{groupID}/rename", s.handleRenameGroup)
mux.HandleFunc("POST /notebooks/{id}/groups/{groupID}/delete", s.handleDeleteGroup)
mux.HandleFunc("POST /notebooks/{id}/groups/{groupID}/words", s.handleSaveGroupWords)
```

`web/templates/group.html`：

```html
{{define "content"}}
<header class="page-head">
  <a class="back-link" href="/notebooks/{{.Notebook.ID}}">← {{.Notebook.Name}}</a>
  <div class="detail-head">
    <div>
      <h1 class="page-title">{{.Group.Name}}</h1>
      <p class="page-subtitle">{{.Group.WordCount}} 个单词</p>
    </div>
    <div class="detail-actions">
      <details class="settings-details group-rename">
        <summary class="settings-summary">改名</summary>
        <form method="post" action="/notebooks/{{.Notebook.ID}}/groups/{{.Group.ID}}/rename" class="form-stack settings-form">
          {{template "csrf" .}}
          <input class="input" type="text" name="name" value="{{.Group.Name}}" maxlength="40" required>
          <button class="btn" type="submit">保存名称</button>
        </form>
      </details>
      <form method="post" action="/notebooks/{{.Notebook.ID}}/groups/{{.Group.ID}}/delete">
        {{template "csrf" .}}
        <button class="btn btn-danger" type="submit" data-confirm="删除分组「{{.Group.Name}}」？单词不会被删除。">删除</button>
      </form>
    </div>
  </div>
</header>
{{if .Error}}<p class="alert alert-error" role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/notebooks/{{.Notebook.ID}}/groups/{{.Group.ID}}/words" class="group-words-form">
  {{template "csrf" .}}
  {{if .Words}}
  <ul class="group-word-list">
    {{range .Words}}
    <li class="card group-word-row">
      <label class="choice group-word-choice">
        <input type="checkbox" name="word_id" value="{{.ID}}"{{if .Checked}} checked{{end}}>
        <span class="group-word-main">
          <span class="word-en">{{.Word}}</span>
          {{if .Phonetic}}<span class="word-phonetic">{{.Phonetic}}</span>{{end}}
          <span class="word-translation">{{FirstLine .Translation}}</span>
        </span>
        <span class="group-word-stars">{{template "stars" .Proficiency}}</span>
      </label>
    </li>
    {{end}}
  </ul>
  <button class="btn btn-primary btn-block" type="submit">保存</button>
  {{else}}
  <p class="card empty-state">这个生词本还没有单词，先去<a class="link" href="/lookup">查词</a>加入吧。</p>
  {{end}}
</form>
{{end}}
```

`input.css` 增加：

```css
.group-words-form { @apply flex flex-col gap-3; }
.group-word-list { @apply flex flex-col gap-2; }
.group-word-row { @apply p-3; }
.group-word-choice { @apply w-full items-center gap-3; }
.group-word-main { @apply flex min-w-0 flex-1 flex-col gap-0.5; }
.group-word-stars { @apply shrink-0; }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'TestGroupPage|TestRenameGroup|TestDeleteGroup|TestSaveGroupWords' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/group_handlers.go internal/web/server.go internal/web/group_handlers_test.go \
  web/templates/group.html web/static/input.css
git commit -m "feat(web): add group page with rename, delete, and word selection"
```

---

### Task 5: `RecallScope` 与 `ListRecallWords`

**Files:**
- Modify: `internal/store/errors.go`
- Create: `internal/store/recall_scope.go`
- Create: `internal/store/recall_scope_test.go`

**Interfaces:**
- Consumes: `wordColumns`、`scanWord`、`ListWords` 的排序约定。
- Produces:
  ```go
  type RecallScopeKind int
  const (
      ScopeAll RecallScopeKind = iota
      ScopeProficiency
      ScopeGroups
  )
  type RecallScope struct {
      Kind           RecallScopeKind
      MaxProficiency int
      GroupIDs       []int64
  }
  func (s *Store) ListRecallWords(ctx context.Context, notebookID int64, scope RecallScope) ([]Word, error)
  ```
- `ErrInvalidScope`：新增到 `internal/store/errors.go`，`errors.New("invalid recall scope")`。
- 行为：未知 Kind、`ScopeProficiency` 的 `MaxProficiency` 不在 2–10、`ScopeGroups` 的空 `GroupIDs` → `ErrInvalidScope`。`ScopeGroups` 里不属于该生词本的 group id 不报错，只是不贡献词（查询按 `w.notebook_id` 过滤）。

- [ ] **Step 1: Write the failing tests**

```go
func TestListRecallWordsScopes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple", Proficiency: 3})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear", Proficiency: 6})
	cat, _ := st.AddWord(ctx, nb.ID, Word{Word: "cat", Proficiency: 9})
	animals, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	food, _ := st.CreateWordGroup(ctx, nb.ID, "食物")
	_ = st.SetGroupWords(ctx, u.ID, animals.ID, []int64{apple.ID, pear.ID})
	_ = st.SetGroupWords(ctx, u.ID, food.ID, []int64{pear.ID, cat.ID})

	all, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeAll})
	if err != nil || len(all) != 3 {
		t.Fatalf("all=%d err=%v", len(all), err)
	}
	low, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeProficiency, MaxProficiency: 6})
	if err != nil || len(low) != 1 || low[0].Word != "apple" {
		t.Fatalf("low=%+v err=%v", low, err)
	}
	group, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeGroups, GroupIDs: []int64{animals.ID}})
	if err != nil || len(group) != 2 {
		t.Fatalf("group=%d err=%v", len(group), err)
	}
	union, err := st.ListRecallWords(ctx, nb.ID, RecallScope{Kind: ScopeGroups, GroupIDs: []int64{animals.ID, food.ID, animals.ID}})
	if err != nil || len(union) != 3 {
		t.Fatalf("union=%d err=%v", len(union), err)
	}
}

func TestListRecallWordsRejectsInvalidScope(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	cases := []RecallScope{
		{Kind: ScopeProficiency, MaxProficiency: 1},
		{Kind: ScopeProficiency, MaxProficiency: 11},
		{Kind: ScopeGroups},
		{Kind: RecallScopeKind(99)},
	}
	for i, sc := range cases {
		if _, err := st.ListRecallWords(ctx, nb.ID, sc); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store -run 'TestListRecallWords' -v`
Expected: FAIL，类型/方法未定义。

- [ ] **Step 3: Implement**

`ListRecallWords`：

- `ScopeAll`：复用 `ListWords(ctx, notebookID, "added")`。
- `ScopeProficiency`：校验 2–10；查询
  ```sql
  SELECT id, notebook_id, word, phonetic, translation, definition, examples_json,
         proficiency, added_at, last_reviewed_at, review_count
  FROM words WHERE notebook_id = ? AND proficiency < ?
  ORDER BY added_at DESC, id DESC
  ```
- `ScopeGroups`：`GroupIDs` 去重排序；空 → `ErrInvalidScope`；占位符拼接：
  ```sql
  SELECT DISTINCT id, notebook_id, word, phonetic, translation, definition, examples_json,
         proficiency, added_at, last_reviewed_at, review_count
  FROM words w
  JOIN word_group_members m ON m.word_id = w.id
  WHERE w.notebook_id = ? AND m.group_id IN (?,...)
  ORDER BY added_at DESC, id DESC
  ```
  `word_group_members` 没有 `id`、`added_at`、`proficiency` 等同名列，未限定的列名只可能来自 `words`；为可读性可以给 `wordColumns` 里的每一列加 `w.` 前缀（新定义一个 `wordColumnsAliased`），但不要改变 `wordColumns` 本身。
- 默认返回空切片 `[]Word{}`，不要返回 `nil`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store -run 'TestListRecallWords' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/store/errors.go internal/store/recall_scope.go internal/store/recall_scope_test.go
git commit -m "feat(store): add recall scopes and scoped word listing"
```

---

### Task 6: recall manager 保存范围

**Files:**
- Modify: `internal/recall/manager.go`
- Modify: `internal/recall/manager_test.go`

**Interfaces:**
- Consumes: `store.RecallScope`、`store.ScopeAll`（Task 5）。
- Produces:
  ```go
  type State struct {
      NotebookID int64
      Scope      store.RecallScope
      // ...existing fields
  }
  func (m *Manager) Start(userID, notebookID int64, scope store.RecallScope) State
  ```
- `Begin` 签名不变；新建状态时 `Scope.Kind = store.ScopeAll`；同一生词本已有活跃轮次时保留其范围。
- `Start` 总是创建新状态，复制 `scope.GroupIDs`，清零 `Known`/`Unknown`、重置 `StartedAt`、`LastWordID`、`AnsweredWordID`。
- `cloneState` 复制 `Scope.GroupIDs`，避免共享底层数组。

- [ ] **Step 1: Write the failing tests**

```go
func TestManagerStartSetsScopeAndResets(t *testing.T) {
	m := NewManager()
	scope := store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{2, 1}}
	s := m.Start(1, 10, scope)
	if s.NotebookID != 10 || s.Scope.Kind != store.ScopeGroups || len(s.Scope.GroupIDs) != 2 {
		t.Fatalf("state=%+v", s)
	}
	if _, err := m.Answer(1, 10, 5, true); err != nil {
		t.Fatal(err)
	}
	s = m.Start(1, 10, store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6})
	if s.Known != 0 || s.Unknown != 0 || s.Scope.Kind != store.ScopeProficiency || s.Scope.MaxProficiency != 6 {
		t.Fatalf("restart should reset: %+v", s)
	}
}

func TestManagerBeginKeepsActiveScope(t *testing.T) {
	m := NewManager()
	m.Start(1, 10, store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{3}})
	s := m.Begin(1, 10)
	if s.Scope.Kind != store.ScopeGroups || len(s.Scope.GroupIDs) != 1 {
		t.Fatalf("begin should keep scope: %+v", s)
	}
	s = m.Begin(1, 11)
	if s.Scope.Kind != store.ScopeAll || s.NotebookID != 11 {
		t.Fatalf("new notebook should default to all: %+v", s)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/recall -run 'TestManagerStart|TestManagerBeginKeeps' -v`
Expected: FAIL，`Start` 未定义、`Scope` 字段不存在。

- [ ] **Step 3: Implement**

- 增加 `store` import（`recall` → `store`，`store` 不反向依赖 `recall`，无环）。
- `State` 加 `Scope store.RecallScope`。
- `Start`：
  ```go
  func (m *Manager) Start(userID, notebookID int64, scope store.RecallScope) State {
      scope.GroupIDs = append([]int64(nil), scope.GroupIDs...)
      m.mu.Lock()
      defer m.mu.Unlock()
      s := &State{NotebookID: notebookID, Scope: scope, StartedAt: time.Now()}
      m.states[userID] = s
      return *s
  }
  ```
- `Begin` 的新建分支 `&State{NotebookID: notebookID, Scope: store.RecallScope{Kind: store.ScopeAll}, StartedAt: time.Now()}`。
- `cloneState` 复制 `Scope.GroupIDs`。
- `SetLastWord` 在没有状态时创建的 `State` 也显式带 `ScopeAll`（零值即可，但写出来更清楚）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/recall -run 'TestManagerStart|TestManagerBeginKeeps' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/recall/manager.go internal/recall/manager_test.go
git commit -m "feat(recall): keep the chosen scope in round state"
```

---

### Task 7: 范围页 GET、解析与编码

**Files:**
- Create: `internal/web/recall_scope.go`
- Create: `internal/web/recall_scope_test.go`
- Modify: `internal/web/server.go`
- Create: `web/templates/recall_start.html`
- Modify: `web/templates/notebook.html`
- Modify: `web/templates/recall_select.html`
- Modify: `web/static/app.js`
- Modify: `web/static/input.css`

**Interfaces:**
- Consumes: `Store.ListWords`、`Store.ListWordGroups`、`Store.ErrInvalidScope`、`store.RecallScope`。
- Produces:
  ```go
  type scopeOption struct {
      Level int
      Label string
      Count int
  }
  type recallStartGroup struct {
      store.WordGroup
      Checked bool
  }
  type recallStartData struct {
      Base
      Notebook            store.Notebook
      AllCount            int
      Options             []scopeOption
      Groups              []recallStartGroup
      AllSelected         bool
      ProficiencySelected bool
      GroupsSelected      bool
      MaxProficiency      int
      Error               string
  }
  func parseRecallScope(v url.Values) (store.RecallScope, error)
  func scopeQuery(scope store.RecallScope) url.Values
  func scopeLabel(scope store.RecallScope, groups []store.WordGroup) string
  func formatStars(level int) string
  func (s *Server) handleRecallStartPage(w http.ResponseWriter, r *http.Request)
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestRecallStartPageShowsCountsAndDefaultsToAll(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple", Proficiency: 3})
	tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear", Proficiency: 6})
	tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "cat", Proficiency: 9})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	body := tc.get(fmt.Sprintf("/recall/%d/start", tc.notebook.ID)).Body.String()
	for _, want := range []string{
		"练哪些词", `value="all" checked`, "3 个",
		"低于 4 星（1 个）", "低于 7 星（2 个）", "动物（0 个）",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	_ = g
}

func TestParseRecallScope(t *testing.T) {
	ok := []struct {
		v    url.Values
		want store.RecallScope
	}{
		{url.Values{}, store.RecallScope{Kind: store.ScopeAll}},
		{url.Values{"scope": {"all"}}, store.RecallScope{Kind: store.ScopeAll}},
		{url.Values{"scope": {"proficiency"}}, store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}},
		{url.Values{"scope": {"proficiency"}, "max": {"8"}}, store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 8}},
		{url.Values{"scope": {"groups"}, "group_id": {"2", "1", "2"}}, store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 2}}},
	}
	for i, c := range ok {
		got, err := parseRecallScope(c.v)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("case %d got=%+v err=%v", i, got, err)
		}
	}
	bad := []url.Values{
		{"scope": {"proficiency"}, "max": {"1"}},
		{"scope": {"proficiency"}, "max": {"11"}},
		{"scope": {"groups"}},
		{"scope": {"weird"}},
	}
	for i, v := range bad {
		if _, err := parseRecallScope(v); !errors.Is(err, store.ErrInvalidScope) {
			t.Errorf("bad case %d: %v", i, err)
		}
	}
}

func TestScopeQueryAndLabel(t *testing.T) {
	if got := scopeQuery(store.RecallScope{Kind: store.ScopeAll}).Encode(); got != "scope=all" {
		t.Fatalf("all query=%s", got)
	}
	if got := scopeQuery(store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}).Encode(); got != "max=6&scope=proficiency" {
		t.Fatalf("proficiency query=%s", got)
	}
	if got := scopeQuery(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{2, 1}}).Encode(); got != "group_id=1&group_id=2&scope=groups" {
		t.Fatalf("groups query=%s", got)
	}
	groups := []store.WordGroup{{ID: 1, Name: "动物"}, {ID: 2, Name: "食物"}, {ID: 3, Name: "易错词"}, {ID: 4, Name: "Unit 4"}}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeAll}, groups); got != "全部" {
		t.Fatalf("label=%q", got)
	}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}, groups); got != "低于 3 星" {
		t.Fatalf("label=%q", got)
	}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 2}}, groups); got != "动物、食物" {
		t.Fatalf("label=%q", got)
	}
	if got := scopeLabel(store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{1, 2, 3, 4}}, groups); got != "动物、食物、易错词 等 4 组" {
		t.Fatalf("label=%q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'TestRecallStartPage|TestParseRecallScope|TestScopeQuery' -v`
Expected: FAIL，函数未定义或路由 404。

- [ ] **Step 3: Implement**

`parseRecallScope(v url.Values)`：
- `scope := strings.TrimSpace(v.Get("scope"))`；空或 `"all"` → `ScopeAll`。
- `"proficiency"`：`raw := v.Get("max")`；空 → 6；`strconv.Atoi`；不在 2–10 → `store.ErrInvalidScope`。
- `"groups"`：遍历 `v["group_id"]`；`parseID`；非法跳过还是报错？**报错**（`ErrInvalidScope`）；去重、排序；空 → `ErrInvalidScope`。
- 其他 → `store.ErrInvalidScope`。

`scopeQuery(scope)`：按 §8.6 的编码规则构造 `url.Values`；`groups` 的 ID 先排序再 `Add`。

`formatStars(level int) string`：偶数 `strconv.Itoa(level/2)`，奇数 `strconv.FormatFloat(float64(level)/2, 'f', 1, 64)`（3 → "1.5"，6 → "3"）。

`scopeLabel(scope, groups)`：
- `all` → "全部"。
- `proficiency` → `"低于 " + formatStars(scope.MaxProficiency) + " 星"`。
- `groups`：用 `map[int64]string` 查名称；缺失的 ID 忽略；没有名称 → "分组"；超过 3 个 → 前三个名称 + `fmt.Sprintf(" 等 %d 组", len(ids))`；否则 `strings.Join(names, "、")`。

`recallStartView`（内部 helper）：
- `words, err := s.store.ListWords(ctx, nb.ID, "added")`。
- `Options`：level 2..10，`Count` 为 `proficiency < level` 的词数，`Label = "低于 " + formatStars(level) + " 星"`。
- `groups, err := s.store.ListWordGroups(ctx, nb.ID)`。
- `MaxProficiency`：scope 是 proficiency 时用其值，否则 6。
- 选中标志：`AllSelected`、`ProficiencySelected`、`GroupsSelected`。
- `Groups`：每个 `recallStartGroup{WordGroup: g, Checked: groupIDIn(scope.GroupIDs, g.ID)}`。
- `Error` 用传入的 `errMsg`。

`handleRecallStartPage`：
1. `nb, ok := s.loadRecallNotebook(w, r)`。
2. `scope, err := parseRecallScope(r.URL.Query())`；`ErrInvalidScope` → `renderError(400, "范围参数不对。")`。
3. `data, ok := s.recallStartView(w, r, nb, scope, "")`。
4. `renderer.page(w, 200, "recall_start", data)`。

`server.go` 增加 `mux.HandleFunc("GET /recall/{id}/start", s.handleRecallStartPage)`。

`web/templates/recall_start.html`：

```html
{{define "content"}}
<header class="page-head">
  <a class="back-link" href="/notebooks/{{.Notebook.ID}}">← {{.Notebook.Name}}</a>
  <h1 class="page-title">练哪些词？</h1>
  <p class="page-subtitle">选好范围，这一轮就从里面抽词。</p>
</header>
{{if .Error}}<p class="alert alert-error" role="alert">{{.Error}}</p>{{end}}
{{if .AllCount}}
<form method="post" action="/recall/{{.Notebook.ID}}/start" class="card scope-form">
  {{template "csrf" .}}
  <fieldset class="scope-options">
    <legend class="field-label">回忆范围</legend>
    <label class="scope-option">
      <input type="radio" name="scope" value="all"{{if .AllSelected}} checked{{end}}>
      <span class="scope-name">全部</span>
      <span class="scope-count">{{.AllCount}} 个</span>
    </label>
    <label class="scope-option">
      <input type="radio" name="scope" value="proficiency"{{if .ProficiencySelected}} checked{{end}}>
      <span class="scope-name">低于
        <select class="input select scope-select" name="max" data-scope-proficiency>
          {{range .Options}}<option value="{{.Level}}"{{if eq .Level $.MaxProficiency}} selected{{end}}>{{.Label}}（{{.Count}} 个）</option>{{end}}
        </select>
        的单词
      </span>
    </label>
    <label class="scope-option{{if not .Groups}} is-disabled{{end}}">
      <input type="radio" name="scope" value="groups"{{if .GroupsSelected}} checked{{end}}{{if not .Groups}} disabled{{end}}>
      <span class="scope-name">分组</span>
    </label>
    {{if .Groups}}
    <ul class="scope-groups">
      {{range .Groups}}
      <li><label class="choice"><input type="checkbox" name="group_id" value="{{.ID}}"{{if .Checked}} checked{{end}} data-scope-group><span>{{.Name}}（{{.WordCount}} 个）</span></label></li>
      {{end}}
    </ul>
    {{else}}
    <p class="muted-text">还没有分组，先去生词本里建一个吧。</p>
    {{end}}
  </fieldset>
  <button class="btn btn-primary btn-block" type="submit">开始回忆</button>
</form>
{{else}}
<section class="card empty-state">
  {{template "owl" .}}
  <p>这个生词本还没有单词，去<a class="link" href="/lookup">查词</a>加入吧。</p>
</section>
{{end}}
{{end}}
```

`app.js` 追加两段 `change` 委托（与现有 `[data-auto-submit]` 的委托并列，不要覆盖已有监听）：

```js
document.addEventListener('change', (event) => {
  const select = event.target.closest('[data-scope-proficiency]');
  if (select) {
    const radio = select.form.querySelector('input[name="scope"][value="proficiency"]');
    if (radio) radio.checked = true;
  }
  const box = event.target.closest('[data-scope-group]');
  if (box && box.checked) {
    const radio = box.form.querySelector('input[name="scope"][value="groups"]');
    if (radio) radio.checked = true;
  }
});
```

`notebook.html` 的「开始回忆」链接改为 `/recall/{{.Notebook.ID}}/start`；`recall_select.html` 的链接同样改为 `/recall/{{.ID}}/start`。

`input.css` 增加：

```css
.scope-form { @apply flex flex-col gap-4; }
.scope-options { @apply flex flex-col gap-3; }
.scope-option { @apply flex items-start gap-3 rounded-2xl px-4 py-3 font-semibold; background: var(--glass-fill); border: 1px solid var(--glass-border); }
.scope-option input[type="radio"] { @apply mt-1 h-5 w-5 shrink-0 accent-blue; }
.scope-option.is-disabled { @apply opacity-50; }
.scope-name { @apply flex flex-1 flex-wrap items-center gap-2; }
.scope-count { @apply ml-auto text-sm text-muted; }
.scope-select { @apply inline-flex min-h-[36px] w-auto px-3 py-1; }
.scope-groups { @apply ml-8 flex flex-col gap-2; }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'TestRecallStartPage|TestParseRecallScope|TestScopeQuery' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/recall_scope.go internal/web/recall_scope_test.go internal/web/server.go \
  web/templates/recall_start.html web/templates/notebook.html web/templates/recall_select.html \
  web/static/app.js web/static/input.css
git commit -m "feat(web): add recall scope selection page"
```

---

### Task 8: 开始一轮与范围感知的抽词

**Files:**
- Modify: `internal/web/recall_scope.go`
- Modify: `internal/web/recall_handlers.go`
- Modify: `internal/web/server.go`
- Modify: `web/templates/recall.html`
- Modify: `web/templates/partials.html`
- Modify: `internal/web/recall_scope_test.go`（如测试分组需要独立文件，也可新建 `recall_start_test.go`）
- Modify: `web/static/input.css`

**Interfaces:**
- Consumes: `parseRecallScope`、`scopeQuery`、`scopeLabel`、`recallStartView`（Task 7）；`Manager.Start`（Task 6）；`ListRecallWords`（Task 5）。
- Produces:
  ```go
  func (s *Server) handleRecallStartPost(w http.ResponseWriter, r *http.Request)
  func (s *Server) scopeGroupsExist(ctx context.Context, notebookID int64, scope store.RecallScope) (bool, error)
  ```
- `recallView` 增加 `ScopeLabel string`、`ChangeScopeURL string`。

- [ ] **Step 1: Write the failing tests**

```go
func TestRecallStartAllScope(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID), url.Values{"scope": {"all"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, "本轮范围：全部") || !strings.Contains(body, "apple") {
		t.Fatalf("body=%s", body)
	}
}

func TestRecallStartProficiencyOnlyPicksQualifyingWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple", Proficiency: 8})
	low, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear", Proficiency: 3})
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"proficiency"}, "max": {"6"}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, low.Word) || strings.Contains(body, apple.Word) {
		t.Fatalf("scope leaked: %s", body)
	}
	if !strings.Contains(body, "本轮范围：低于 3 星") {
		t.Fatalf("label missing: %s", body)
	}
}

func TestRecallStartGroupsOnlyPicksGroupWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	pear, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, apple.Word) || strings.Contains(body, pear.Word) {
		t.Fatalf("scope leaked: %s", body)
	}
	if !strings.Contains(body, "本轮范围：动物") {
		t.Fatalf("label missing: %s", body)
	}
}

func TestRecallStartRejectsForeignGroup(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	g, _ := tc.store.CreateWordGroup(context.Background(), other.ID, "动物")
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "分组已经不存在") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRecallStartEmptyScopeDoesNotStart(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果") // 熟练度 5
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"proficiency"}, "max": {"2"}})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "这个范围里还没有单词") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRecallGroupDeletedMidRoundShowsEmptyScope(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	resp := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("start code=%d", resp.Code)
	}
	if err := tc.store.DeleteWordGroup(context.Background(), tc.user.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	next := tc.postForm(fmt.Sprintf("/recall/%d/next", tc.notebook.ID), url.Values{})
	if next.Code != http.StatusOK {
		t.Fatalf("next code=%d body=%s", next.Code, next.Body.String())
	}
	body := next.Body.String()
	if !strings.Contains(body, "这个范围里还没有单词") || !strings.Contains(body, "换个范围") {
		t.Fatalf("empty scope missing: %s", body)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'TestRecallStartAll|TestRecallStartProficiency|TestRecallStartGroups|TestRecallStartRejects|TestRecallStartEmpty|TestRecallGroupDeleted' -v`
Expected: FAIL，POST 路由 404，`ScopeLabel` 字段不存在。

- [ ] **Step 3: Implement**

`scopeGroupsExist(ctx, notebookID, scope)`：
- 非 `ScopeGroups` → `true, nil`。
- `ListWordGroups(ctx, notebookID)`；建 `map[int64]bool`；每个 `scope.GroupIDs` 都必须在 map 里；否则 `false, nil`。
- 数据库错误原样返回。

`handleRecallStartPost`：
1. `nb := loadRecallNotebook`。
2. `r.ParseForm()`。
3. `scope, err := parseRecallScope(r.PostForm)`；`ErrInvalidScope` → `renderError(400, "范围参数不对。")`。
4. `ok, err := s.scopeGroupsExist(ctx, nb.ID, scope)`；err → `s.fail`；`!ok` → `data := recallStartView(..., scope, "分组已经不存在，请重新选择。")`，200 渲染范围页。
5. `words, err := s.store.ListRecallWords(ctx, nb.ID, scope)`；`ErrInvalidScope` → 400；其他 → fail。
6. `len(words)==0` → `recallStartView(..., scope, "这个范围里还没有单词。")`，200 渲染范围页。
7. `s.recall.Start(userID, nb.ID, scope)`；303 到 `/recall/{nb.ID}`。

`handleRecallPage`：
- 先 `state := s.recall.Begin(userID, nb.ID)`。
- `words, err := s.store.ListRecallWords(ctx, nb.ID, state.Scope)`；`ErrInvalidScope` 不可能出现，若出现按 `s.fail`。
- 抽词逻辑不变；设置 `ScopeLabel` 与 `ChangeScopeURL`：
  ```go
  groups, err := s.store.ListWordGroups(ctx, nb.ID)
  view := recallView{
      Base: base, Notebook: nb, Word: word, State: state,
      ScopeLabel: scopeLabel(state.Scope, groups),
      ChangeScopeURL: "/recall/" + strconv.FormatInt(nb.ID, 10) + "/start?" + scopeQuery(state.Scope).Encode(),
  }
  ```
- 注意 `Base` 要在 `pageData` 之后取；`ListWordGroups` 失败时 `s.fail`。

`handleRecallNext`：
- `state := s.recall.Begin(userID, nb.ID)`；`ListRecallWords(ctx, nb.ID, state.Scope)`。
- `ChangeScopeURL` 同上；`ScopeLabel` 可留空（fragment 不渲染标题），但为了复用 `recall_card` 的空状态，`ChangeScopeURL` 必须设置。

`recall.html` 标题下加：

```html
<p class="page-subtitle">本轮范围：{{.ScopeLabel}}</p>
<p class="page-subtitle">只凭记忆，选一个答案。</p>
```

`partials.html` 的 `recall_card` 空状态改为：

```html
{{else}}
<p class="empty-recall">这个范围里还没有单词。</p>
<div class="detail-actions center-actions">
  <a class="btn btn-primary" href="{{.ChangeScopeURL}}">换个范围</a>
  <a class="btn" href="/lookup">去查词</a>
</div>
{{end}}
```

`server.go` 增加 `mux.HandleFunc("POST /recall/{id}/start", s.handleRecallStartPost)`。

`input.css` 如缺 `.center-actions`，复用已有定义或补：

```css
.center-actions { @apply justify-center; }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'TestRecallStartAll|TestRecallStartProficiency|TestRecallStartGroups|TestRecallStartRejects|TestRecallStartEmpty|TestRecallGroupDeleted' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/recall_scope.go internal/web/recall_handlers.go internal/web/server.go \
  internal/web/recall_scope_test.go web/templates/recall.html web/templates/partials.html web/static/input.css
git commit -m "feat(web): start recall rounds with the selected scope"
```

---

### Task 9: 小结、结束与再来一轮保留范围

**Files:**
- Modify: `internal/web/recall_handlers.go`
- Modify: `web/templates/summary.html`
- Modify: `internal/web/recall_scope_test.go`

**Interfaces:**
- Consumes: `parseRecallScope`、`scopeQuery`、`scopeLabel`、`scopeGroupsExist`、`ListRecallWords`、`Manager.Start`。
- Produces:
  ```go
  type summaryData struct {
      Base
      Notebook      store.Notebook
      Known         int
      Unknown       int
      Total         int
      ScopeLabel    string
      RestartURL    string
      ChangeScopeURL string
  }
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestRecallEndPreservesScopeInSummary(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	start := tc.postForm(fmt.Sprintf("/recall/%d/start", tc.notebook.ID),
		url.Values{"scope": {"groups"}, "group_id": {fmt.Sprint(g.ID)}})
	if start.Code != http.StatusSeeOther {
		t.Fatalf("start code=%d", start.Code)
	}
	end := tc.postForm(fmt.Sprintf("/recall/%d/end", tc.notebook.ID), url.Values{})
	if end.Code != http.StatusSeeOther {
		t.Fatalf("end code=%d", end.Code)
	}
	loc := end.Header().Get("Location")
	if !strings.Contains(loc, "scope=groups") || !strings.Contains(loc, "group_id=") {
		t.Fatalf("location=%s", loc)
	}
	body := tc.get(loc).Body.String()
	if !strings.Contains(body, "本轮范围：动物") || !strings.Contains(body, "再来一轮") || !strings.Contains(body, "换个范围") {
		t.Fatalf("summary=%s", body)
	}
}

func TestRecallRestartUsesSameScope(t *testing.T) {
	tc := newAuthedTestClient(t)
	apple, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple"})
	pear, _ := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "pear"})
	g, _ := tc.store.CreateWordGroup(context.Background(), tc.notebook.ID, "动物")
	_ = tc.store.SetGroupWords(context.Background(), tc.user.ID, g.ID, []int64{apple.ID})
	loc := fmt.Sprintf("/recall/%d/restart?scope=groups&group_id=%d", tc.notebook.ID, g.ID)
	resp := tc.get(loc)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("restart code=%d body=%s", resp.Code, resp.Body.String())
	}
	body := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(body, "本轮范围：动物") || !strings.Contains(body, apple.Word) || strings.Contains(body, pear.Word) {
		t.Fatalf("restart scope lost: %s", body)
	}
}

func TestRecallRestartEmptyScopeReturnsToScopePage(t *testing.T) {
	tc := newAuthedTestClient(t)
	_, _ = tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{Word: "apple", Proficiency: 5})
	loc := fmt.Sprintf("/recall/%d/restart?scope=proficiency&max=2", tc.notebook.ID)
	resp := tc.get(loc)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if !strings.Contains(resp.Header().Get("Location"), "/start?") {
		t.Fatalf("location=%s", resp.Header().Get("Location"))
	}
	page := tc.get(resp.Header().Get("Location")).Body.String()
	if !strings.Contains(page, "这个范围里还没有单词") {
		t.Fatalf("flash missing: %s", page)
	}
}

func TestRecallRestartRejectsForeignGroup(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	g, _ := tc.store.CreateWordGroup(context.Background(), other.ID, "动物")
	resp := tc.get(fmt.Sprintf("/recall/%d/restart?scope=groups&group_id=%d", tc.notebook.ID, g.ID))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if !strings.Contains(resp.Header().Get("Location"), "/start?") {
		t.Fatalf("location=%s", resp.Header().Get("Location"))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'TestRecallEndPreserves|TestRecallRestart' -v`
Expected: FAIL，范围参数未保留。

- [ ] **Step 3: Implement**

`summaryData` 加三个字段。`handleRecallSummary`：
1. `parseRecallScope(r.URL.Query())`；`ErrInvalidScope` → 400。
2. `groups, _ := s.store.ListWordGroups(ctx, nb.ID)`。
3. `RestartURL = "/recall/{id}/restart?" + scopeQuery(scope).Encode()`。
4. `ChangeScopeURL = "/recall/{id}/start?" + scopeQuery(scope).Encode()`。
5. `ScopeLabel = scopeLabel(scope, groups)`。

`handleRecallEnd`：

```go
redirect := "/recall/" + strconv.FormatInt(nb.ID, 10) + "/summary?known=" +
    strconv.Itoa(state.Known) + "&unknown=" + strconv.Itoa(state.Unknown) +
    "&" + scopeQuery(state.Scope).Encode()
```

`handleRecallRestart`：
1. `scope, err := parseRecallScope(r.URL.Query())`；`ErrInvalidScope` → 400。
2. `ok, err := s.scopeGroupsExist(ctx, nb.ID, scope)`；err → fail；`!ok` → `s.recall.Reset(userID)`，flash「这个范围里还没有单词，先换个范围吧。」，303 到 `/recall/{id}/start?` + `scopeQuery(scope).Encode()`。
3. `words, err := s.store.ListRecallWords(ctx, nb.ID, scope)`；err → fail；空 → 同样 `Reset` + flash + 303 到范围页。
4. `s.recall.Start(userID, nb.ID, scope)`；303 到 `/recall/{id}`。

`Start` 会整体覆盖旧状态，因此成功分支不需要再调用 `Reset`；两个提前返回的分支必须调用 `Reset`，否则上一轮的计数和范围会残留。

`summary.html`：

```html
<p class="auth-subtitle">{{.Notebook.Name}} · 本轮范围：{{.ScopeLabel}}</p>
...
<div class="detail-actions center-actions">
  <a class="btn btn-primary" href="{{.RestartURL}}">再来一轮</a>
  <a class="btn" href="{{.ChangeScopeURL}}">换个范围</a>
  <a class="btn" href="/notebooks/{{.Notebook.ID}}">返回生词本</a>
</div>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'TestRecallEndPreserves|TestRecallRestart' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/recall_handlers.go internal/web/recall_scope_test.go web/templates/summary.html
git commit -m "feat(web): preserve recall scope in summary and restart"
```

---

### Task 10: 导出 v2 / 导入分组

**Files:**
- Modify: `internal/store/export.go`
- Modify: `internal/store/export_test.go`
- Modify: `internal/store/import.go`
- Modify: `internal/store/import_test.go`
- Modify: `internal/web/import_handlers.go`
- Modify: `internal/web/export_import_test.go`
- Modify: `web/templates/settings.html`

**Interfaces:**
- Consumes: `ListWordGroups`、`GroupMemberships`（Task 1–2）。
- Produces:
  ```go
  const exportVersion = 2

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

  type ImportStats struct {
      // ...existing fields
      GroupsCreated          int
      GroupMembershipsAdded  int
  }
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestExportIncludesGroups(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID, apple.ID})
	if _, err := st.CreateWordGroup(ctx, nb.ID, "空组"); err != nil {
		t.Fatal(err)
	}

	f, err := st.ExportAll(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 2 {
		t.Fatalf("version=%d", f.Version)
	}
	if len(f.Notebooks[0].Groups) != 2 {
		t.Fatalf("groups=%+v", f.Notebooks[0].Groups)
	}
	byName := map[string]ExportGroup{}
	for _, g := range f.Notebooks[0].Groups {
		byName[g.Name] = g
	}
	if !slices.Equal(byName["动物"].Words, []string{"apple", "pear"}) {
		t.Fatalf("animal words=%v", byName["动物"].Words)
	}
	if byName["空组"].Words == nil || len(byName["空组"].Words) != 0 {
		t.Fatalf("empty group=%+v", byName["空组"])
	}
}

func TestImportV2GroupsRoundTrip(t *testing.T) {
	src := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := src.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := src.AddWord(ctx, nb.ID, Word{Word: "apple"})
	g, _ := src.CreateWordGroup(ctx, nb.ID, "动物")
	_ = src.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})
	f, err := src.ExportAll(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	dst := newTestStore(t)
	u2, _, _ := dst.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	stats, err := dst.Import(ctx, u2.ID, f, ImportOverwrite)
	if err != nil {
		t.Fatal(err)
	}
	if stats.GroupsCreated != 1 || stats.GroupMembershipsAdded != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	nbs, _ := dst.ListNotebooks(ctx, u2.ID)
	if len(nbs) != 1 {
		t.Fatalf("notebooks=%+v", nbs)
	}
	groups, _ := dst.ListWordGroups(ctx, nbs[0].ID)
	if len(groups) != 1 || groups[0].Name != "动物" || groups[0].WordCount != 1 {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestImportMergeUnionsGroupMembers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, nb.ID, Word{Word: "pear"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "动物")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{pear.ID})

	f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{{
		Name:  nb.Name,
		Words: []ExportWord{{Word: "apple", Proficiency: 5}},
		Groups: []ExportGroup{{Name: "动物", Words: []string{"apple"}}},
	}}}
	stats, err := st.Import(ctx, u.ID, f, ImportMerge)
	if err != nil {
		t.Fatal(err)
	}
	if stats.GroupsCreated != 0 || stats.GroupMembershipsAdded != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	ids, _ := st.GroupWordIDs(ctx, u.ID, g.ID)
	if !slices.Equal(ids, []int64{apple.ID, pear.ID}) {
		t.Fatalf("ids=%v", ids)
	}
}

func TestImportRejectsUnknownGroupWordAndRollsBack(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{{
		Name:   "新本",
		Words:  []ExportWord{{Word: "apple", Proficiency: 5}},
		Groups: []ExportGroup{{Name: "动物", Words: []string{"ghost"}}},
	}}}
	if _, err := st.Import(ctx, u.ID, f, ImportMerge); err == nil {
		t.Fatal("import should reject unknown group word")
	}
	nbs, _ := st.ListNotebooks(ctx, u.ID)
	if len(nbs) != 1 || nbs[0].ID != nb.ID {
		t.Fatalf("partial import: %+v", nbs)
	}
}

func TestValidateExportRejectsBadGroups(t *testing.T) {
	base := ExportNotebook{
		Name:  "A",
		Words: []ExportWord{{Word: "apple", Proficiency: 5}},
	}
	cases := []ExportNotebook{
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "", Words: []string{"apple"}}}},
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "动物", Words: []string{"ghost"}}}},
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "动物", Words: []string{"apple", "APPLE"}}}},
		{Name: "A", Words: base.Words, Groups: []ExportGroup{{Name: "动物"}, {Name: "动物"}}},
	}
	for i, nb := range cases {
		f := ExportFile{Format: "study-words", Version: 2, Notebooks: []ExportNotebook{nb}}
		if err := ValidateExport(f); err == nil {
			t.Errorf("case %d should be invalid", i)
		}
	}
}
```

同时更新现有测试：
- `internal/store/export_test.go:33`：`f.Version != 1` → `f.Version != 2`。
- `internal/store/import_test.go:245`：把 `Version: 2` 的非法用例改为 `Version: 3`，并保留一个合法的 v2 用例。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store -run 'TestExportIncludesGroups|TestImportV2|TestImportMergeUnions|TestImportRejectsUnknown|TestValidateExportRejectsBadGroups' -v`
Expected: FAIL，`Groups` 字段不存在或 v2 被拒。

- [ ] **Step 3: Implement export**

- `exportVersion = 2`。
- `ExportNotebook` 加 `Groups []ExportGroup`；`ExportGroup` 按 spec §9.1 定义。
- `export` 每个生词本：
  ```go
  groups, err := s.ListWordGroups(ctx, nb.ID)
  memberships, err := s.GroupMemberships(ctx, nb.ID)
  byGroup := map[int64][]string{}
  for _, w := range words {
      for _, g := range memberships[w.ID] {
          byGroup[g.ID] = append(byGroup[g.ID], w.Word)
      }
  }
  en.Groups = make([]ExportGroup, 0, len(groups))
  for _, g := range groups {
      names := byGroup[g.ID]
      if names == nil { names = []string{} }
      slices.Sort(names)
      en.Groups = append(en.Groups, ExportGroup{Name: g.Name, Words: names})
  }
  ```
- 空分组输出 `words: []`。

- [ ] **Step 4: Implement validation and import**

`ValidateExport`：
- 版本检查改为 `if f.Version != 1 && f.Version != 2`。
- 仅当 `f.Version >= 2` 时：
  - `len(nb.Groups) > 100` → error。
  - `groupNames := map[string]bool{}`；组名 `strings.TrimSpace` 后 1–40 runes；重复（`strings.ToLower`）→ error。
  - `wordSet := map[string]bool{}`，由 `nb.Words` 的 `strings.ToLower(strings.TrimSpace(w.Word))` 构造。
  - 每组词名 trim 后 1–100 runes；必须 `wordSet[key]`；组内重复 → error；`len(g.Words) > 10000` → error。
- v1 即使带了 `Groups` 也完全跳过这些检查。

`Import`：
- 在 `ImportOverwrite` 分支里，删除 `notebooks` 已经通过外键级联删除 `word_groups` 和成员，无需额外 SQL。
- 每个 notebook 的单词导入循环结束后，若 `f.Version >= 2 && len(nb.Groups) > 0`：
  ```go
  if err := importGroups(ctx, tx, id, nb.Groups, &stats); err != nil { return stats, err }
  ```
- 新增 `func importGroups(ctx context.Context, tx *sql.Tx, notebookID int64, groups []ExportGroup, stats *ImportStats) error`：
  1. `SELECT id, word FROM words WHERE notebook_id = ?` → `wordIDs map[string]int64`（lower）。
  2. `SELECT id, name FROM word_groups WHERE notebook_id = ?` → `groupIDs map[string]int64`（lower）。
  3. 每个组：组名 trim；key lower；不存在则 `INSERT INTO word_groups (notebook_id, name, created_at) VALUES (?, ?, ?)`，`stats.GroupsCreated++`。
  4. 每个词名：`wid, ok := wordIDs[lower]`；`!ok` 返回 error（校验应已拦截）；`INSERT OR IGNORE INTO word_group_members`；`RowsAffected()>0` → `stats.GroupMembershipsAdded++`。
- `ImportStats` 加两个字段。
- `importSummary` 加：
  ```go
  if stats.GroupsCreated > 0 { parts = append(parts, fmt.Sprintf("新建 %d 个分组", stats.GroupsCreated)) }
  if stats.GroupMembershipsAdded > 0 { parts = append(parts, fmt.Sprintf("恢复 %d 个分组词", stats.GroupMembershipsAdded)) }
  ```
- 设置页「导出数据」文案改为「导出文件包含生词本、分组、单词、例句、熟练度和回忆记录」；「覆盖」确认文案加「分组」。

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store -run 'TestExport|TestImport|TestValidateExport' -v && go test ./internal/web -run 'TestImport|TestExport' -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/store/export.go internal/store/export_test.go internal/store/import.go \
  internal/store/import_test.go internal/web/import_handlers.go internal/web/export_import_test.go \
  web/templates/settings.html
git commit -m "feat: export and import word groups as format version 2"
```

---

## Phase 2：MCP 令牌与 MCP 工具

### Task 11: MCP 令牌存储、生成函数与密码联动

**Files:**
- Modify: `internal/store/schema.sql`
- Create: `internal/store/mcp_tokens.go`
- Create: `internal/store/mcp_tokens_test.go`
- Modify: `internal/auth/auth.go`
- Modify: `internal/auth/auth_test.go`
- Modify: `internal/store/users.go`
- Modify: `internal/store/users_test.go`

**Interfaces:**
- Produces:
  ```go
  type MCPToken struct {
      UserID     int64
      CreatedAt  time.Time
      LastUsedAt *time.Time
  }
  func (s *Store) MCPTokenByUser(ctx context.Context, userID int64) (MCPToken, error)
  func (s *Store) ReplaceMCPToken(ctx context.Context, userID int64, hash string) (MCPToken, error)
  func (s *Store) RevokeMCPToken(ctx context.Context, userID int64) error
  func (s *Store) UserByMCPToken(ctx context.Context, hash string) (User, error)
  func (s *Store) TouchMCPToken(ctx context.Context, userID int64, at time.Time) error
  func NewPrefixedToken(prefix string, n int) (raw, hash string, err error) // internal/auth
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestNewPrefixedToken(t *testing.T) {
	raw, hash, err := NewPrefixedToken("mcp_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "mcp_") || len(raw) != 47 {
		t.Fatalf("raw=%q len=%d", raw, len(raw))
	}
	if hash != HashToken(raw) {
		t.Fatal("hash mismatch")
	}
	raw2, _, _ := NewPrefixedToken("mcp_", 32)
	if raw2 == raw {
		t.Fatal("tokens should differ")
	}
}

func TestMCPTokenLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if _, err := st.MCPTokenByUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty: %v", err)
	}
	tok, err := st.ReplaceMCPToken(ctx, u.ID, "hash1")
	if err != nil || tok.UserID != u.ID || tok.LastUsedAt != nil {
		t.Fatalf("tok=%+v err=%v", tok, err)
	}
	got, err := st.MCPTokenByUser(ctx, u.ID)
	if err != nil || got.CreatedAt.IsZero() || got.LastUsedAt != nil {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := st.ReplaceMCPToken(ctx, u.ID, "hash2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserByMCPToken(ctx, "hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old hash should be gone: %v", err)
	}
	if got, err := st.UserByMCPToken(ctx, "hash2"); err != nil || got.ID != u.ID {
		t.Fatalf("new hash: %+v %v", got, err)
	}
	if err := st.RevokeMCPToken(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MCPTokenByUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after revoke: %v", err)
	}
}

func TestTouchMCPTokenThrottles(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	_, _ = st.ReplaceMCPToken(ctx, u.ID, "hash1")
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	if err := st.TouchMCPToken(ctx, u.ID, t0); err != nil {
		t.Fatal(err)
	}
	got, _ := st.MCPTokenByUser(ctx, u.ID)
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(t0) {
		t.Fatalf("last=%v", got.LastUsedAt)
	}
	if err := st.TouchMCPToken(ctx, u.ID, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.MCPTokenByUser(ctx, u.ID)
	if !got.LastUsedAt.Equal(t0) {
		t.Fatalf("should throttle: %v", got.LastUsedAt)
	}
	if err := st.TouchMCPToken(ctx, u.ID, t0.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = st.MCPTokenByUser(ctx, u.ID)
	if !got.LastUsedAt.Equal(t0.Add(6 * time.Minute)) {
		t.Fatalf("should update: %v", got.LastUsedAt)
	}
}

func TestUpdatePasswordRevokesMCPToken(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, _, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	_, _ = st.ReplaceMCPToken(ctx, u.ID, "hash1")
	if err := st.UpdatePassword(ctx, u.ID, "newhash"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.UserByID(ctx, u.ID)
	if got.PasswordHash != "newhash" {
		t.Fatalf("hash=%q", got.PasswordHash)
	}
	if _, err := st.UserByMCPToken(ctx, "hash1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token should be revoked: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth -run TestNewPrefixedToken -v && go test ./internal/store -run 'TestMCPToken|TestTouchMCP|TestUpdatePasswordRevokes' -v`
Expected: FAIL，函数/表不存在。

- [ ] **Step 3: Implement schema and store**

`schema.sql` 追加：

```sql
CREATE TABLE IF NOT EXISTS mcp_tokens (
  user_id      INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   TEXT NOT NULL,
  last_used_at TEXT
);
```

`mcp_tokens.go`：

- `MCPTokenByUser`：`SELECT user_id, created_at, last_used_at FROM mcp_tokens WHERE user_id = ?`；`scanNullTime`。
- `ReplaceMCPToken`：
  ```sql
  INSERT INTO mcp_tokens (user_id, token_hash, created_at, last_used_at)
  VALUES (?, ?, ?, NULL)
  ON CONFLICT(user_id) DO UPDATE SET
    token_hash = excluded.token_hash,
    created_at = excluded.created_at,
    last_used_at = NULL
  ```
  返回 `MCPToken{UserID: userID, CreatedAt: now}`。
- `RevokeMCPToken`：`DELETE FROM mcp_tokens WHERE user_id = ?`，无行也返回 nil。
- `UserByMCPToken`：
  ```sql
  SELECT u.id, u.username, u.password_hash, u.created_at
  FROM mcp_tokens t JOIN users u ON u.id = t.user_id
  WHERE t.token_hash = ?
  ```
  用 `scanUser`。
- `TouchMCPToken`：
  ```sql
  UPDATE mcp_tokens SET last_used_at = ?
  WHERE user_id = ? AND (last_used_at IS NULL OR last_used_at < ?)
  ```
  第三个参数 `formatTime(at.UTC().Add(-5 * time.Minute))`。

`auth.NewPrefixedToken`：

```go
func NewPrefixedToken(prefix string, n int) (raw, hash string, err error) {
    raw, err = randomString(n)
    if err != nil {
        return "", "", err
    }
    raw = prefix + raw
    return raw, HashToken(raw), nil
}
```

`UpdatePassword` 改为事务：`UPDATE users SET password_hash = ? WHERE id = ?`；`RowsAffected()==0` → `ErrNotFound`；`DELETE FROM mcp_tokens WHERE user_id = ?`；`Commit`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth -run TestNewPrefixedToken -v && go test ./internal/store -run 'TestMCPToken|TestTouchMCP|TestUpdatePasswordRevokes' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/store/schema.sql internal/store/mcp_tokens.go internal/store/mcp_tokens_test.go \
  internal/auth/auth.go internal/auth/auth_test.go internal/store/users.go internal/store/users_test.go
git commit -m "feat: store hashed MCP tokens and revoke them on password change"
```

---

### Task 12: 设置页生成 / 撤销令牌

**Files:**
- Modify: `internal/web/settings_account.go`
- Create: `internal/web/mcp_token_handlers.go`
- Create: `internal/web/mcp_token_handlers_test.go`
- Modify: `internal/web/server.go`
- Modify: `web/templates/settings.html`
- Modify: `web/static/app.js`
- Modify: `web/static/input.css`

**Interfaces:**
- Consumes: `auth.NewPrefixedToken`、`Store.ReplaceMCPToken`、`MCPTokenByUser`、`RevokeMCPToken`（Task 11）。
- Produces:
  ```go
  type settingsView struct {
      // ...existing fields
      MCPToken    *store.MCPToken
      MCPCreated  string
      MCPLastUsed string
      NewMCPToken string
  }
  func (s *Server) handleGenerateMCPToken(w http.ResponseWriter, r *http.Request) // POST
  func (s *Server) handleRevokeMCPToken(w http.ResponseWriter, r *http.Request)   // POST
  ```

- [ ] **Step 1: Write the failing tests**

```go
var mcpTokenRE = regexp.MustCompile(`mcp_[A-Za-z0-9_-]+`)

func TestGenerateMCPTokenShowsOnceAndStoresHash(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/settings/mcp-token/generate", url.Values{})
	if resp.Code != http.StatusOK || resp.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code=%d cache=%q", resp.Code, resp.Header().Get("Cache-Control"))
	}
	body := resp.Body.String()
	if !strings.Contains(body, "令牌只显示这一次") {
		t.Fatalf("warning missing: %s", body)
	}
	raw := mcpTokenRE.FindString(body)
	if raw == "" {
		t.Fatalf("token missing: %s", body)
	}
	u, err := tc.store.UserByMCPToken(context.Background(), auth.HashToken(raw))
	if err != nil || u.ID != tc.user.ID {
		t.Fatalf("stored token: %+v %v", u, err)
	}
	page := tc.get("/settings").Body.String()
	if strings.Contains(page, raw) {
		t.Fatal("settings page must not show the token again")
	}
}

func TestRegenerateInvalidatesOldToken(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.postForm("/settings/mcp-token/generate", url.Values{}).Body.String()
	old := mcpTokenRE.FindString(body)
	tc.postForm("/settings/mcp-token/generate", url.Values{})
	if _, err := tc.store.UserByMCPToken(context.Background(), auth.HashToken(old)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old token still valid: %v", err)
	}
}

func TestRevokeMCPToken(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.postForm("/settings/mcp-token/generate", url.Values{})
	resp := tc.postForm("/settings/mcp-token/revoke", url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	if _, err := tc.store.MCPTokenByUser(context.Background(), tc.user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token still stored: %v", err)
	}
}

func TestChangePasswordRevokesMCPToken(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.postForm("/settings/mcp-token/generate", url.Values{}).Body.String()
	raw := mcpTokenRE.FindString(body)
	resp := tc.postForm("/settings/password", FormValues(
		"current", "secret1", "new", "secret2", "confirm", "secret2"))
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	if _, err := tc.store.UserByMCPToken(context.Background(), auth.HashToken(raw)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token should be revoked: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web -run 'TestGenerateMCPToken|TestRegenerateInvalidates|TestRevokeMCPToken|TestChangePasswordRevokes' -v`
Expected: FAIL，路由 404。

- [ ] **Step 3: Implement**

`settings_account.go`：
- `settingsView` 加 `MCPToken *store.MCPToken`、`MCPCreated string`、`MCPLastUsed string`、`NewMCPToken string`。
- `settingsData` 读取 `MCPTokenByUser`；`ErrNotFound` → nil；其他错误返回 false（走 fail）。
- 若 token 存在：`MCPCreated = s.FormatDate(tok.CreatedAt)`；`LastUsedAt != nil` 时 `MCPLastUsed = s.FormatDate(*tok.LastUsedAt) + " " + s.FormatTime(*tok.LastUsedAt)`。
- `renderSettingsData` 和 `handleSettingsPage` 都设置 `Cache-Control: no-store`。

`mcp_token_handlers.go`：
- `handleGenerateMCPToken`：
  ```go
  raw, hash, err := auth.NewPrefixedToken("mcp_", 32)
  if err != nil { s.fail(w, r, err); return }
  if _, err := s.store.ReplaceMCPToken(ctx, userID, hash); err != nil { s.fail(...); return }
  data, ok := s.settingsData(w, r)
  if !ok { return }
  data.NewMCPToken = raw
  w.Header().Set("Cache-Control", "no-store")
  _ = s.renderer.page(w, http.StatusOK, "settings", data)
  ```
- `handleRevokeMCPToken`：`RevokeMCPToken`；flash「MCP 令牌已撤销。」；303 到 `/settings`。

`server.go`：
```go
mux.HandleFunc("POST /settings/mcp-token/generate", s.handleGenerateMCPToken)
mux.HandleFunc("POST /settings/mcp-token/revoke", s.handleRevokeMCPToken)
```

`settings.html` 在「账户」和「导出数据」之间插入：

```html
<section class="card settings-section">
  <h2 class="settings-title">MCP 访问</h2>
  <p class="muted-text">用访问令牌让支持 MCP 的 Agent 管理生词本、分组和单词，并读取原始回忆记录。</p>
  {{if .NewMCPToken}}
  <div class="token-box">
    <p class="alert alert-error" role="alert">令牌只显示这一次，请现在复制；如果泄露，请立即重新生成。</p>
    <div class="token-row">
      <input class="input token-value" id="mcp-token-value" type="text" value="{{.NewMCPToken}}" readonly>
      <button class="btn" type="button" data-copy="#mcp-token-value">复制</button>
    </div>
  </div>
  {{end}}
  {{if .MCPToken}}
  <p class="muted-text">已生成 · 创建于 {{.MCPCreated}}{{if .MCPLastUsed}} · 最近使用 {{.MCPLastUsed}}{{end}}</p>
  <div class="settings-actions">
    <form method="post" action="/settings/mcp-token/generate">
      {{template "csrf" .}}
      <button class="btn" type="submit" data-confirm="重新生成后旧令牌会立即失效，确定吗？">重新生成</button>
    </form>
    <form method="post" action="/settings/mcp-token/revoke">
      {{template "csrf" .}}
      <button class="btn btn-danger" type="submit" data-confirm="撤销令牌后，使用它的 Agent 将立即无法访问。确定吗？">撤销令牌</button>
    </form>
  </div>
  {{else}}
  <form method="post" action="/settings/mcp-token/generate">
    {{template "csrf" .}}
    <button class="btn btn-primary" type="submit">生成访问令牌</button>
  </form>
  {{end}}
</section>
```

`app.js` 追加复制按钮委托：

```js
document.addEventListener('click', async (event) => {
  const btn = event.target.closest('[data-copy]');
  if (!btn) return;
  const input = document.querySelector(btn.getAttribute('data-copy'));
  if (!input) return;
  const text = input.value ?? input.textContent;
  try {
    await navigator.clipboard.writeText(text);
    btn.textContent = '已复制';
  } catch {
    input.focus();
    input.select();
    document.execCommand('copy');
    btn.textContent = '已复制';
  }
});
```

`input.css`：

```css
.token-box { @apply flex flex-col gap-2; }
.token-row { @apply flex flex-col gap-2 sm:flex-row; }
.token-value { @apply font-mono text-sm; }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web -run 'TestGenerateMCPToken|TestRegenerateInvalidates|TestRevokeMCPToken|TestChangePasswordRevokes' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/settings_account.go internal/web/mcp_token_handlers.go \
  internal/web/mcp_token_handlers_test.go internal/web/server.go \
  web/templates/settings.html web/static/app.js web/static/input.css
git commit -m "feat(web): generate and revoke MCP access tokens in settings"
```

---

### Task 13: MCP server 骨架、Bearer 鉴权、`/mcp` 挂载与 `list_notebooks`

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/mcpserver/server.go`
- Create: `internal/mcpserver/types.go`
- Create: `internal/mcpserver/tools_notebooks.go`
- Create: `internal/mcpserver/testhelp_test.go`
- Create: `internal/mcpserver/server_test.go`
- Create: `internal/mcpserver/tools_notebooks_test.go`
- Create: `internal/web/mcp_mount_test.go`
- Modify: `internal/web/server.go`
- Modify: `internal/web/middleware.go`

**Interfaces:**
- Consumes: `Store.UserByMCPToken`、`TouchMCPToken`（Task 11）；`Store.ListNotebooks`。
- Produces:
  ```go
  package mcpserver

  type Dictionary interface {
      Lookup(ctx context.Context, word string) (dict.Entry, error)
      Examples(ctx context.Context, word, exchange string, limit int) ([]dict.Sentence, error)
  }
  type Config struct {
      Store    *store.Store
      Dict     Dictionary
      Location *time.Location
      Version  string
      Now      func() time.Time
  }
  func NewHandler(cfg Config) http.Handler

  type NotebookInfo struct {
      ID        int64     `json:"id"`
      Name      string    `json:"name"`
      IsDefault bool      `json:"is_default"`
      WordCount int       `json:"word_count"`
      CreatedAt time.Time `json:"created_at"`
      UpdatedAt time.Time `json:"updated_at"`
  }
  func notebookInfo(nb store.Notebook) NotebookInfo
  ```
- `internal/web.Server` 增加 `mcp http.Handler`。

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/modelcontextprotocol/go-sdk@v1.8.0`
Expected: `go.mod` 出现 `github.com/modelcontextprotocol/go-sdk v1.8.0`，并带入 `github.com/google/jsonschema-go v0.4.3` 等间接依赖。

- [ ] **Step 2: Write the failing tests**

`testhelp_test.go`：

```go
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

type testEnv struct {
	Store    *store.Store
	User     store.User
	Notebook store.Notebook
	Token    string
	URL      string
	Dict     *fakeDict
	Session  *mcp.ClientSession
}

type fakeDict struct {
	entries  map[string]dict.Entry
	examples []dict.Sentence
}

func (f *fakeDict) Lookup(_ context.Context, word string) (dict.Entry, error) {
	e, ok := f.entries[strings.ToLower(word)]
	if !ok {
		return dict.Entry{}, dict.ErrNotFound
	}
	return e, nil
}

func (f *fakeDict) Examples(_ context.Context, _, _ string, limit int) ([]dict.Sentence, error) {
	out := f.examples
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithLocation(t, time.UTC)
}

func newTestEnvWithLocation(t *testing.T, loc *time.Location) *testEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	u, nb, err := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := auth.NewPrefixedToken("mcp_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceMCPToken(ctx, u.ID, hash); err != nil {
		t.Fatal(err)
	}
	d := &fakeDict{entries: map[string]dict.Entry{}}
	h := NewHandler(Config{Store: st, Dict: d, Location: loc, Version: "test", Now: time.Now})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             ts.URL,
			HTTPClient:           &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: raw}},
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &testEnv{Store: st, User: u, Notebook: nb, Token: raw, URL: ts.URL, Dict: d, Session: session}
}

func (e *testEnv) call(t *testing.T, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := e.Session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decodeOut[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", toolText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
```

`server_test.go`：

```go
func TestMCPListNotebooks(t *testing.T) {
	env := newTestEnv(t)
	out := decodeOut[ListNotebooksOutput](t, env.call(t, "list_notebooks", nil))
	if len(out.Notebooks) != 1 || out.Notebooks[0].Name != "默认生词本" {
		t.Fatalf("out=%+v", out)
	}
}

func TestMCPRequiresBearerToken(t *testing.T) {
	env := newTestEnv(t)
	for _, token := range []string{"", "wrong"} {
		status := postMCPStatus(t, env.URL, token)
		if status != http.StatusUnauthorized {
			t.Fatalf("token %q status=%d", token, status)
		}
	}
}

func TestMCPRejectsRevokedToken(t *testing.T) {
	env := newTestEnv(t)
	if err := env.Store.RevokeMCPToken(context.Background(), env.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Session.ListTools(context.Background(), nil); err == nil {
		t.Fatal("revoked token should fail")
	}
}

func TestMCPUserIsolation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	u2, _, _ := env.Store.CreateUser(ctx, "child", "hash", "孩子的本")
	raw, hash, _ := auth.NewPrefixedToken("mcp_", 32)
	_, _ = env.Store.ReplaceMCPToken(ctx, u2.ID, hash)
	sess, err := mcp.NewClient(&mcp.Implementation{Name: "test2", Version: "1"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{
			Endpoint:             env.URL,
			HTTPClient:           &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: raw}},
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: "list_notebooks"})
	if err != nil {
		t.Fatal(err)
	}
	out := decodeOut[ListNotebooksOutput](t, res)
	if len(out.Notebooks) != 1 || out.Notebooks[0].Name != "孩子的本" {
		t.Fatalf("isolation broken: %+v", out)
	}
}

func postMCPStatus(t *testing.T, url, token string) int {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
```

`tools_notebooks_test.go`：

```go
func TestMCPListNotebooks(t *testing.T) { /* 如与 server_test.go 重名，保留一个 */ }
```

`internal/web/mcp_mount_test.go`：

```go
func TestWebMountsMCPWithoutSessionOrCSRF(t *testing.T) {
	tc := newAuthedTestClient(t)
	raw, hash, err := auth.NewPrefixedToken("mcp_", 32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tc.store.ReplaceMCPToken(context.Background(), tc.user.ID, hash); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(tc.srv.Handler())
	defer ts.Close()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "mount", Version: "1"}, nil).
		Connect(context.Background(), &mcp.StreamableClientTransport{
			Endpoint:             ts.URL + "/mcp",
			HTTPClient:           &http.Client{Transport: bearerTransportForTest{token: raw}},
			DisableStandaloneSSE: true,
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// No token must be 401, not a 303 redirect to /login.
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}
```

（`bearerTransportForTest` 可以复用 `mcpserver` 测试里的同名 helper 思路；web 包内重新定义一个最小的 `http.RoundTripper`，不要跨包引用测试代码。）

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/mcpserver -v`
Expected: FAIL，包/函数不存在。
Run: `go test ./internal/web -run TestWebMountsMCP -v`
Expected: FAIL，`/mcp` 被会话中间件重定向。

- [ ] **Step 4: Implement `internal/mcpserver`**

`types.go`：`NotebookInfo` + `notebookInfo`；`GroupInfo`、`WordInfo`、`ExampleInfo` 可以先定义，Task 14–16 使用：

```go
type GroupInfo struct {
    ID         int64     `json:"id"`
    NotebookID int64     `json:"notebook_id"`
    Name       string    `json:"name"`
    WordCount  int       `json:"word_count"`
    CreatedAt  time.Time `json:"created_at"`
}
type ExampleInfo struct {
    EN     string `json:"en"`
    ZH     string `json:"zh"`
    Source string `json:"source"`
}
type WordInfo struct {
    ID             int64         `json:"id"`
    NotebookID     int64         `json:"notebook_id"`
    Word           string        `json:"word"`
    Phonetic       string        `json:"phonetic,omitempty"`
    Translation    string        `json:"translation,omitempty"`
    Definition     string        `json:"definition,omitempty"`
    Examples       []ExampleInfo `json:"examples"`
    Proficiency    int           `json:"proficiency"`
    AddedAt        time.Time     `json:"added_at"`
    LastReviewedAt *time.Time    `json:"last_reviewed_at,omitempty"`
    ReviewCount    int           `json:"review_count"`
    Groups         []GroupInfo   `json:"groups"`
}
func groupInfo(g store.WordGroup) GroupInfo
func wordInfo(w store.Word, groups []store.WordGroup) WordInfo
func examplesInfo(in []store.Example) []ExampleInfo
```

`server.go`：

```go
func NewHandler(cfg Config) http.Handler {
    if cfg.Now == nil { cfg.Now = time.Now }
    if cfg.Location == nil { cfg.Location = time.Local }

    verifier := func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
        u, err := cfg.Store.UserByMCPToken(ctx, auth.HashToken(token))
        if errors.Is(err, store.ErrNotFound) {
            return nil, sdkauth.ErrInvalidToken
        }
        if err != nil {
            return nil, err
        }
        if err := cfg.Store.TouchMCPToken(ctx, u.ID, cfg.Now()); err != nil {
            slog.Warn("touch mcp token", "user_id", u.ID, "err", err)
        }
        return &sdkauth.TokenInfo{
            UserID: strconv.FormatInt(u.ID, 10),
            Extra:  map[string]any{"user_id": u.ID},
        }, nil
    }

    var servers sync.Map
    getServer := func(r *http.Request) *mcp.Server {
        info := sdkauth.TokenInfoFromContext(r.Context())
        if info == nil {
            return nil
        }
        userID, err := strconv.ParseInt(info.UserID, 10, 64)
        if err != nil {
            return nil
        }
        if v, ok := servers.Load(userID); ok {
            return v.(*mcp.Server)
        }
        s := newServer(cfg, userID)
        actual, _ := servers.LoadOrStore(userID, s)
        return actual.(*mcp.Server)
    }

    h := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
        Stateless:                  true,
        JSONResponse:               true,
        DisableLocalhostProtection: true,
    })
    return sdkauth.RequireBearerToken(verifier, &sdkauth.RequireBearerTokenOptions{
        AllowMissingExpiration: true,
    })(h)
}

func newServer(cfg Config, userID int64) *mcp.Server {
    s := mcp.NewServer(&mcp.Implementation{
        Name: "study-words", Title: "少儿背单词", Version: cfg.Version,
    }, &mcp.ServerOptions{
        Instructions: "所有操作都作用于当前访问令牌对应的账号。时间按服务器时区。需要学习分析时，请用 query_recall_logs 获取原始记录后自行计算。",
    })
    registerNotebookTools(s, cfg, userID)
    registerWordTools(s, cfg, userID)
    registerGroupTools(s, cfg, userID)
    registerRecallTools(s, cfg, userID)
    return s
}
```

Task 13 只实现 `registerNotebookTools` 里的 `list_notebooks`；其余 register 函数先留空（空函数体），Task 14–16 填充。空函数不要 panic。

`tools_notebooks.go`：

```go
type ListNotebooksOutput struct {
    Notebooks []NotebookInfo `json:"notebooks"`
}

func registerNotebookTools(s *mcp.Server, cfg Config, userID int64) {
    mcp.AddTool(s, &mcp.Tool{
        Name:        "list_notebooks",
        Description: "列出当前账号的全部生词本，包含词数、默认标记和创建/更新时间。",
    }, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListNotebooksOutput, error) {
        nbs, err := cfg.Store.ListNotebooks(ctx, userID)
        if err != nil {
            return nil, ListNotebooksOutput{}, err
        }
        out := ListNotebooksOutput{Notebooks: make([]NotebookInfo, 0, len(nbs))}
        for _, nb := range nbs {
            out.Notebooks = append(out.Notebooks, notebookInfo(nb))
        }
        return nil, out, nil
    })
}
```

- [ ] **Step 5: Wire into web**

`internal/web/server.go`：
- `Server` 加 `mcp http.Handler`。
- 在 `New` 里，结构体字面量之后、`mux := s.routes(staticFS)` 之前赋值（`routes` 会读取 `s.mcp`）：
  ```go
  s.mcp = mcpserver.NewHandler(mcpserver.Config{
      Store: opts.Store, Dict: opts.Dict, Location: opts.Location, Version: opts.Version, Now: opts.Now,
  })
  ```
  注意 `opts.Dict` 是 `web.Dictionary`，其方法集包含 `mcpserver.Dictionary` 需要的 `Lookup` 和 `Examples`，接口赋值成立；`web` 包需要新增对 `internal/mcpserver` 的 import。
- `routes` 里 `mux.Handle("/mcp", s.mcp)`。

`internal/web/middleware.go`：
- `publicPath` 加 `case "/mcp":`。
- `csrfMiddleware` 在方法 switch 之前：
  ```go
  if r.URL.Path == "/mcp" {
      next.ServeHTTP(w, r)
      return
  }
  ```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/mcpserver -v && go test ./internal/web -run TestWebMountsMCP -v`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/mcpserver internal/web/server.go internal/web/middleware.go internal/web/mcp_mount_test.go
git commit -m "feat(mcp): mount bearer-authenticated MCP server at /mcp"
```

---

### Task 14: 生词本与单词工具

**Files:**
- Modify: `internal/store/words.go`
- Modify: `internal/store/words_test.go`
- Modify: `internal/mcpserver/types.go`
- Modify: `internal/mcpserver/tools_notebooks.go`
- Create: `internal/mcpserver/tools_words.go`
- Create: `internal/mcpserver/tools_notebooks_test.go`
- Create: `internal/mcpserver/tools_words_test.go`
- Modify: `internal/mcpserver/server.go`（若 register 调用已在 Task 13 写好则不改）

**Interfaces:**
- Consumes: `Store.CreateNotebook`、`RenameNotebook`、`NotebookByID`、`ListWords`、`WordByID`、`DeleteWord`、`CachedExamples`；`WordGroupByID`、`GroupWordIDs`、`GroupMemberships`；`Dictionary.Lookup/Examples`。
- Produces:
  ```go
  func (s *Store) WordByText(ctx context.Context, notebookID int64, word string) (Word, error)

  type CreateNotebookInput struct { Name string `json:"name"` }
  type RenameNotebookInput struct { NotebookID int64 `json:"notebook_id"`; Name string `json:"name"` }
  type NotebookOutput struct { Notebook NotebookInfo `json:"notebook"` }

  type ListWordsInput struct {
      NotebookID int64  `json:"notebook_id"`
      GroupID    int64  `json:"group_id,omitempty"`
      Sort       string `json:"sort,omitempty"`
      Limit      int    `json:"limit,omitempty"`
      Offset     int    `json:"offset,omitempty"`
  }
  type ListWordsOutput struct {
      Words      []WordInfo `json:"words"`
      Total      int        `json:"total"`
      HasMore    bool       `json:"has_more"`
      NextOffset int        `json:"next_offset,omitempty"`
  }

  type AddWordInput struct { NotebookID int64 `json:"notebook_id"`; Word string `json:"word"` }
  type AddWordOutput struct { Word WordInfo `json:"word"` }

  type RemoveWordInput struct { NotebookID int64 `json:"notebook_id"`; Word string `json:"word"` }
  type RemoveWordOutput struct { Removed bool `json:"removed"`; Word string `json:"word"`; Groups []GroupInfo `json:"groups"` }
  ```

- [ ] **Step 1: Write the failing tests**

`internal/store/words_test.go`：

```go
func TestWordByTextCaseInsensitive(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	w, _ := st.AddWord(ctx, nb.ID, Word{Word: "Apple"})
	got, err := st.WordByText(ctx, nb.ID, " apple ")
	if err != nil || got.ID != w.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := st.WordByText(ctx, nb.ID, "pear"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
```

`internal/mcpserver/tools_notebooks_test.go`：

```go
func TestMCPCreateAndRenameNotebook(t *testing.T) {
	env := newTestEnv(t)
	created := decodeOut[NotebookOutput](t, env.call(t, "create_notebook", map[string]any{"name": "动物"}))
	if created.Notebook.Name != "动物" || created.Notebook.IsDefault {
		t.Fatalf("created=%+v", created)
	}
	renamed := decodeOut[NotebookOutput](t, env.call(t, "rename_notebook", map[string]any{
		"notebook_id": created.Notebook.ID, "name": "宠物",
	}))
	if renamed.Notebook.Name != "宠物" {
		t.Fatalf("renamed=%+v", renamed)
	}
	res := env.call(t, "create_notebook", map[string]any{"name": "宠物"})
	if !res.IsError {
		t.Fatal("duplicate should be a tool error")
	}
}
```

`internal/mcpserver/tools_words_test.go`：

```go
func TestMCPListWordsFilterSortPaginate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	apple, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple", Proficiency: 3})
	pear, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "pear", Proficiency: 7})
	if _, err := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "cat", Proficiency: 9}); err != nil {
		t.Fatal(err)
	}
	g, _ := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "动物")
	_ = env.Store.SetGroupWords(ctx, env.User.ID, g.ID, []int64{apple.ID, pear.ID})

	all := decodeOut[ListWordsOutput](t, env.call(t, "list_words", map[string]any{"notebook_id": env.Notebook.ID, "sort": "proficiency"}))
	if all.Total != 3 || all.Words[0].Word != "apple" || all.Words[2].Word != "cat" {
		t.Fatalf("all=%+v", all)
	}
	page := decodeOut[ListWordsOutput](t, env.call(t, "list_words", map[string]any{
		"notebook_id": env.Notebook.ID, "limit": 1, "offset": 1,
	}))
	if page.Total != 3 || len(page.Words) != 1 || !page.HasMore || page.NextOffset != 2 {
		t.Fatalf("page=%+v", page)
	}
	group := decodeOut[ListWordsOutput](t, env.call(t, "list_words", map[string]any{
		"notebook_id": env.Notebook.ID, "group_id": g.ID,
	}))
	if group.Total != 2 {
		t.Fatalf("group=%+v", group)
	}
}

func TestMCPAddAndRemoveWord(t *testing.T) {
	env := newTestEnv(t)
	env.Dict.entries["apple"] = dict.Entry{Word: "apple", Phonetic: "/ˈæpl/", Translation: "n. 苹果", Exchange: "p:apples"}
	env.Dict.examples = []dict.Sentence{{EN: "An apple.", ZH: "一个苹果。"}}

	added := decodeOut[AddWordOutput](t, env.call(t, "add_word", map[string]any{
		"notebook_id": env.Notebook.ID, "word": "Apple",
	}))
	if added.Word.Word != "apple" || added.Word.Phonetic != "/ˈæpl/" || len(added.Word.Examples) != 1 {
		t.Fatalf("added=%+v", added)
	}
	if res := env.call(t, "add_word", map[string]any{"notebook_id": env.Notebook.ID, "word": "apple"}); !res.IsError {
		t.Fatal("duplicate should error")
	}

	g, _ := env.Store.CreateWordGroup(context.Background(), env.Notebook.ID, "水果")
	_ = env.Store.SetGroupWords(context.Background(), env.User.ID, g.ID, []int64{added.Word.ID})
	removed := decodeOut[RemoveWordOutput](t, env.call(t, "remove_word", map[string]any{
		"notebook_id": env.Notebook.ID, "word": "APPLE",
	}))
	if !removed.Removed || removed.Word != "apple" || len(removed.Groups) != 1 {
		t.Fatalf("removed=%+v", removed)
	}
}

func TestMCPAddWordDictionaryMiss(t *testing.T) {
	env := newTestEnv(t)
	res := env.call(t, "add_word", map[string]any{"notebook_id": env.Notebook.ID, "word": "zzz"})
	if !res.IsError {
		t.Fatal("dictionary miss should error")
	}
}

func TestMCPWordToolsRejectForeignNotebook(t *testing.T) {
	env := newTestEnv(t)
	other, _ := env.Store.CreateNotebook(context.Background(), env.User.ID, "另一本")
	g, _ := env.Store.CreateWordGroup(context.Background(), other.ID, "动物")
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_words", map[string]any{"notebook_id": env.Notebook.ID, "group_id": g.ID}},
		{"add_word", map[string]any{"notebook_id": 99999, "word": "apple"}},
		{"remove_word", map[string]any{"notebook_id": 99999, "word": "apple"}},
	} {
		if res := env.call(t, call.name, call.args); !res.IsError {
			t.Errorf("%s should reject foreign notebook", call.name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mcpserver -run 'TestMCPCreate|TestMCPListWords|TestMCPAdd|TestMCPWordTools|TestMCPRemove' -v && go test ./internal/store -run TestWordByText -v`
Expected: FAIL，工具/方法未定义。

- [ ] **Step 3: Implement `WordByText` and tool helpers**

`WordByText`：

```go
func (s *Store) WordByText(ctx context.Context, notebookID int64, text string) (Word, error) {
    return scanWord(s.db.QueryRowContext(ctx,
        `SELECT `+wordColumns+` FROM words WHERE notebook_id = ? AND word = ? COLLATE NOCASE`,
        notebookID, strings.TrimSpace(text)))
}
```

`types.go` 的 `wordInfo` / `groupInfo` / `examplesInfo`：把 `store.Word` 的字段逐项复制；`Examples`、`Groups` 初始化为 `[]`，不要 `nil`。

`tools_notebooks.go` 增加 `create_notebook`、`rename_notebook`：

- 名称校验：`utf8.RuneCountInString(strings.TrimSpace(name))` 为 1–40。
- `create_notebook`：`cfg.Store.CreateNotebook(ctx, userID, name)`；`ErrDuplicate` → `fmt.Errorf("已经有同名的生词本了")`；输出 `NotebookOutput{notebookInfo(nb)}`。
- `rename_notebook`：`cfg.Store.RenameNotebook(ctx, userID, id, name)`；`ErrDuplicate` / `ErrNotFound` 转中文错误；输出 `NotebookOutput`。

`tools_words.go`：

- `registerWordTools` 注册 `list_words`、`add_word`、`remove_word`。
- `list_words`：
  1. `nb, err := cfg.Store.NotebookByID(ctx, userID, input.NotebookID)`；`ErrNotFound` → "生词本不存在"。
  2. `sort := input.Sort`；空 → "added"；只接受 `added`/`proficiency`/`reviewed`，否则错误。
  3. `words, err := cfg.Store.ListWords(ctx, nb.ID, sort)`。
  4. 若 `input.GroupID != 0`：`g, err := cfg.Store.WordGroupByID(ctx, userID, input.GroupID)`；`g.NotebookID != nb.ID` → "分组不在这个生词本里"；`ids, _ := cfg.Store.GroupWordIDs(ctx, userID, g.ID)`；用集合过滤 `words`。
  5. `memberships, _ := cfg.Store.GroupMemberships(ctx, nb.ID)`。
  6. `limit`：`<=0` → 200；`>500` → 500；`offset < 0` → 0。
  7. `total := len(words)`；`page := words[min(offset,len):min(offset+limit,len)]`；`has_more := offset+len(page) < total`；`next_offset := offset+len(page)`（仅 has_more 时非 0）。
  8. 输出 `[]WordInfo`，每个词带 `memberships[w.ID]`。
- `add_word`：
  1. 校验 `NotebookID` 归属；`word := strings.TrimSpace(input.Word)`，1–100 runes。
  2. `cfg.Dict == nil` → "词典未加载。"
  3. `entry, err := cfg.Dict.Lookup(ctx, word)`；`dict.ErrNotFound` → `fmt.Errorf("词典里没有「%s」", word)`；其他错误原样返回。
  4. 例句：`cached, ok, err := cfg.Store.CachedExamples(ctx, entry.Word)`；`err == nil && ok && len(cached)>0` 用它；否则 `cfg.Dict.Examples(ctx, entry.Word, entry.Exchange, 2)` 转 `[]store.Example`；错误忽略（保持网页行为：例句拿不到也能加词）。
  5. `cfg.Store.AddWord(ctx, nb.ID, store.Word{Word: entry.Word, Phonetic: entry.Phonetic, Translation: entry.Translation, Definition: entry.Definition, Examples: examples})`；`ErrDuplicate` → "「X」已在这个生词本里"。
  6. 输出 `AddWordOutput{wordInfo(w, nil)}`（新词不属于任何组，`Groups: []`）。
- `remove_word`：
  1. 校验 notebook；`w, err := cfg.Store.WordByText(ctx, nb.ID, input.Word)`；`ErrNotFound` → "生词本里没有「X」"。
  2. `memberships, _ := cfg.Store.GroupMemberships(ctx, nb.ID)`；`groups := memberships[w.ID]`。
  3. `cfg.Store.DeleteWord(ctx, nb.ID, w.ID)`。
  4. 输出 `RemoveWordOutput{Removed: true, Word: w.Word, Groups: []GroupInfo}`。

所有工具用 `mcp.AddTool[In, Out]`，输入字段带 `json` 和 `jsonschema` 描述标签，例如：

```go
type AddWordInput struct {
    NotebookID int64  `json:"notebook_id" jsonschema:"生词本 ID"`
    Word       string `json:"word" jsonschema:"要加入的英文单词"`
}
```

错误直接 `return nil, Out{}, fmt.Errorf("...")`；SDK 会设置 `isError`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcpserver -run 'TestMCPCreate|TestMCPListWords|TestMCPAdd|TestMCPWordTools|TestMCPRemove' -v && go test ./internal/store -run TestWordByText -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/store/words.go internal/store/words_test.go internal/mcpserver \
git commit -m "feat(mcp): add notebook and word tools"
```

---

### Task 15: 分组工具

**Files:**
- Create: `internal/mcpserver/tools_groups.go`
- Create: `internal/mcpserver/tools_groups_test.go`
- Modify: `internal/mcpserver/server.go`（若 register 调用已在 Task 13 写好则不改）

**Interfaces:**
- Consumes: `ListWordGroups`、`WordGroupByID`、`CreateWordGroup`、`RenameWordGroup`、`DeleteWordGroup`、`GroupWordIDs`、`SetGroupWords`；`ListWords`。
- Produces:
  ```go
  type ListGroupsInput struct { NotebookID int64 `json:"notebook_id"` }
  type ListGroupsOutput struct { Groups []GroupInfo `json:"groups"` }

  type GetGroupInput struct { GroupID int64 `json:"group_id"`; Limit int `json:"limit,omitempty"`; Offset int `json:"offset,omitempty"` }
  type GetGroupOutput struct {
      Group      GroupInfo  `json:"group"`
      Words      []WordInfo `json:"words"`
      Total      int        `json:"total"`
      HasMore    bool       `json:"has_more"`
      NextOffset int        `json:"next_offset,omitempty"`
  }

  type CreateGroupInput struct { NotebookID int64 `json:"notebook_id"`; Name string `json:"name"` }
  type CreateGroupOutput struct { Group GroupInfo `json:"group"` }

  type RenameGroupInput struct { GroupID int64 `json:"group_id"`; Name string `json:"name"` }
  type RenameGroupOutput struct { Group GroupInfo `json:"group"` }

  type DeleteGroupInput struct { GroupID int64 `json:"group_id"` }
  type DeleteGroupOutput struct { Deleted bool `json:"deleted"`; Name string `json:"name"` }

  type GroupWordsInput struct { GroupID int64 `json:"group_id"`; Words []string `json:"words"` }
  type AddGroupWordsOutput struct {
      Added          []string `json:"added"`
      AlreadyPresent []string `json:"already_present"`
      NotFound       []string `json:"not_found"`
  }
  type RemoveGroupWordsOutput struct {
      Removed    []string `json:"removed"`
      NotPresent []string `json:"not_present"`
      NotFound   []string `json:"not_found"`
  }
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestMCPGroupCRUD(t *testing.T) {
	env := newTestEnv(t)
	created := decodeOut[CreateGroupOutput](t, env.call(t, "create_group", map[string]any{
		"notebook_id": env.Notebook.ID, "name": "动物",
	}))
	if created.Group.Name != "动物" || created.Group.NotebookID != env.Notebook.ID {
		t.Fatalf("created=%+v", created)
	}
	listed := decodeOut[ListGroupsOutput](t, env.call(t, "list_groups", map[string]any{"notebook_id": env.Notebook.ID}))
	if len(listed.Groups) != 1 {
		t.Fatalf("listed=%+v", listed)
	}
	renamed := decodeOut[RenameGroupOutput](t, env.call(t, "rename_group", map[string]any{
		"group_id": created.Group.ID, "name": "宠物",
	}))
	if renamed.Group.Name != "宠物" {
		t.Fatalf("renamed=%+v", renamed)
	}
	if res := env.call(t, "create_group", map[string]any{"notebook_id": env.Notebook.ID, "name": "宠物"}); !res.IsError {
		t.Fatal("duplicate group should error")
	}
	deleted := decodeOut[DeleteGroupOutput](t, env.call(t, "delete_group", map[string]any{"group_id": created.Group.ID}))
	if !deleted.Deleted || deleted.Name != "宠物" {
		t.Fatalf("deleted=%+v", deleted)
	}
}

func TestMCPAddAndRemoveGroupWords(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	apple, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	pear, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "pear"})
	g, _ := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "水果")
	_ = env.Store.SetGroupWords(ctx, env.User.ID, g.ID, []int64{pear.ID})

	added := decodeOut[AddGroupWordsOutput](t, env.call(t, "add_group_words", map[string]any{
		"group_id": g.ID, "words": []string{"Apple", "pear", "ghost"},
	}))
	if !slices.Equal(added.Added, []string{"Apple"}) ||
		!slices.Equal(added.AlreadyPresent, []string{"pear"}) ||
		!slices.Equal(added.NotFound, []string{"ghost"}) {
		t.Fatalf("added=%+v", added)
	}
	ids, _ := env.Store.GroupWordIDs(ctx, env.User.ID, g.ID)
	if !slices.Equal(ids, []int64{apple.ID, pear.ID}) {
		t.Fatalf("ids=%v", ids)
	}

	removed := decodeOut[RemoveGroupWordsOutput](t, env.call(t, "remove_group_words", map[string]any{
		"group_id": g.ID, "words": []string{"apple", "cat", "ghost"},
	}))
	if !slices.Equal(removed.Removed, []string{"apple"}) ||
		!slices.Equal(removed.NotPresent, []string{"cat"}) ||
		!slices.Equal(removed.NotFound, []string{"ghost"}) {
		t.Fatalf("removed=%+v", removed)
	}
}

func TestMCPGetGroupPaginates(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	var ids []int64
	for _, name := range []string{"apple", "pear", "cat"} {
		w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: name})
		ids = append(ids, w.ID)
	}
	g, _ := env.Store.CreateWordGroup(ctx, env.Notebook.ID, "动物")
	_ = env.Store.SetGroupWords(ctx, env.User.ID, g.ID, ids)

	out := decodeOut[GetGroupOutput](t, env.call(t, "get_group", map[string]any{
		"group_id": g.ID, "limit": 2, "offset": 0,
	}))
	if out.Total != 3 || len(out.Words) != 2 || !out.HasMore || out.NextOffset != 2 {
		t.Fatalf("out=%+v", out)
	}
}

func TestMCPGroupToolsRejectForeignGroup(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	other, _ := env.Store.CreateNotebook(ctx, env.User.ID, "另一本")
	g, _ := env.Store.CreateWordGroup(ctx, other.ID, "动物")
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"get_group", map[string]any{"group_id": 99999}},
		{"rename_group", map[string]any{"group_id": 99999, "name": "x"}},
		{"delete_group", map[string]any{"group_id": 99999}},
		{"add_group_words", map[string]any{"group_id": 99999, "words": []string{"apple"}}},
		{"remove_group_words", map[string]any{"group_id": 99999, "words": []string{"apple"}}},
	} {
		if res := env.call(t, call.name, call.args); !res.IsError {
			t.Errorf("%s should reject missing group", call.name)
		}
	}
	// 该组属于当前用户，但不在请求指定的生词本里时，list_words 必须拒绝。
	apple, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	_ = apple
	if res := env.call(t, "list_words", map[string]any{"notebook_id": env.Notebook.ID, "group_id": g.ID}); !res.IsError {
		t.Fatal("cross-notebook group filter should error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mcpserver -run 'TestMCPGroup|TestMCPAddAndRemoveGroup|TestMCPGetGroup' -v`
Expected: FAIL，工具未注册。

- [ ] **Step 3: Implement `tools_groups.go`**

- `list_groups`：校验 `NotebookByID`，`ListWordGroups`，输出 `[]GroupInfo`（初始化为空）。
- `get_group`：
  1. `g, err := cfg.Store.WordGroupByID(ctx, userID, input.GroupID)`；`ErrNotFound` → "分组不存在"。
  2. `ids, _ := GroupWordIDs(ctx, userID, g.ID)`；集合。
  3. `words, _ := ListWords(ctx, g.NotebookID, "added")`；过滤集合。
  4. `memberships, _ := GroupMemberships(ctx, g.NotebookID)`。
  5. limit/offset 同 `list_words`（默认 200、最大 500）。
  6. 输出 `GroupInfo` + 分页后的 `[]WordInfo`。
- `create_group`：校验 `NotebookByID`；名称 1–40；`CreateWordGroup`；`ErrDuplicate` → "已经有同名的分组了"。
- `rename_group`：名称 1–40；`RenameWordGroup(ctx, userID, id, name)`；`ErrNotFound` → "分组不存在"；`ErrDuplicate` → "已经有同名的分组了"。
- `delete_group`：`WordGroupByID` 拿名字并校验；`DeleteWordGroup`；输出。
- `add_group_words` / `remove_group_words` 共用 helper：
  ```go
  func resolveGroupWords(ctx context.Context, cfg Config, userID, groupID int64, names []string) (g store.WordGroup, current map[int64]bool, found map[string]store.Word, notFound []string, err error)
  ```
  - `g := WordGroupByID`。
  - `len(names) > 200` → error。
  - 每个名字 trim，1–100 runes。
  - `ListWords(ctx, g.NotebookID, "added")` 建 `lower -> Word`。
  - 去重请求名（保留首次出现顺序）。
  - 分类 `found` / `notFound`。
  - `currentIDs := GroupWordIDs`。
  - 返回。
- `add_group_words`：
  - 对每个请求名：在 `found` 里且 `current[id]` → `already_present`；否则 `added`（并加入新集合）。
  - `newIDs := current ∪ found ids`；`SetGroupWords`。
  - 输出三个切片（初始化为空）。
- `remove_group_words`：
  - `removed`：found 且 current；`not_present`：found 但不在 current；`not_found`。
  - `newIDs := current - removed`；`SetGroupWords`。
  - 输出。
- 所有切片初始化为空，保证 JSON 是 `[]`。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcpserver -run 'TestMCPGroup|TestMCPAddAndRemoveGroup|TestMCPGetGroup' -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/tools_groups.go internal/mcpserver/tools_groups_test.go internal/mcpserver/server.go
git commit -m "feat(mcp): add word group tools"
```

---

### Task 16: 回忆记录查询（store + MCP 工具）

**Files:**
- Create: `internal/store/recall_query.go`
- Create: `internal/store/recall_query_test.go`
- Create: `internal/mcpserver/tools_recall.go`
- Create: `internal/mcpserver/tools_recall_test.go`
- Modify: `internal/mcpserver/server.go`（若 register 调用已在 Task 13 写好则不改）

**Interfaces:**
- Produces:
  ```go
  type RecallLogFilter struct {
      From, To             *time.Time
      Word                 string
      NotebookID           *int64
      GroupID              *int64
      Known                *bool
      ProficiencyMin       *int
      ProficiencyMax       *int
      ReviewCountMin       *int
      ReviewCountMax       *int
      Ascending            bool
      Limit, Offset        int
  }
  func (s *Store) QueryRecallLogs(ctx context.Context, userID int64, f RecallLogFilter) ([]RecallLog, int, error)

  type QueryRecallLogsInput struct {
      From           string `json:"from,omitempty" jsonschema:"起始时间，YYYY-MM-DD（按服务器时区）或 RFC3339"`
      To             string `json:"to,omitempty" jsonschema:"结束时间，YYYY-MM-DD 包含当天整天，或 RFC3339"`
      Word           string `json:"word,omitempty" jsonschema:"单词精确匹配，不区分大小写"`
      NotebookID     int64  `json:"notebook_id,omitempty"`
      GroupID        int64  `json:"group_id,omitempty"`
      Known          *bool  `json:"known,omitempty" jsonschema:"true 只看认识，false 只看不认识，省略表示都要"`
      ProficiencyMin int    `json:"proficiency_min,omitempty" jsonschema:"答后熟练度下限 1-10"`
      ProficiencyMax int    `json:"proficiency_max,omitempty" jsonschema:"答后熟练度上限 1-10"`
      ReviewCountMin int    `json:"review_count_min,omitempty"`
      ReviewCountMax int    `json:"review_count_max,omitempty"`
      Order          string `json:"order,omitempty" jsonschema:"desc（默认）或 asc"`
      Limit          int    `json:"limit,omitempty" jsonschema:"默认 200，最大 1000"`
      Offset         int    `json:"offset,omitempty"`
  }
  type RecallLogRow struct {
      ID               int64     `json:"id"`
      ReviewedAt       time.Time `json:"reviewed_at"`
      Word             string    `json:"word"`
      WordID           *int64    `json:"word_id"`
      NotebookID       *int64    `json:"notebook_id"`
      NotebookName     string    `json:"notebook_name"`
      Known            bool      `json:"known"`
      ProficiencyAfter int       `json:"proficiency_after"`
      ReviewCount      int       `json:"review_count"`
  }
  type QueryRecallLogsOutput struct {
      Logs       []RecallLogRow `json:"logs"`
      Total      int            `json:"total"`
      Returned   int            `json:"returned"`
      HasMore    bool           `json:"has_more"`
      NextOffset int            `json:"next_offset,omitempty"`
  }
  ```

- [ ] **Step 1: Write the failing tests**

`internal/store/recall_query_test.go`：

```go
func TestQueryRecallLogsFiltersAndPagination(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	u, nb, _ := st.CreateFirstUser(ctx, "parent", "hash", "默认生词本")
	other, _ := st.CreateNotebook(ctx, u.ID, "另一本")
	apple, _ := st.AddWord(ctx, nb.ID, Word{Word: "apple"})
	pear, _ := st.AddWord(ctx, other.ID, Word{Word: "pear"})
	g, _ := st.CreateWordGroup(ctx, nb.ID, "水果")
	_ = st.SetGroupWords(ctx, u.ID, g.ID, []int64{apple.ID})

	base := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	insert := func(wordID int64, notebookID int64, word string, known bool, prof, count int, at time.Time) {
		_, err := st.db.ExecContext(ctx, `INSERT INTO recall_logs
			(user_id, notebook_id, notebook_name, word_id, word, known, proficiency_after, review_count, reviewed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, notebookID, "本", wordID, word, boolInt(known), prof, count, formatTime(at))
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(apple.ID, nb.ID, "apple", true, 6, 1, base)
	insert(apple.ID, nb.ID, "apple", false, 4, 2, base.Add(time.Hour))
	insert(pear.ID, other.ID, "pear", true, 8, 3, base.Add(2*time.Hour))
	insert(apple.ID, nb.ID, "apple", true, 9, 4, base.Add(3*time.Hour))

	known := true
	f := RecallLogFilter{Known: &known, Limit: 10}
	logs, total, err := st.QueryRecallLogs(ctx, u.ID, f)
	if err != nil || total != 3 || len(logs) != 3 {
		t.Fatalf("known total=%d len=%d err=%v", total, len(logs), err)
	}
	f = RecallLogFilter{NotebookID: &nb.ID, Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 3 {
		t.Fatalf("notebook total=%d", total)
	}
	f = RecallLogFilter{GroupID: &g.ID, Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 3 {
		t.Fatalf("group total=%d", total)
	}
	minProf, maxProf := 5, 8
	f = RecallLogFilter{ProficiencyMin: &minProf, ProficiencyMax: &maxProf, Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 2 {
		t.Fatalf("proficiency total=%d", total)
	}
	f = RecallLogFilter{Word: "APPLE", Limit: 10}
	_, total, _ = st.QueryRecallLogs(ctx, u.ID, f)
	if total != 3 {
		t.Fatalf("word total=%d", total)
	}
	f = RecallLogFilter{Limit: 2, Offset: 0}
	page1, total, _ := st.QueryRecallLogs(ctx, u.ID, f)
	f.Offset = 2
	page2, _, _ := st.QueryRecallLogs(ctx, u.ID, f)
	if total != 4 || len(page1) != 2 || len(page2) != 2 || page1[0].ID == page2[0].ID {
		t.Fatalf("pagination total=%d p1=%+v p2=%+v", total, page1, page2)
	}
	f = RecallLogFilter{Ascending: true, Limit: 10}
	asc, _, _ := st.QueryRecallLogs(ctx, u.ID, f)
	if len(asc) != 4 || asc[0].ReviewedAt.After(asc[3].ReviewedAt) {
		t.Fatalf("asc=%+v", asc)
	}
}
```

`internal/mcpserver/tools_recall_test.go`：

```go
func TestQueryRecallLogsToolFilters(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	// 通过答题生成两条记录；熟练度从 5 开始，认识 +1、不认识 -1。
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC))
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, false, time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC))

	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"word": "APPLE", "order": "asc",
	}))
	if out.Total != 2 || len(out.Logs) != 2 || out.Logs[0].ProficiencyAfter != 6 || out.Logs[1].ProficiencyAfter != 5 {
		t.Fatalf("out=%+v", out)
	}
	known := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"known": false,
	}))
	if known.Total != 1 || known.Logs[0].Known {
		t.Fatalf("known=%+v", known)
	}
}

func TestQueryRecallLogsDateOnlyToIncludesWholeDay(t *testing.T) {
	env := newTestEnvWithLocation(t, time.FixedZone("CST", 8*3600))
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	// 本地 11 月 15 日 23:30 与 11 月 16 日 00:30，存 UTC。
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 15, 30, 0, 0, time.UTC))
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, false, time.Date(2026, 11, 15, 16, 30, 0, 0, time.UTC))

	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"from": "2026-11-15", "to": "2026-11-15",
	}))
	if out.Total != 1 || !out.Logs[0].Known {
		t.Fatalf("date range=%+v", out)
	}
}

func TestQueryRecallLogsClampsLimitAndOffset(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	for i := 0; i < 3; i++ {
		_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 10+i, 0, 0, 0, time.UTC))
	}
	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{
		"limit": 5000, "offset": -1,
	}))
	if out.Total != 3 || out.Returned != 3 || out.HasMore {
		t.Fatalf("out=%+v", out)
	}
	res := env.call(t, "query_recall_logs", map[string]any{"from": "2026-11-16", "to": "2026-11-15"})
	if !res.IsError {
		t.Fatal("from > to should error")
	}
}

func TestQueryRecallLogsDeletedWordSnapshot(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	w, _ := env.Store.AddWord(ctx, env.Notebook.ID, store.Word{Word: "apple"})
	_, _, _ = env.Store.AnswerWord(ctx, env.Notebook.ID, w.ID, true, time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))
	if err := env.Store.DeleteWord(ctx, env.Notebook.ID, w.ID); err != nil {
		t.Fatal(err)
	}
	out := decodeOut[QueryRecallLogsOutput](t, env.call(t, "query_recall_logs", map[string]any{"word": "apple"}))
	if out.Total != 1 || out.Logs[0].WordID != nil || out.Logs[0].NotebookID == nil {
		t.Fatalf("out=%+v", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store -run TestQueryRecallLogs -v && go test ./internal/mcpserver -run TestQueryRecallLogs -v`
Expected: FAIL，方法/工具未定义。

- [ ] **Step 3: Implement `internal/store/recall_query.go`**

动态拼接 WHERE：

```go
where := []string{"user_id = ?"}
args := []any{userID}
if f.From != nil { where = append(where, "reviewed_at >= ?"); args = append(args, formatTime(*f.From)) }
if f.To != nil { where = append(where, "reviewed_at <= ?"); args = append(args, formatTime(*f.To)) }
if w := strings.TrimSpace(f.Word); w != "" { where = append(where, "LOWER(word) = LOWER(?)"); args = append(args, w) }
if f.NotebookID != nil { where = append(where, "notebook_id = ?"); args = append(args, *f.NotebookID) }
if f.GroupID != nil { where = append(where, "word_id IN (SELECT word_id FROM word_group_members WHERE group_id = ?)"); args = append(args, *f.GroupID) }
if f.Known != nil { where = append(where, "known = ?"); args = append(args, boolInt(*f.Known)) }
if f.ProficiencyMin != nil { where = append(where, "proficiency_after >= ?"); args = append(args, *f.ProficiencyMin) }
// ...max、review_count 同理
```

- `clause := strings.Join(where, " AND ")`。
- `SELECT COUNT(*) FROM recall_logs WHERE ` + clause → total。
- limit：`<=0` → 200；`>1000` → 1000；`offset <0` → 0。
- order：`ASC` if `Ascending` else `DESC`；`ORDER BY reviewed_at <dir>, id <dir>`。
- 数据查询与 `ListRecallLogs` 相同的列和扫描逻辑；可以提取 `scanRecallLogRow` helper，避免复制 `sql.NullInt64` 处理。
- 返回 `([]RecallLog, total, error)`；空结果返回 `[]RecallLog{}`。

- [ ] **Step 4: Implement `tools_recall.go`**

`parseTimeBound(raw string, loc *time.Location, endOfDay bool) (*time.Time, error)`：

- trim；空 → nil, nil。
- 先试 `time.ParseInLocation("2006-01-02", raw, loc)`；成功且 `endOfDay` → `t.Add(24*time.Hour).Add(-time.Nanosecond)`；`u := t.UTC()`。
- 否则 `time.Parse(time.RFC3339, raw)`；失败 → `fmt.Errorf("时间格式应为 YYYY-MM-DD 或 RFC3339：%q", raw)`；`u := t.UTC()`。
- 返回 `&u`。

`query_recall_logs`：

1. `from, err := parseTimeBound(input.From, cfg.Location, false)`；`to` 用 `true`。
2. 如果都非空且 `from.After(*to)` → "起始时间不能晚于结束时间"。
3. `NotebookID != 0` → `NotebookByID` 校验；`GroupID != 0` → `WordGroupByID` 校验。
4. proficiency：0 表示未设置；非 0 时必须在 1–10；min/max 都非 0 且 min > max → 错误。
5. review count：负数 → 错误；min/max 都 >=0 且 min > max → 错误。
6. order：空/`desc` → 倒序；`asc` → 正序；其他 → 错误。
7. limit：`<=0` → 200；`>1000` → 1000；offset `<0` → 0。
8. 调 `cfg.Store.QueryRecallLogs`。
9. 输出：
   ```go
   out := QueryRecallLogsOutput{Logs: make([]RecallLogRow, 0, len(logs)), Total: total, Returned: len(logs)}
   for _, l := range logs { out.Logs = append(out.Logs, recallLogRow(l)) }
   out.HasMore = offset+len(logs) < total
   if out.HasMore { out.NextOffset = offset + len(logs) }
   ```
10. `recallLogRow` 逐字段复制；`WordID`/`NotebookID` 直接传指针。

工具描述要写明：这是原始记录，不做分析；`from`/`to` 语义；`known` 省略表示两者都要；`group_id` 按当前分组成员过滤。

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store -run TestQueryRecallLogs -v && go test ./internal/mcpserver -run TestQueryRecallLogs -v`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/store/recall_query.go internal/store/recall_query_test.go \
  internal/mcpserver/tools_recall.go internal/mcpserver/tools_recall_test.go internal/mcpserver/server.go
git commit -m "feat(mcp): add raw recall log query tool"
```

---

### Task 17: README、样式构建、全量验证

**Files:**
- Modify: `README.md`
- Modify: `web/static/input.css`（补齐遗漏的响应式规则）
- Modify: `web/static/app.css`（`make css` 生成）
- Modify: `docs/superpowers/specs/2026-11-15-word-groups-and-mcp-design.md`（状态改为「已实现」可选）

**Interfaces:**
- Consumes: 所有前序任务。
- Produces: 可发布的单一提交。

- [ ] **Step 1: README 增加两节**

在功能列表中加入「生词分组」「按范围回忆」「MCP 访问」。在配置/使用章节后加入：

```markdown
## 生词分组与回忆范围

在生词本详情页的「分组」区域新建分组，点进组页面勾选本内单词。开始回忆前会进入范围页，可以选择：

- 全部
- 低于指定星级（2–10 级对应低于 1 星到低于 5 星）
- 一个或多个分组

小结页的「再来一轮」沿用上一轮范围，「换个范围」回到范围页。

## MCP 访问

在「设置 → MCP 访问」生成访问令牌。令牌只显示一次，数据库只保存它的哈希；重新生成或撤销后旧令牌立即失效。修改密码也会撤销令牌。

客户端配置示例：

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

Claude Code：

```bash
claude mcp add --transport http study-words https://your-host/mcp \
  --header "Authorization: Bearer mcp_..."
```

说明：

- 令牌是完整账号凭证，只通过 HTTPS 使用，不要提交到仓库或分享。
- 反向代理需要放行 `/mcp` 的 POST；不需要 Cookie。
- 服务端以 stateless + JSON 响应运行，不依赖长连接。
- 提供 `list_notebooks`、`create_notebook`、`rename_notebook`、`list_words`、`add_word`、`remove_word`、`list_groups`、`get_group`、`create_group`、`rename_group`、`delete_group`、`add_group_words`、`remove_group_words`、`query_recall_logs`。
- 学习分析由 Agent 基于 `query_recall_logs` 的原始记录自行完成；系统不内置分析算法。
```

（README 里的嵌套代码块按实际 Markdown 调整，不要把外层 ``` 与内层混淆。）

- [ ] **Step 2: 补齐样式并构建**

检查 `input.css` 是否已有 `.center-actions`、`.select`、`.load-more` 等既有类；只补新增类。运行：

```bash
make css
```

Expected: 生成/更新 `web/static/app.css`，无 Tailwind 报错。

- [ ] **Step 3: 全量验证**

```bash
gofmt -l .
go vet ./...
go test ./...
go build ./...
```

Expected: `gofmt -l .` 无输出；`go vet`、`go test`、`go build` 全部成功。若 `go test ./...` 因环境缺少 `data/dict.db` 失败，确认失败只来自需要词典的集成测试，并用 `go test ./internal/... ./web/...` 复核；本项目现有测试应不依赖 `data/dict.db`。

- [ ] **Step 4: 检查令牌与日志**

```bash
git grep -n "mcp_" -- ':!docs' ':!README.md' ':!internal/store/mcp_tokens_test.go' ':!internal/web/mcp_token_handlers_test.go' || true
git grep -n "Authorization" -- internal || true
```

Expected: 生产代码只在 `internal/auth`、`internal/mcpserver` 的 Bearer 校验和设置页生成处出现 `mcp_`；日志代码不打印 `Authorization`。

- [ ] **Step 5: Commit**

```bash
git add README.md web/static/input.css web/static/app.css
git commit -m "docs: document word groups and MCP access; rebuild styles"
```

---

## Self-Review

**1. Spec coverage**

- §6 数据模型：Task 1（分组表）、Task 11（`mcp_tokens`）。
- §6.1 `RecallScope`：Task 5。
- §7 分组管理：Task 1–4。
- §7.2 生词本页：Task 3。
- §7.3 组页面：Task 4。
- §8 回忆范围：Task 5–9。
- §8.1 范围页：Task 7。
- §8.2 开始一轮：Task 8。
- §8.3 manager：Task 6。
- §8.4 抽词：Task 5、8。
- §8.5 范围显示：Task 8。
- §8.6 小结/重来：Task 9。
- §8.7 入口调整：Task 7。
- §9 导出/导入：Task 10。
- §10.1 令牌：Task 11。
- §10.2 设置页：Task 12。
- §10.3 路由/中间件：Task 13。
- §10.4 MCP server：Task 13。
- §10.5 工具：Task 13–16。
- §10.6 README 示例：Task 17。
- §11 界面：Task 3、4、7、12。
- §12 安全：Task 11–13、17。
- §13 测试：每个任务的测试步骤。
- §14 实施顺序：Phase 1 / Phase 2 对应。
- §16 兼容：Task 10（v1）、Task 8（直接 `/recall/{id}`）、Task 1/11（`IF NOT EXISTS`）。

**2. Step scan**

每个任务都有：失败测试 → 运行失败 → 实现 → 运行通过 → 提交。测试步骤给出测试名和断言；实现步骤给出签名、文件、SQL/关键规则。没有「TBD」「适当处理」之类不决定任何事的步骤。

**3. Type consistency**

- `store.WordGroup` / `WordGroupByID` / `GroupWordIDs` / `GroupMemberships` 在 Task 1–2 定义，Task 3–4、10、15 使用同一签名。
- `store.RecallScope` / `ScopeAll` / `ScopeProficiency` / `ScopeGroups` 在 Task 5 定义，Task 6–9 使用。
- `Manager.Start` 在 Task 6 定义，Task 8–9 使用。
- `parseRecallScope` / `scopeQuery` / `scopeLabel` 在 Task 7 定义，Task 8–9 使用。
- `mcpserver.Config` / `NewHandler` / `NotebookInfo` / `GroupInfo` / `WordInfo` 在 Task 13 定义，Task 14–16 使用。
- `RecallLogFilter` / `QueryRecallLogs` 在 Task 16 定义，工具即时使用。
- `MCPToken` 在 Task 11 定义，Task 12–13 使用。

**4. Review Focus**

五条都在对应任务里有明确测试（见 Review Focus 小节）。

**5. Proportion**

计划约 3700 行，spec 约 850 行。其中大部分是可以直接运行的失败测试（每个任务都要求先写出并运行失败测试）和需要精确复制的 SQL / 模板片段；实现正文只给签名、字段和规则，不写完整函数体。17 个任务每个约 200 行，与「一个任务一次独立 review」的粒度相称。

---

## Execution Handoff

计划已保存到 `docs/superpowers/plans/2026-11-15-word-groups-and-mcp.md`。请先审阅计划；确认后再选择执行方式。

- **Subagent-driven**：每个任务由新的 subagent 实现，再由新的 reviewer 检查，最后做整分支 review。最彻底，适合这种接口多、跨 store/web/MCP 三层的计划。
- **Native**：我在当前会话按计划逐任务实现，最后请一个 reviewer 检查整个分支。更快、更省，但过程中没有独立 review。

推荐 **Subagent-driven**：17 个任务里有 6 个新增包/文件组，接口在前一任务定义、后一任务使用，独立 reviewer 更容易在 Task 13–16 的 MCP 类型和鉴权边界上发现问题。

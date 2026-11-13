# 少儿背单词 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建一个可自托管、手机/平板友好的儿童背单词 Web 应用，包含查词、多生词本、加权回忆、回忆记录、导入导出、单用户登录，并以 MIT 开源发布。

**Architecture:** Go 单进程 `net/http` 服务端渲染 + htmx 局部刷新；数据分两个 SQLite 文件——可写的 `study.db` 与只读的构建产物 `dict.db`（ECDICT + Tatoeba 句对）。Tailwind 在开发期编译为静态 CSS 并内嵌，运行时不依赖 CDN。AI 例句为可选的 OpenAI 兼容接口，结果写入 SQLite 缓存。

**Tech Stack:** Go 1.24+、`modernc.org/sqlite`（纯 Go）、`html/template`、htmx 2.0.4、Tailwind CSS 3.4.17、bcrypt、Docker、GitHub Actions。

**Spec:** `docs/superpowers/specs/2026-11-13-study-words-design.md`

## Global Constraints

- 模块路径 `github.com/yaofeng/kids-study-words`；二进制名 `study-words`；Go 1.24+；`CGO_ENABLED=0`。
- 仅使用纯 Go SQLite 驱动 `modernc.org/sqlite`，不使用 CGO。
- 单用户实例：`users` 为空时只允许 `/setup`；创建首个用户后，`/setup` 永久 404，网页端不提供注册。
- 熟练度整数 1–10，加入时为 5；展示为 5 星、每级 0.5 星。
- 时间一律以 UTC RFC3339 存库；展示与按天分组用 `TZ`（默认 `Asia/Shanghai`）。
- 所有非 GET 请求校验 CSRF；登录/首次设置用 Origin/Referer 同源校验；所有 SQL 参数化。
- 静态资源（htmx、字体、CSS）全部本地提供，不访问外部 CDN。
- 固定外部数据：ECDICT 提交 `9c46923fb81a2eb6c264a3a1689562ea7bc56b6a`，`ecdict.csv` SHA-256 `d1ecbdf9ee2b920afb6f430ca6b6bb9f559588cf0dff86e04bd255f9390843cc`；Tatoeba `cmn-eng.zip` SHA-256 `d606361aa7312dcdfb15ef4fc51cb1e7ed8bc2dc608a7b192626cd36e8403106`。
- htmx 固定 2.0.4，字体固定 `@fontsource/nunito@5.1.0`。
- Docker 运行时非 root（UID/GID 1000），数据目录 `/data`，监听 `8080`。
- 中文查词接口目标 300ms 内返回；英文查词最多 20 条，中文最多 30 条。
- 仓库 `yaofeng/kids-study-words`，MIT；镜像 `${{ secrets.DOCKERHUB_USERNAME }}/kids-study-words`。

## Review Focus

- **导入的文件缺字段、字段越界或超过 10MB**：应整体拒绝，数据库保持导入前状态，并指出具体原因；绝不能半途写入。
- **删除默认生词本或最后一个生词本**：必须维持「恰好一个默认本」；删最后一个应被拒绝，删默认本应自动提升另一个。
- **大小写不同的重复词 / 重复生词本名**：不能产生两条记录，应给出「已在《xx》中」或名称重复的明确提示。
- **快速双击「认识 / 不认识」**：同一个单词只能改一次熟练度、写一条记录；第二次请求应被拒绝或忽略。
- **回忆记录跨本地午夜、以及同一秒多条记录的分页**：按 `TZ` 正确分天，游标分页不丢行、不重复。

---

### Task 1: 项目脚手架、配置与健康检查

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/web/health.go`, `internal/web/health_test.go`
- Create: `cmd/server/main.go`
- Create: `Makefile`, `.gitignore`
- Test: `internal/config/config_test.go`, `internal/web/health_test.go`

**Interfaces:**
- Produces: `config.Config{Port, DataDir, DictPath string; Location *time.Location; CookieSecure bool; AIBaseURL, AIAPIKey, AIModel string}`；`config.Load(getenv func(string) string) (Config, error)`；`web.HealthHandler(w http.ResponseWriter, r *http.Request)`。
- Produces CLI: `study-words`（serve）、`study-words version`、`study-words healthcheck`。

- [ ] **Step 1: 安装 Go 并初始化模块**

```bash
brew install go        # 若 go version 已可用则跳过
go version
cd /Users/fengyao/workspace/popeye/study-words
go mod init github.com/yaofeng/kids-study-words
go get modernc.org/sqlite@latest golang.org/x/crypto/bcrypt golang.org/x/term
git init -b main
```

- [ ] **Step 2: 写配置测试**

```go
func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil { t.Fatal(err) }
	if cfg.Port != "8080" || cfg.DataDir != "./data" || cfg.DictPath != "./data/dict.db" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Location.String() != "Asia/Shanghai" { t.Fatalf("tz = %s", cfg.Location) }
	if cfg.CookieSecure { t.Fatal("cookie secure should default false") }
}

func TestLoadRejectsBadTimezone(t *testing.T) {
	if _, err := Load(func(k string) string { if k == "TZ" { return "Mars/Base" }; return "" }); err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/config -run TestLoad -v`
Expected: FAIL（`undefined: Load`）。

- [ ] **Step 4: 实现 `config.Load` 与健康检查**

`config.Load` 读取环境变量并给默认值；`TZ` 用 `time.LoadLocation`，错误时返回 `fmt.Errorf("invalid TZ: %w", err)`。`web.HealthHandler` 写 `200`、`Content-Type: text/plain`、正文 `ok`。

- [ ] **Step 5: 写健康检查测试**

```go
func TestHealthHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	HealthHandler(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != 200 || rec.Body.String() != "ok" { t.Fatalf("got %d %q", rec.Code, rec.Body.String()) }
}
```

- [ ] **Step 6: 实现 `cmd/server/main.go`**

- `version` 子命令打印 `main.version`（默认 `dev`）。
- `healthcheck` 用 `http.Client{Timeout: 2*time.Second}` 请求 `http://127.0.0.1:<PORT>/healthz`，非 200 退出 1。
- 默认启动 `http.Server{Addr: ":" + cfg.Port, Handler: mux}`，只注册 `GET /healthz`；监听 SIGINT/SIGTERM 优雅退出。
- `main.version` 变量供 `-ldflags -X main.version=...` 注入。

- [ ] **Step 7: 运行通过并提交**

```bash
go test ./...
go build ./...
git add -A && git commit -m "chore: bootstrap Go module, config, and health endpoint"
```

---

### Task 2: 数据模型、用户/会话存储、认证原语与重置密码 CLI

**Files:**
- Create: `internal/store/store.go`, `internal/store/schema.sql`, `internal/store/users.go`, `internal/store/sessions.go`, `internal/store/store_test.go`
- Create: `internal/store/errors.go`
- Create: `internal/auth/auth.go`, `internal/auth/auth_test.go`
- Modify: `cmd/server/main.go`
- Test: `internal/store/store_test.go`, `internal/auth/auth_test.go`

**Interfaces:**
- Consumes: `config.Config`。
- Produces: `store.Open(ctx, path) (*Store, error)`、`(*Store).Close()`、`(*Store).Ping(ctx)`。
- Produces: `store.User{ID int64; Username, PasswordHash string; CreatedAt time.Time}`、`store.Session{TokenHash string; UserID int64; CSRFToken string; CreatedAt, ExpiresAt time.Time}`、`store.Notebook{ID, UserID int64; Name string; IsDefault bool; CreatedAt, UpdatedAt time.Time; WordCount int}`。
- Produces: `CountUsers`、`CreateFirstUser(ctx, username, hash, defaultNotebook string) (User, Notebook, error)`、`UserByUsername`、`UserByID`、`UpdatePassword`、`DeleteSessionsForUser`、`DeleteSessionsForUserExcept`、`CreateSession`、`SessionByToken`、`DeleteSession`、`DeleteExpiredSessions`、`DefaultNotebook(ctx, userID) (Notebook, error)`。
- Produces: `auth.HashPassword(string) (string, error)`、`auth.CheckPassword(hash, pw string) bool`、`auth.NewToken() (raw, hash string, err error)`、`auth.HashToken(raw) string`、`auth.NewCSRFToken() (string, error)`、`auth.ValidUsername(string) bool`、`auth.ValidPassword(string) bool`、`auth.NewLoginLimiter(limit int, window time.Duration, now func() time.Time) *LoginLimiter`、`(*LoginLimiter).Allow(key string) bool`、`(*LoginLimiter).Fail(key string)`、`(*LoginLimiter).Reset(key string)`。
- Produces errors: `store.ErrNotFound`、`store.ErrSetupComplete`、`store.ErrDuplicate`、`store.ErrLastNotebook`。

- [ ] **Step 1: 写存储层测试**

```go
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateFirstUserAndDefaultNotebook(t *testing.T) {
	st := newTestStore(t)
	u, nb, err := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err != nil { t.Fatal(err) }
	if u.ID == 0 || nb.Name != "默认生词本" || !nb.IsDefault { t.Fatalf("bad: %+v %+v", u, nb) }
	if _, _, err := st.CreateFirstUser(context.Background(), "other", "h", "x"); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("expected ErrSetupComplete, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	st := newTestStore(t)
	u, _, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if err := st.CreateSession(context.Background(), "th", u.ID, "csrf", time.Now().UTC(), time.Now().UTC().Add(time.Hour)); err != nil { t.Fatal(err) }
	s, err := st.SessionByToken(context.Background(), "th")
	if err != nil || s.UserID != u.ID || s.CSRFToken != "csrf" { t.Fatalf("got %+v %v", s, err) }
	u2, err := st.UserBySession(context.Background(), "th", time.Now().UTC())
	if err != nil || u2.ID != u.ID { t.Fatalf("got %+v %v", u2, err) }
	if _, err := st.UserBySession(context.Background(), "th", time.Now().UTC().Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session should be rejected: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store -run 'TestCreateFirst|TestSession' -v`
Expected: FAIL（`undefined: Open`）。

- [ ] **Step 3: 实现 schema 与存储层**

`schema.sql` 使用 `embed`，`Open` 先 `os.MkdirAll(filepath.Dir(path), 0o755)`（内存库/无目录时跳过）并检查目录可写，设置 DSN 参数 `_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`、`SetMaxOpenConns(1)`，执行 schema（全部 `CREATE ... IF NOT EXISTS`），并 `Ping`。表结构严格按 spec §7.1，`sessions` 含 `csrf_token`。

`CreateFirstUser` 在事务内 `SELECT COUNT(*)`，非 0 返回 `ErrSetupComplete`；插入用户和默认本后提交。

- [ ] **Step 4: 写认证测试**

```go
func TestPasswordAndTokens(t *testing.T) {
	h, err := HashPassword("secret1")
	if err != nil || !CheckPassword(h, "secret1") || CheckPassword(h, "wrong") { t.Fatal("bcrypt round trip failed") }
	raw, hashed, err := NewToken()
	if err != nil || raw == "" || hashed != HashToken(raw) { t.Fatal("token mismatch") }
}

func TestLoginLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLoginLimiter(5, time.Minute, func() time.Time { return now })
	for i := 0; i < 5; i++ { if !l.Allow("ip") { t.Fatal("should allow") }; l.Fail("ip") }
	if l.Allow("ip") { t.Fatal("should block after 5") }
	now = now.Add(time.Minute)
	if !l.Allow("ip") { t.Fatal("should allow after window") }
}
```

- [ ] **Step 5: 运行测试确认失败并实现**

Run: `go test ./internal/auth -v`
Expected: FAIL；实现 bcrypt cost 12、32 字节随机 token（`crypto/rand` + `base64.RawURLEncoding`）、SHA-256 hex、`crypto/subtle` 比较、滑动窗口限速。用户名 1–32 字符且非空白；密码 6–72 字节。

- [ ] **Step 6: 实现 `reset-password` 子命令**

`study-words reset-password <username>`：打开 store，用 `golang.org/x/term.ReadPassword` 两次读取新密码（非 TTY 时退回 `bufio`），不一致或不足 6 位则退出 1；调用 `UpdatePassword` 和 `DeleteSessionsForUser`，打印成功。测试用 `UpdatePassword` + 新 hash 校验。

- [ ] **Step 7: 运行全部测试并提交**

```bash
go test ./...
git add -A && git commit -m "feat: add sqlite schema, users, sessions, auth primitives"
```

---

### Task 3: Web 骨架、中间件、首次设置与登录

**Files:**
- Create: `internal/web/server.go`, `internal/web/render.go`, `internal/web/middleware.go`, `internal/web/auth_handlers.go`
- Create: `internal/web/templates_test.go`, `internal/web/helpers_test.go`, `internal/web/auth_handlers_test.go`
- Create: `web/templates/layout.html`, `partials.html`, `login.html`, `setup.html`, `error.html`
- Create: `web/static/input.css`, `web/static/app.js`, `web/static/htmx.min.js`, `web/static/fonts/*.woff2`
- Create: `tailwind.config.js`
- Modify: `cmd/server/main.go`
- Test: `internal/web/auth_handlers_test.go`

**Interfaces:**
- Consumes: `store.Store`、`auth`、`config.Config`。
- Produces: `web.Options{Store *store.Store; Location *time.Location; Version string; CookieSecure bool; Now func() time.Time}`；`web.New(opts Options) (*Server, error)`；`(*Server).Handler() http.Handler`。（后续任务按需扩展 Options：Task 7 加 `Dict`，Task 8 在 Server 内建 recall manager，Task 13 加 `AI`。）
- Produces template data base：

```go
type Base struct {
	Title string
	User store.User
	CSRF string
	Active string
	Notebooks []store.Notebook   // Task 4 追加
	DefaultNotebook *store.Notebook // Task 4 追加
	Flash string
	Error string
	Version string
}
```

- Produces test helpers：
  - `newTestServer(t) (*Server, *store.Store)`：临时 SQLite + `web.New`；
  - `newAuthedTestClient(t)`：在 `newTestServer` 基础上完成 setup（用户名 `parent`、密码 `secret1`），返回 `srv`、`store`、`user`、`notebook`、cookie、`csrf`，并提供 `seedWord(notebookID, word, translation) store.Word`；后续任务复用它。

- Produces helpers: `type StarState struct{ Fill string }`（`full`/`half`/`empty`）、`Stars(level int) []StarState`、`StarLabel(level int) string`、`FormatTime(t time.Time) string`、`FormatDate(t time.Time) string`、`Weekday(t time.Time) string`。

- [ ] **Step 1: 写设置/登录流程测试**

```go
func TestSetupCreatesUserAndDefaultNotebook(t *testing.T) {
	srv, st := newTestServer(t)
	form := url.Values{"username": {"parent"}, "password": {"secret1"}, "confirm": {"secret1"}}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther { t.Fatalf("code=%d body=%s", rec.Code, rec.Body) }
	u, err := st.UserByUsername(context.Background(), "parent")
	if err != nil { t.Fatal(err) }
	nb, err := st.DefaultNotebook(context.Background(), u.ID)
	if err != nil || !nb.IsDefault { t.Fatalf("default notebook: %+v %v", nb, err) }
}

func TestSetupDisabledAfterFirstUser(t *testing.T) {
	srv, st := newTestServer(t)
	st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusNotFound { t.Fatalf("code=%d", rec.Code) }
}

func TestPostWithoutCSRFRejected(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postFormNoCSRF("/notebooks", url.Values{"name": {"动物"}})
	if resp.Code != http.StatusForbidden { t.Fatalf("code=%d", resp.Code) }
}

func TestStarsLevels(t *testing.T) {
	if got := Stars(1); got[0].Fill != "half" || got[1].Fill != "empty" { t.Fatalf("%+v", got) }
	if got := Stars(5); got[0].Fill != "full" || got[1].Fill != "full" || got[2].Fill != "half" || got[3].Fill != "empty" { t.Fatalf("%+v", got) }
	if got := Stars(10); got[4].Fill != "full" { t.Fatalf("%+v", got) }
	if StarLabel(5) != "熟练度 2.5 / 5" { t.Fatalf("label=%s", StarLabel(5)) }
}
```

（`newAuthedTestClient` 与 `postFormNoCSRF` 放在 `internal/web/helpers_test.go`，供后续任务复用。）

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/web -run 'TestSetup|TestPostWithoutCSRF' -v`
Expected: FAIL（`undefined: New`）。

- [ ] **Step 3: 实现 Server、渲染器和中间件**

- `New` 校验 store 非空，默认 `Now = time.Now`；`Location` 为空时用 `time.Local`。
- 渲染器：页面解析 `layout.html + partials.html + <page>.html`，片段解析 `partials.html + <fragment>.html`；按名字缓存；`page(w, name, status, data)`、`fragment(w, name, data)`。
- Flash：写 `sw_flash` Cookie（`url.QueryEscape`、HttpOnly、SameSite=Lax、MaxAge 30 秒、Path=/）；页面渲染时 `popFlash(w, r)` 读取并清空；`setFlash(w, msg)` 供重定向后提示。
- 中间件链：recover → 安全头（CSP `default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'`、nosniff、DENY、same-origin）→ 请求日志（slog，不含敏感字段）→ 首次设置门禁 → 会话加载 → CSRF。
- 公开路径：`/login`、`/setup`、`/healthz`、`/static/`。
- 路由：`GET /` 303 `/lookup`（未登录时由会话中间件先跳 `/login`）。
- 无用户时非公开路径 302 `/setup`；有用户但无会话时 302 `/login`；htmx 请求（`HX-Request: true`）未登录返回 401 + `HX-Redirect: /login`。
- 非 GET：若已加载会话，校验 `X-CSRF-Token` 或表单 `csrf_token`；否则校验 Origin/Referer 的 host 等于 `r.Host`，缺失或不匹配返回 403。
- Cookie：`sw_session`，HttpOnly、SameSite=Lax、Path=/、30 天、`Secure=CookieSecure`。

- [ ] **Step 4: 实现 setup/login/logout handler**

- `GET /setup`：`CountUsers()==0` 渲染表单，否则 404。
- `POST /setup`：同源校验；校验用户名/密码/确认；`CreateFirstUser`；创建会话；设置 Cookie；303 `/lookup`。错误重渲染表单并保留用户名。
- `GET /login`：无用户时 303 `/setup`；已登录时 303 `/lookup`。
- `POST /login`：限速（key=客户端 IP）；`UserByUsername`；bcrypt 校验；失败统一提示「用户名或密码不对」并 `Fail`；成功 `Reset`、创建会话、303 `/lookup`。
- `POST /logout`：删除当前会话、清 Cookie、303 `/login`。

- [ ] **Step 5: 创建基础模板与静态资源**

`layout.html` 使用 `{{define "layout"}}`、`{{block "content" .}}`，导航按 `Base.Active` 高亮：查词 / 生词本 / 回忆 / 记录 / 设置；手机底部 Tab、`md:` 以上左侧栏。登录/设置页不显示导航。`partials.html` 提供 `csrf`（隐藏 input）与 `nav` 片段；所有表单包含 `{{template "csrf" .}}`，htmx 的 body 带 `hx-headers='{"X-CSRF-Token":"{{.CSRF}}"}'`。

下载固定资源：

```bash
curl -fsSL https://cdn.jsdelivr.net/npm/htmx.org@2.0.4/dist/htmx.min.js -o web/static/htmx.min.js
for w in 400 600 700 800; do
  curl -fsSL "https://cdn.jsdelivr.net/npm/@fontsource/nunito@5.1.0/files/nunito-latin-${w}-normal.woff2" \
    -o "web/static/fonts/nunito-${w}.woff2"
done
```

`input.css`：`@tailwind base/components/utilities` + `@font-face` + 少量 `@layer components`（`.btn`, `.btn-primary`, `.card`, `.input`, `.tab`）。`tailwind.config.js` 内容路径 `./web/templates/**/*.html`、`./web/static/app.js`，扩展颜色 `cream #FFF8EE`、`ink #3B3A36`、`muted #7A756C`、`teal #4FB3A9`、`teal-dark #237A72`、`coral #FF9F80`、`coral-dark #D9633F`、`sun #FFD966`、`lilac #B9A7E6`，字体 `rounded`，阴影 `sticker: 3px 3px 0 #3B3A36`。

`make css`：`npx --yes tailwindcss@3.4.17 -i web/static/input.css -o web/static/app.css --minify`，生成的 `app.css` 提交进仓库。

`app.js`：仅处理 `data-confirm` 的确认弹窗，无内联脚本。

- [ ] **Step 6: 接入 main 并运行测试**

`main` 打开 store，构造 `web.Options`，`http.Server` 使用 `srv.Handler()`；`/healthz` 同时 `store.Ping`，失败返回 503；静态资源经 `embed` 提供，`/static/` 带 `Cache-Control: max-age=3600`。

Run: `go test ./internal/web -v && go build ./...`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "feat: add web skeleton, setup, login, sessions and CSRF"
```

---

### Task 4: 生词本存储与页面

**Files:**
- Create: `internal/store/notebooks.go`, `internal/store/notebooks_test.go`
- Create: `internal/web/notebook_handlers.go`, `internal/web/notebook_handlers_test.go`
- Create: `web/templates/notebooks.html`
- Modify: `internal/web/server.go`, `cmd/server/main.go`
- Test: `internal/store/notebooks_test.go`, `internal/web/notebook_handlers_test.go`

**Interfaces:**
- Produces: `store.Notebook{ID, UserID int64; Name string; IsDefault bool; CreatedAt, UpdatedAt time.Time; WordCount int}`。
- Produces: `ListNotebooks(ctx, userID) ([]Notebook, error)`、`NotebookByID(ctx, userID, id) (Notebook, error)`、`CreateNotebook(ctx, userID, name) (Notebook, error)`、`RenameNotebook`、`SetDefaultNotebook`、`DeleteNotebook`（`DefaultNotebook` 已在 Task 2 提供）。
- Modifies `Base`：追加 `Notebooks []store.Notebook` 与 `DefaultNotebook *store.Notebook`，并在登录后的页面统一填充，供导航和加词下拉使用。
- Routes: `GET /notebooks`、`POST /notebooks`、`POST /notebooks/{id}/rename`、`POST /notebooks/{id}/default`、`POST /notebooks/{id}/delete`。

- [ ] **Step 1: 写存储不变量测试**

```go
func TestNotebookDefaultInvariants(t *testing.T) {
	st := newTestStore(t)
	u, first, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	second, err := st.CreateNotebook(context.Background(), u.ID, "动物")
	if err != nil { t.Fatal(err) }
	if err := st.SetDefaultNotebook(context.Background(), u.ID, second.ID); err != nil { t.Fatal(err) }
	list, _ := st.ListNotebooks(context.Background(), u.ID)
	defaults := 0
	for _, nb := range list { if nb.IsDefault { defaults++ } }
	if defaults != 1 { t.Fatalf("defaults=%d", defaults) }
	if err := st.DeleteNotebook(context.Background(), u.ID, second.ID); err != nil { t.Fatal(err) }
	def, err := st.DefaultNotebook(context.Background(), u.ID)
	if err != nil || def.ID != first.ID { t.Fatalf("promoted=%+v err=%v", def, err) }
	if err := st.DeleteNotebook(context.Background(), u.ID, first.ID); !errors.Is(err, ErrLastNotebook) {
		t.Fatalf("expected ErrLastNotebook, got %v", err)
	}
}

func TestNotebookNameDuplicateCaseInsensitive(t *testing.T) {
	st := newTestStore(t)
	u, _, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	if _, err := st.CreateNotebook(context.Background(), u.ID, "animals"); err != nil { t.Fatal(err) }
	if _, err := st.CreateNotebook(context.Background(), u.ID, "Animals"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败** → `go test ./internal/store -run TestNotebook -v`，FAIL。

- [ ] **Step 3: 实现存储方法**

- 创建时若用户没有生词本，自动设为默认。
- 设默认：先 `UPDATE notebooks SET is_default=0 WHERE user_id=?`，再设目标（同一事务）。
- 删除：事务内统计数量；只有 1 个返回 `ErrLastNotebook`；若删的是默认本，先清默认、按 `updated_at DESC` 选另一个设为默认，再删。
- 名称唯一用 `UNIQUE (user_id, name)` + 应用层先做 NOCASE 查询判断，重复返回 `ErrDuplicate`。

- [ ] **Step 4: 写页面测试**

```go
func TestCreateNotebookPage(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.postForm("/notebooks", url.Values{"name": {"动物"}, "csrf_token": {tc.csrf}})
	if resp.Code != http.StatusSeeOther { t.Fatalf("code=%d", resp.Code) }
	body := tc.get("/notebooks").Body.String()
	if !strings.Contains(body, "动物") || !strings.Contains(body, "默认") {
		t.Fatalf("body missing notebook or default badge: %s", body)
	}
}
```

- [ ] **Step 5: 实现 handler 与模板**

`GET /notebooks` 渲染卡片：名称、词数、默认徽标、最近更新；每张卡有重命名、设为默认、删除（删除用 `data-confirm`）。`POST` 成功后 303 回 `/notebooks`，错误在页面顶部显示。所有写操作校验生词本归属当前用户。

- [ ] **Step 6: 运行测试并提交**

```bash
go test ./internal/store ./internal/web -v
git add -A && git commit -m "feat: add notebooks with default/delete invariants"
```

---

### Task 5: 单词存储与生词本详情

**Files:**
- Create: `internal/store/words.go`, `internal/store/words_test.go`
- Create: `internal/web/word_handlers.go`, `internal/web/word_handlers_test.go`
- Create: `web/templates/notebook.html`, `web/templates/word_edit.html`
- Modify: `internal/web/server.go`
- Test: `internal/store/words_test.go`, `internal/web/word_handlers_test.go`

**Interfaces:**
- Produces: `store.Example{EN, ZH, Source string}`、`store.Word{ID, NotebookID int64; Word, Phonetic, Translation, Definition string; Examples []Example; Proficiency, ReviewCount int; AddedAt time.Time; LastReviewedAt *time.Time}`。
- Produces: `ListWords(ctx, notebookID int64, sort string) ([]Word, error)`（sort: `added`/`proficiency`/`reviewed`）、`WordByID`、`AddWord(ctx, notebookID int64, w Word) (Word, error)`、`UpdateWord(ctx, notebookID, id int64, translation string, examples []Example) error`、`DeleteWord`（`AnswerWord` 在 Task 8 定义）。
- Routes: `GET /notebooks/{id}`、`GET/POST /notebooks/{id}/words/{wordID}/edit`、`POST /notebooks/{id}/words/{wordID}/delete`。

- [ ] **Step 1: 写单词存储测试**

```go
func TestAddWordDefaults(t *testing.T) {
	st, nb := seedNotebook(t)
	now := time.Now().UTC().Truncate(time.Second)
	w, err := st.AddWord(context.Background(), nb.ID, Word{Word: "apple", Translation: "n. 苹果", Examples: []Example{{EN: "An apple.", ZH: "一个苹果。", Source: "manual"}}})
	if err != nil { t.Fatal(err) }
	if w.Proficiency != 5 || w.AddedAt.IsZero() || w.ReviewCount != 0 { t.Fatalf("bad: %+v", w) }
	if _, err := st.AddWord(context.Background(), nb.ID, Word{Word: "Apple"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestUpdateWordKeepsProgress(t *testing.T) {
	st, nb := seedNotebook(t)
	w, _ := st.AddWord(context.Background(), nb.ID, Word{Word: "apple", Translation: "old"})
	if err := st.UpdateWord(context.Background(), nb.ID, w.ID, "new", []Example{{EN: "E", ZH: "中"}}); err != nil { t.Fatal(err) }
	got, _ := st.WordByID(context.Background(), nb.ID, w.ID)
	if got.Translation != "new" || got.Proficiency != 5 || len(got.Examples) != 1 { t.Fatalf("got %+v", got) }
}
```

- [ ] **Step 2: 运行确认失败** → `go test ./internal/store -run 'TestAddWord|TestUpdateWord' -v`，FAIL。

- [ ] **Step 3: 实现单词存储**

`AddWord`：trim 单词；`proficiency==0` 时置 5；`w.AddedAt` 为零时用 `time.Now().UTC()`；`w.LastReviewedAt` 原样保留（测试/导入用）；`w.ReviewCount` 原样保留；`examples_json` 用 `json.Marshal`；插入前 `SELECT 1 ... WHERE notebook_id=? AND word=? COLLATE NOCASE`，命中返回 `ErrDuplicate`。列表按白名单排序；`reviewed` 用 `last_reviewed_at IS NULL, last_reviewed_at DESC`。编辑只更新 `translation` 与 `examples_json`。增删改词时同步 `UPDATE notebooks SET updated_at=? WHERE id=?`。

- [ ] **Step 4: 写页面测试**

```go
func TestWordEditAndDelete(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "old")
	resp := tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID), url.Values{
		"translation": {"new"}, "csrf_token": {tc.csrf},
		"example_en_1": {"An apple."}, "example_zh_1": {"一个苹果。"},
		"example_en_2": {""}, "example_zh_2": {""},
	})
	if resp.Code != http.StatusSeeOther { t.Fatalf("edit code=%d", resp.Code) }
	if body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String(); !strings.Contains(body, "new") {
		t.Fatal("new translation missing")
	}
	tc.postForm(fmt.Sprintf("/notebooks/%d/words/%d/delete", tc.notebook.ID, w.ID), url.Values{"csrf_token": {tc.csrf}})
	if body := tc.get(fmt.Sprintf("/notebooks/%d", tc.notebook.ID)).Body.String(); strings.Contains(body, "An apple.") {
		t.Fatal("word still listed")
	}
}

func TestWordRoutesRejectCrossNotebookWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	w := tc.seedWord(other.ID, "pear", "n. 梨")
	resp := tc.get(fmt.Sprintf("/notebooks/%d/words/%d/edit", tc.notebook.ID, w.ID))
	if resp.Code != http.StatusNotFound { t.Fatalf("code=%d", resp.Code) }
}
```

- [ ] **Step 5: 实现页面与 handler**

详情页：词表显示英文、释义首行、星级、加入时间、最后回忆时间；排序切换（`?sort=`）；编辑/删除；顶部「开始回忆」链接 `/recall/{id}` 与「添加单词」提示去查词。编辑页：释义 textarea + 例句行（每条 EN/ZH 输入、删除按钮、最多 5 条、空行丢弃、两栏必须同时有值）+ 新增行按钮（`app.js` 克隆模板）。删除需确认。

- [ ] **Step 6: 运行并提交**

```bash
go test ./internal/store ./internal/web -v
git add -A && git commit -m "feat: add words storage, notebook detail, edit and delete"
```

---

### Task 6: 词典构建工具与查询包

**Files:**
- Create: `internal/dictbuild/build.go`, `internal/dictbuild/build_test.go`
- Create: `cmd/dictbuild/main.go`
- Create: `internal/dict/dict.go`, `internal/dict/dict_test.go`, `internal/dict/forms.go`, `internal/dict/forms_test.go`
- Create: `internal/dict/fixture/fixture_test.go`（可选测试辅助）
- Test: 上述 `_test.go`

**Interfaces:**
- Produces: `dictbuild.Build(ecdictCSV, sentencesPath, outPath string) (Stats, error)`，`sentencesPath` 支持 `.txt` 或 `.zip`；`Stats{Entries, Sentences int}`。
- Produces: `dict.Entry{Word, Phonetic, Translation, Definition, POS, Tag, Exchange string; Collins, Oxford, BNC, FRQ int}`、`dict.Sentence{EN, ZH, Source string}`。
- Produces: `dict.Open(path string) (*Dict, error)`、`(*Dict).Close()`、`SearchEnglish(ctx, q string, limit int) ([]Entry, error)`、`SearchChinese(ctx, q string, limit int) ([]Entry, error)`、`Lookup(ctx, word string) (Entry, error)`、`Examples(ctx, word, exchange string, limit int) ([]Sentence, error)`。
- Produces: `dict.Forms(word, exchange string) []string`。

- [ ] **Step 1: 写构建测试**

```go
func TestBuildAndSearch(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "ecdict.csv")
	os.WriteFile(csv, []byte("word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio\n"+
		"apple,/ˈæpl/,a round fruit,n. 苹果,n,3,1,zk,100,200,,,\n"+
		"go,/ɡəʊ/,to move,v. 去,,0,0,zk,10,20,p:went/d:gone/i:going/3:goes,,,\n"+
		"went,/went/,past of go,go的过去式,,0,0,,5000,6000,,,\n"+
		"zebra,/ˈziːbrə/,an animal,n. 斑马,n,1,0,,8000,9000,,,\n"), 0o644)
	tsv := filepath.Join(dir, "cmn.txt")
	os.WriteFile(tsv, []byte("I ate an apple.\t我吃了一个苹果。\tCC-BY\n"+
		"Go home now.\t现在回家。\tCC-BY\n"+
		"He went home.\t他回家了。\tCC-BY\n"+
		"The apple is red.\t苹果是红的。\tCC-BY\n"), 0o644)
	out := filepath.Join(dir, "dict.db")
	if _, err := Build(csv, tsv, out); err != nil { t.Fatal(err) }
	d, err := dict.Open(out)
	if err != nil { t.Fatal(err) }
	defer d.Close()
	es, _ := d.SearchEnglish(context.Background(), "app", 10)
	if len(es) == 0 || es[0].Word != "apple" { t.Fatalf("got %+v", es) }
	cs, _ := d.SearchChinese(context.Background(), "苹果", 10)
	if len(cs) == 0 || cs[0].Word != "apple" { t.Fatalf("got %+v", cs) }
	sent, _ := d.Examples(context.Background(), "go", "p:went/d:gone/i:going/3:goes", 2)
	if len(sent) != 2 { t.Fatalf("got %+v", sent) }
}
```

- [ ] **Step 2: 运行确认失败** → `go test ./internal/dictbuild ./internal/dict -v`，FAIL。

- [ ] **Step 3: 实现 `dictbuild.Build`**

- `encoding/csv` 读 ECDICT（首行表头）；字段映射见 spec §7.2；空整数按 0。
- 创建 `entries`、`sentences`、`sentences_fts`（FTS5，`content='sentences'`）与索引。
- 句对清洗：跳过含制表符/换行、EN 长度 <3 或 >120、ZH 长度 <2 或 >80；按 `lower(en)+"\x00"+zh` 去重；命中强冒犯词黑名单（约 15 个英文词）跳过。
- `sentencesPath` 以 `.zip` 结尾时用 `archive/zip` 读取第一个 `.txt`。
- 批量插入用事务；插入后 `INSERT INTO sentences_fts(rowid,en,zh) SELECT id,en,zh FROM sentences`。
- 打印 `Stats`。

- [ ] **Step 4: 实现查询与词形**

- `SearchEnglish`：先精确 `word = ? COLLATE NOCASE`，再前缀 `word LIKE ? ESCAPE '\' COLLATE NOCASE`；排序 `(word = ? COLLATE NOCASE) DESC, COALESCE(NULLIF(frq,0), 1000000), COALESCE(NULLIF(bnc,0), 1000000), length(word)`；limit 上限 20。
- `SearchChinese`：`translation LIKE ? ESCAPE '\'`，过滤 `bnc>0 OR frq>0 OR tag<>''`，按 frq/bnc 升序、词长升序；limit 上限 30。用 `strings.NewReplacer("\\","\\\\","%","\\%","_","\\_")` 转义。
- `Lookup`：精确；找不到返回 `dict.ErrNotFound`。
- `Forms`：基础词 + `exchange` 中 `p/d/i/3/r/t/s` 的值，去重、非空。
- `Examples`：`sentences_fts MATCH` 查询 `"form1" OR "form2" ...`（双引号转义），取 10 条后在 Go 中优先包含基础词的、再按长度升序，返回前 `limit` 条。`source` 固定 `tatoeba`。

- [ ] **Step 5: 运行测试并提交**

```bash
go test ./internal/dictbuild ./internal/dict -v
go build ./cmd/dictbuild
git add -A && git commit -m "feat: build and query offline dictionary with example sentences"
```

---

### Task 7: 查词页面、例句展示与加入生词本

**Files:**
- Create: `internal/web/lookup_handlers.go`, `internal/web/lookup_handlers_test.go`
- Create: `web/templates/lookup.html`
- Modify: `internal/web/server.go`, `internal/web/word_handlers.go`, `web/templates/partials.html`, `cmd/server/main.go`
- Test: `internal/web/lookup_handlers_test.go`

**Interfaces:**
- Consumes: `dict`（Task 6）、`store.AddWord`（Task 5）。
- Produces `Options.Dict Dictionary` 与接口：

```go
type Dictionary interface {
	SearchEnglish(ctx context.Context, q string, limit int) ([]dict.Entry, error)
	SearchChinese(ctx context.Context, q string, limit int) ([]dict.Entry, error)
	Lookup(ctx context.Context, word string) (dict.Entry, error)
	Examples(ctx context.Context, word, exchange string, limit int) ([]dict.Sentence, error)
}
```

- Test fake `fakeDict{entry dict.Entry; results []dict.Entry; examples []dict.Sentence}`：`Lookup` 返回 `entry`（词不同则 `dict.ErrNotFound`），搜索返回 `results`，`Examples` 返回 `examples`；放在 `helpers_test.go`。

- Produces routes: `GET /lookup`、`GET /lookup/results`、`GET /lookup/examples`、`POST /words`。
- Produces helper: `(*Server).snapshotExamples(ctx, entry dict.Entry) []store.Example`（Task 7 只用本地句对；Task 13 追加 AI 缓存/生成，仍不阻塞加词）。

- [ ] **Step 1: 写查词与加词测试（使用 fake Dictionary）**

```go
func TestLookupEnglishRendersResults(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Phonetic: "/ˈæpl/", Translation: "n. 苹果"}}
	resp := tc.get("/lookup/results?mode=en&q=app")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "apple") || !strings.Contains(resp.Body.String(), "苹果") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAddWordSnapshotsEntry(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.dict = fakeDict{entry: dict.Entry{Word: "apple", Translation: "n. 苹果"}, examples: []dict.Sentence{{EN: "An apple.", ZH: "一个苹果。"}}}
	resp := tc.postForm("/words", url.Values{"notebook_id": {fmt.Sprint(tc.notebook.ID)}, "word": {"apple"}, "csrf_token": {tc.csrf}})
	if resp.Code != http.StatusSeeOther { t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String()) }
	w, err := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if err != nil || len(w) != 1 || w[0].Translation != "n. 苹果" || len(w[0].Examples) != 1 { t.Fatalf("got %+v %v", w, err) }
}

func TestAddWordDuplicateShowsMessage(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	resp := tc.postForm("/words", url.Values{
		"notebook_id": {fmt.Sprint(tc.notebook.ID)}, "word": {"apple"}, "csrf_token": {tc.csrf},
	})
	if resp.Code != http.StatusSeeOther { t.Fatalf("code=%d", resp.Code) }
	body := tc.get("/lookup?mode=en&q=apple").Body.String()
	if !strings.Contains(body, "已在《") { t.Fatalf("flash missing: %s", body) }
	list, _ := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if len(list) != 1 { t.Fatalf("count=%d", len(list)) }
}
```

- [ ] **Step 2: 运行确认失败** → `go test ./internal/web -run 'TestLookup|TestAddWord' -v`，FAIL。

- [ ] **Step 3: 实现查词 handlers**

- `GET /lookup`：`mode=en|zh`（默认 en）、`q`；服务端直接渲染结果（无 q 时显示引导）。
- `GET /lookup/results`：htmx 片段；英文调 `SearchEnglish`，中文调 `SearchChinese`；空结果显示建议。
- 每条结果：单词、音标、释义（按换行拆分）、英文释义、标签（`zk→中考`、`gk→高考`、`cet4→CET4`、`cet6→CET6`）、加入表单、展开例句按钮。
- 例句按钮：`hx-get="/lookup/examples?word=..."`，目标该卡片的例句区，带 `hx-indicator` 文本「正在找例句…」。
- `GET /lookup/examples`：`Lookup` 取 entry；`snapshotExamples`（Task 7 仅本地 + AI 缓存，AI 生成在 Task 13 接入）；无则「暂无例句」。每次最多 2 条。
- `POST /words`：校验 `notebook_id` 属于当前用户；`Lookup` 取词条；构造 `store.Word`（音标、释义、英文释义、`snapshotExamples`）；`AddWord`；`ErrDuplicate` → flash「已在《名称》中，没有重复加入」；成功 → flash「已加入《名称》」；303 回 `/lookup?mode=...&q=...`（表单带 `next` 隐藏字段，校验为站内相对路径）。
- `main` 打开 `cfg.DictPath`；失败时打印 `dict.db not found at <path>; run "make dict" or use the Docker image` 并以非零退出。

- [ ] **Step 4: 实现模板与 htmx**

`lookup.html` 顶部两个 Tab（英→中 / 中→英），输入框 `hx-get="/lookup/results"`、`hx-trigger="input changed delay:250ms"`、`hx-target="#results"`、`hx-include="[name='mode']"`。body 上 `hx-headers='{"X-CSRF-Token":"{{.CSRF}}"}'`。加词表单的 notebook select 预选默认本。结果卡片与例句片段定义在 `partials.html` 的 `lookup_results` / `examples` 命名模板中，页面首次渲染与 htmx 片段共用同一份标记。

- [ ] **Step 5: 运行测试并提交**

```bash
go test ./internal/web -v
git add -A && git commit -m "feat: add lookup UI, examples and add-to-notebook flow"
```

---

### Task 8: 回忆引擎、答题事务与会话状态

**Files:**
- Create: `internal/recall/weight.go`, `internal/recall/weight_test.go`, `internal/recall/manager.go`, `internal/recall/manager_test.go`
- Create: `internal/store/recall.go`, `internal/store/recall_test.go`, `internal/store/recall_logs.go`, `internal/store/recall_logs_test.go`
- Modify: `internal/web/server.go`（初始化 `Manager`）
- Test: 上述 `_test.go`

**Interfaces:**
- Produces: `store.RecallLog{ID, UserID int64; NotebookID, WordID *int64; NotebookName, Word string; Known bool; ProficiencyAfter, ReviewCount int; ReviewedAt time.Time}`。
- Produces: `(*Store).AnswerWord(ctx, notebookID, wordID int64, known bool, now time.Time) (Word, RecallLog, error)`。
- Produces: `(*Store).ListRecallLogs(ctx, userID int64, notebookID *int64, beforeID int64, limit int) ([]RecallLog, error)`（`beforeID=0` 表示最新；`ORDER BY id DESC`）。
- Produces: `recall.Weight(proficiency int, lastReviewed *time.Time, now time.Time) float64`。
- Produces: `recall.Pick(words []store.Word, now time.Time, excludeID int64, rng *rand.Rand) (store.Word, bool)`。
- Produces: `recall.Manager`：`NewManager()`、`Begin(userID, notebookID int64) State`、`Snapshot(userID int64) State`、`SetLastWord(userID, wordID int64)`、`Answer(userID, notebookID, wordID int64, known bool) (State, error)`、`UndoAnswer(userID, wordID int64)`、`Reset(userID int64)`；`State{NotebookID int64; Known, Unknown int; LastWordID, AnsweredWordID int64}`；错误 `recall.ErrAlreadyAnswered`、`recall.ErrWrongNotebook`。

- [ ] **Step 1: 写权重与抽词测试**

```go
func TestWeightMonotonic(t *testing.T) {
	now := time.Now()
	week := now.Add(-7 * 24 * time.Hour)
	if Weight(1, &week, now) <= Weight(10, &week, now) { t.Fatal("lower proficiency must weigh more") }
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-time.Minute)
	if Weight(5, &old, now) <= Weight(5, &recent, now) { t.Fatal("older must weigh more") }
	if Weight(10, &recent, now) <= 0 { t.Fatal("weight must stay positive") }
}

func TestPickExcludesLastWordWhenPossible(t *testing.T) {
	now := time.Now()
	words := []store.Word{{ID: 1}, {ID: 2}}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		w, ok := Pick(words, now, 1, rng)
		if !ok || w.ID != 2 { t.Fatalf("picked %+v ok=%v", w, ok) }
	}
	if _, ok := Pick([]store.Word{{ID: 1}}, now, 1, rng); !ok { t.Fatal("single word must still be pickable") }
}
```

- [ ] **Step 2: 写答题事务与会话测试**

```go
func TestAnswerWordUpdatesAndLogs(t *testing.T) {
	st, nb := seedNotebookWithWord(t, 5)
	now := time.Now().UTC().Truncate(time.Second)
	w, log, err := st.AnswerWord(context.Background(), nb.ID, w.ID, false, now)
	if err != nil { t.Fatal(err) }
	if w.Proficiency != 4 || w.ReviewCount != 1 || w.LastReviewedAt == nil { t.Fatalf("word=%+v", w) }
	if log.Known || log.ProficiencyAfter != 4 || log.NotebookName != nb.Name || log.Word != "apple" { t.Fatalf("log=%+v", log) }
}

func TestManagerDoubleAnswerRejected(t *testing.T) {
	m := NewManager()
	m.Begin(1, 10)
	if _, err := m.Answer(1, 10, 99, true); err != nil { t.Fatal(err) }
	if _, err := m.Answer(1, 10, 99, false); !errors.Is(err, ErrAlreadyAnswered) { t.Fatalf("got %v", err) }
}
```

- [ ] **Step 3: 运行确认失败** → `go test ./internal/recall ./internal/store -run 'TestWeight|TestPick|TestAnswerWord|TestManager' -v`，FAIL。

- [ ] **Step 4: 实现权重与抽词**

```
熟练度因子 = (11 - clamp(p,1,10))^2
时间因子   = clamp(小时数/24, 0.05, 30)，从未回忆 = 30
权重       = 熟练度因子 × 时间因子
```

`Pick`：先按 `excludeID` 过滤（仅当剩余 ≥1 个）；构建前缀和；`roll = rng.Float64()*sum`；返回首个累计值 > roll 的词；空列表返回 `false`。所有词权重恒正。

- [ ] **Step 5: 实现 `AnswerWord` 事务**

事务内：读词与所属生词本名称、`user_id`；`newProf = clamp(prof±1)`；更新 `proficiency`、`last_reviewed_at`、`review_count=review_count+1`；插入 `recall_logs`（快照字段）；提交并返回更新后的 Word 与 Log。`known=false` 同样更新 `last_reviewed_at`。

同一批文件实现 `ListRecallLogs`：`WHERE user_id=?`，可选 `notebook_id=?`、`id < ?`，`ORDER BY id DESC LIMIT ?`（上限 500）；测试 `beforeID=0` 取最新、`beforeID=上一页最小ID` 不重复不丢行。

- [ ] **Step 6: 实现 Manager**

Mutex + `map[int64]*State`。`Begin`：无状态或生词本不同则重置为 `{NotebookID: id}`。`Answer`：校验生词本；若 `AnsweredWordID == wordID` 返回 `ErrAlreadyAnswered`；计数 +1，设置 `AnsweredWordID`。`UndoAnswer`：减计数并清 `AnsweredWordID`。`SetLastWord` 设置 `LastWordID` 并清 `AnsweredWordID`。`Reset` 删除状态。

- [ ] **Step 7: 运行测试并提交**

```bash
go test ./internal/recall ./internal/store -v
git add -A && git commit -m "feat: weighted recall selection, answer transaction and session state"
```

---

### Task 9: 回忆页面、答题揭示与本轮小结

**Files:**
- Create: `internal/web/recall_handlers.go`, `internal/web/recall_handlers_test.go`
- Create: `web/templates/recall_select.html`, `recall.html`, `summary.html`
- Modify: `internal/web/server.go`, `web/templates/partials.html`
- Test: `internal/web/recall_handlers_test.go`

**Interfaces:**
- Consumes: `recall.Manager`、`store.AnswerWord`、`store.ListWords`。
- Routes: `GET /recall`、`GET /recall/{id}`、`POST /recall/{id}/answer`、`POST /recall/{id}/next`、`POST /recall/{id}/end`、`GET /recall/{id}/summary`、`GET /recall/{id}/restart`。

- [ ] **Step 1: 写回忆流程测试**

```go
func TestRecallHidesMeaningUntilAnswer(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	body := tc.get("/recall/" + fmt.Sprint(tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "apple") { t.Fatal("word missing") }
	if strings.Contains(body, "苹果") { t.Fatal("meaning must be hidden before answer") }
}

func TestKnownAnswerRevealsMeaningAndIncrements(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), url.Values{
		"word_id": {fmt.Sprint(w.ID)}, "known": {"1"}, "csrf_token": {tc.csrf},
	})
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "苹果") || !strings.Contains(resp.Body.String(), "熟练度") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	got, _ := tc.store.WordByID(context.Background(), tc.notebook.ID, w.ID)
	if got.Proficiency != 6 { t.Fatalf("proficiency=%d", got.Proficiency) }
	logs, _ := tc.store.ListRecallLogs(context.Background(), tc.user.ID, nil, 0, 10)
	if len(logs) != 1 || !logs[0].Known { t.Fatalf("logs=%+v", logs) }
}

func TestDoubleAnswerIsRejected(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	form := url.Values{"word_id": {fmt.Sprint(w.ID)}, "known": {"1"}, "csrf_token": {tc.csrf}}
	if resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), form); resp.Code != 200 {
		t.Fatalf("first code=%d", resp.Code)
	}
	if resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), form); resp.Code != http.StatusConflict {
		t.Fatalf("second code=%d", resp.Code)
	}
	got, _ := tc.store.WordByID(context.Background(), tc.notebook.ID, w.ID)
	if got.Proficiency != 6 { t.Fatalf("proficiency=%d", got.Proficiency) }
	logs, _ := tc.store.ListRecallLogs(context.Background(), tc.user.ID, nil, 0, 10)
	if len(logs) != 1 { t.Fatalf("logs=%d", len(logs)) }
}

func TestEndSummaryCounts(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	b := tc.seedWord(tc.notebook.ID, "pear", "n. 梨")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	// 直接经 manager 计数，避免随机抽词影响断言。
	tc.srv.recall.Answer(tc.user.ID, tc.notebook.ID, a.ID, true)
	tc.srv.recall.Answer(tc.user.ID, tc.notebook.ID, b.ID, false)
	resp := tc.postForm(fmt.Sprintf("/recall/%d/end", tc.notebook.ID), url.Values{"csrf_token": {tc.csrf}})
	if resp.Code != http.StatusSeeOther { t.Fatalf("code=%d", resp.Code) }
	loc := resp.Header().Get("Location")
	if !strings.Contains(loc, "known=1") || !strings.Contains(loc, "unknown=1") { t.Fatalf("location=%s", loc) }
	body := tc.get(loc).Body.String()
	if !strings.Contains(body, "认识 1") || !strings.Contains(body, "不认识 1") { t.Fatalf("body=%s", body) }
}
```

- [ ] **Step 2: 运行确认失败** → `go test ./internal/web -run TestRecall -v`，FAIL。

- [ ] **Step 3: 实现选本页与回忆页**

- `GET /recall`：列出所有本及词数，0 词禁用。
- `GET /recall/{id}`：校验归属；`manager.Begin`；`ListWords`；`recall.Pick`（排除 `LastWordID`，`rand.New(rand.NewSource(time.Now().UnixNano()))`）；`SetLastWord`；渲染只含英文单词的卡片和两个按钮。无词显示空状态。
- 卡片按钮用 `hx-post` + `hx-vals='{"word_id":ID,"known":1}'`，目标 `#recall-card`；body 上已有 CSRF header。
- 页面头部显示「本次认识 x · 不认识 y」与「结束本轮」表单（POST `/end`，带 CSRF）。

- [ ] **Step 4: 实现 answer / next / end**

- `POST /answer`：解析 `word_id`、`known`；`manager.Answer` 先占位；`store.AnswerWord`；失败则 `manager.UndoAnswer` 并 500；`ErrAlreadyAnswered` → 409 + 「这道题已经答过啦」片段。成功渲染答案片段：单词、音标、释义、例句、「第 N 次」、星级、「下一个」按钮。
- `POST /next`：`ListWords` + `Pick`（排除上次）→ `SetLastWord` → 渲染单词卡片片段。
- `POST /end`：读 `manager.Snapshot`，`Reset`，303 到 `/recall/{id}/summary?known=..&unknown=..`。
- `GET /summary`：校验数字 0–1,000,000；渲染吉祥物、认识/不认识/总数、「再来一轮」（GET `/recall/{id}/restart`）、「返回生词本」。
- `GET /restart`：`Reset` 后 303 `/recall/{id}`。

- [ ] **Step 5: 实现模板与星级**

`partials.html` 定义 `{{define "stars"}}`：5 个星位，`full` 实心、`half` 左半实心（空星 + 半宽溢出的实心星叠加）、`empty` 空心；`role="img"`、`aria-label`。答案片段与记录页复用。`recall_card` 与 `recall_answer` 也定义在 `partials.html`，页面首屏与 htmx 端点共用同一份标记。`StarLabel` 返回 `熟练度 2.5 / 5`（`%.1f`）。

- [ ] **Step 6: 运行测试并提交**

```bash
go test ./internal/web -v
git add -A && git commit -m "feat: recall session UI, answer reveal and summary"
```

---

### Task 10: 回忆记录页

**Files:**
- Create: `internal/web/records_handlers.go`, `internal/web/records_handlers_test.go`, `internal/web/grouping.go`, `internal/web/grouping_test.go`
- Create: `web/templates/records.html`
- Modify: `internal/web/server.go`, `web/templates/partials.html`
- Test: 上述 `_test.go`

**Interfaces:**
- Consumes: `(*Store).ListRecallLogs`（Task 8）。
- Produces: `web.GroupLogs(logs []store.RecallLog, loc *time.Location) []DayGroup`、`DayGroup{Date string; Label string; Known, Unknown int; Logs []store.RecallLog}`。
- Route: `GET /records`（参数 `notebook`、`before`）。

- [ ] **Step 1: 写分组测试**

```go
func TestGroupLogsByLocalDay(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	// 2026-11-13 15:59 UTC = 23:59 上海；2026-11-13 16:01 UTC = 次日 00:01。
	logs := []store.RecallLog{
		{ID: 2, ReviewedAt: time.Date(2026, 11, 13, 16, 1, 0, 0, time.UTC), Known: false},
		{ID: 1, ReviewedAt: time.Date(2026, 11, 13, 15, 59, 0, 0, time.UTC), Known: true},
	}
	groups := GroupLogs(logs, loc)
	if len(groups) != 2 { t.Fatalf("groups=%d", len(groups)) }
	if groups[0].Label != "11 月 14 日 · 周六" || groups[0].Unknown != 1 { t.Fatalf("%+v", groups[0]) }
	if groups[1].Label != "11 月 13 日 · 周五" || groups[1].Known != 1 { t.Fatalf("%+v", groups[1]) }
}
```

- [ ] **Step 2: 写查询与页面测试**

```go
func TestRecordsFilterByNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	other, _ := tc.store.CreateNotebook(context.Background(), tc.user.ID, "另一本")
	c := tc.seedWord(other.ID, "grape", "n. 葡萄")
	now := time.Now().UTC()
	tc.store.AnswerWord(context.Background(), tc.notebook.ID, a.ID, true, now)
	tc.store.AnswerWord(context.Background(), other.ID, c.ID, true, now)
	body := tc.get(fmt.Sprintf("/records?notebook=%d", tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "apple") || strings.Contains(body, "grape") {
		t.Fatalf("filter failed: %s", body)
	}
}
```

- [ ] **Step 3: 运行确认失败** → `go test ./internal/store ./internal/web -run 'TestGroup|TestListRecall|TestRecords' -v`，FAIL。

- [ ] **Step 4: 实现查询与页面**

- `ListRecallLogs` 用 `WHERE user_id=?`，可选 `notebook_id=?`，可选 `id < ?`，`ORDER BY id DESC LIMIT ?`（上限 500）。
- 页面默认取 200 条；`GroupLogs` 按 `ReviewedAt.In(loc).Format("2006-01-02")` 分组，保持输入顺序；label 用 `1 月 2 日 · 周一`；每日统计认识/不认识。
- 每条显示：`FormatTime`（15:04）、单词、`第 N 次`、认识/不认识徽标、星级（`log.ProficiencyAfter`）。
- 筛选下拉列出当前生词本；记录中已删除的生词本只出现在「全部」。
- 「加载更早」：`hx-get="/records?before=<最小ID>&notebook=..."`，目标 `#records-list`，`hx-swap="beforeend"`；`records_days` 命名模板定义在 `partials.html`，页面与片段共用；没有更多时隐藏按钮。

- [ ] **Step 5: 运行测试并提交**

```bash
go test ./internal/store ./internal/web -v
git add -A && git commit -m "feat: add recall history grouped by day with cursor pagination"
```

---

### Task 11: 导出 / 导入与设置页数据区

**Files:**
- Create: `internal/store/export.go`, `internal/store/import.go`, `internal/store/export_test.go`, `internal/store/import_test.go`
- Create: `internal/web/settings_handlers.go`, `internal/web/import_handlers.go`, `internal/web/export_import_test.go`
- Create: `web/templates/settings.html`
- Modify: `internal/web/server.go`
- Test: 上述 `_test.go`

**Interfaces:**
- Produces: `store.ExportFile{Format string; Version int; ExportedAt time.Time; Notebooks []ExportNotebook; RecallLogs []ExportLog}`；`ExportNotebook{Name string; IsDefault bool; Words []ExportWord}`；`ExportWord{Word, Phonetic, Translation, Definition string; Examples []Example; Proficiency int; AddedAt time.Time; LastReviewedAt *time.Time; ReviewCount int}`；`ExportLog{Notebook, Word string; Known bool; ProficiencyAfter, ReviewCount int; ReviewedAt time.Time}`。
- Produces: `ExportAll(ctx, userID) (ExportFile, error)`、`ExportNotebook(ctx, userID, notebookID) (ExportFile, error)`、`ValidateExport(f ExportFile) error`、`Import(ctx, userID int64, f ExportFile, mode ImportMode) (ImportStats, error)`；`ImportMerge`、`ImportOverwrite`；`ImportStats{NotebooksCreated, WordsCreated, WordsUpdated, WordsSkipped, LogsAdded int}`。
- JSON tag 严格按 spec §8.5：`format/version/exported_at/notebooks/recall_logs`、`name/is_default/words`、`word/phonetic/translation/definition/examples/proficiency/added_at/last_reviewed_at/review_count`、`notebook/word/known/proficiency_after/review_count/reviewed_at`、例句 `en/zh/source`。
- Routes: `GET /export?notebook=all|<id>`、`POST /import`、`GET /settings`。

- [ ] **Step 1: 写导出/导入测试**

```go
func TestExportImportRoundTrip(t *testing.T) {
	st := newTestStore(t)
	u, nb, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	now := time.Now().UTC().Truncate(time.Second)
	w, _ := st.AddWord(context.Background(), nb.ID, Word{Word: "apple", Translation: "n. 苹果", Proficiency: 6})
	st.AnswerWord(context.Background(), nb.ID, w.ID, true, now)
	f, err := st.ExportAll(context.Background(), u.ID)
	if err != nil { t.Fatal(err) }

	dst := newTestStore(t)
	u2, _, _ := dst.CreateFirstUser(context.Background(), "other", "hash", "默认生词本")
	if _, err := dst.Import(context.Background(), u2.ID, f, ImportOverwrite); err != nil { t.Fatal(err) }
	def, _ := dst.DefaultNotebook(context.Background(), u2.ID)
	words, _ := dst.ListWords(context.Background(), def.ID, "added")
	if len(words) != 1 || words[0].Word != "apple" || words[0].Proficiency != 7 { t.Fatalf("words=%+v", words) }
	logs, _ := dst.ListRecallLogs(context.Background(), u2.ID, nil, 0, 10)
	if len(logs) != 1 { t.Fatalf("logs=%d", len(logs)) }
}

func TestMergeTakesHigherProficiencyAndEarlierAddedAt(t *testing.T) {
	st := newTestStore(t)
	u, nb, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	w, _ := st.AddWord(context.Background(), nb.ID, Word{Word: "apple", Translation: "本地释义", Proficiency: 6, LastReviewedAt: &newer})
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "默认生词本",
		Words: []ExportWord{{Word: "apple", Translation: "", Proficiency: 8, AddedAt: older, LastReviewedAt: &older}},
	}}}
	if _, err := st.Import(context.Background(), u.ID, f, ImportMerge); err != nil { t.Fatal(err) }
	got, _ := st.WordByID(context.Background(), nb.ID, w.ID)
	if got.Proficiency != 8 || got.Translation != "本地释义" || !got.AddedAt.Equal(older) || !got.LastReviewedAt.Equal(newer) {
		t.Fatalf("%+v", got)
	}
}

func TestImportRejectsBadProficiencyAndRollsBack(t *testing.T) {
	st := newTestStore(t)
	u, nb, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	st.AddWord(context.Background(), nb.ID, Word{Word: "keep"})
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{
		Name: "x", Words: []ExportWord{{Word: "ok", Proficiency: 5}, {Word: "bad", Proficiency: 11}},
	}}}
	if _, err := st.Import(context.Background(), u.ID, f, ImportOverwrite); err == nil { t.Fatal("expected error") }
	words, _ := st.ListWords(context.Background(), nb.ID, "added")
	if len(words) != 1 || words[0].Word != "keep" { t.Fatalf("rollback failed: %+v", words) }
}

func TestImportNormalizesDefaultNotebook(t *testing.T) {
	st := newTestStore(t)
	u, _, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	f := ExportFile{Format: "study-words", Version: 1, Notebooks: []ExportNotebook{{Name: "A"}, {Name: "B"}}}
	if _, err := st.Import(context.Background(), u.ID, f, ImportOverwrite); err != nil { t.Fatal(err) }
	list, _ := st.ListNotebooks(context.Background(), u.ID)
	defaults, defaultName := 0, ""
	for _, nb := range list {
		if nb.IsDefault { defaults++; defaultName = nb.Name }
	}
	if len(list) != 2 || defaults != 1 || defaultName != "A" { t.Fatalf("list=%+v", list) }
}

func TestImportDedupesLogs(t *testing.T) {
	st := newTestStore(t)
	u, _, _ := st.CreateFirstUser(context.Background(), "parent", "hash", "默认生词本")
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f := ExportFile{Format: "study-words", Version: 1,
		Notebooks:  []ExportNotebook{{Name: "默认生词本", Words: []ExportWord{{Word: "apple", Proficiency: 5}}}},
		RecallLogs: []ExportLog{{Notebook: "默认生词本", Word: "apple", Known: true, ProficiencyAfter: 6, ReviewCount: 1, ReviewedAt: ts}},
	}
	if _, err := st.Import(context.Background(), u.ID, f, ImportMerge); err != nil { t.Fatal(err) }
	if _, err := st.Import(context.Background(), u.ID, f, ImportMerge); err != nil { t.Fatal(err) }
	logs, _ := st.ListRecallLogs(context.Background(), u.ID, nil, 0, 10)
	if len(logs) != 1 { t.Fatalf("logs=%d", len(logs)) }
}
```

- [ ] **Step 2: 运行确认失败** → `go test ./internal/store -run TestExport -v`，FAIL。

- [ ] **Step 3: 实现导出**

- `ExportAll`：查询用户所有本、词、记录；记录按 `id DESC`。
- `ExportNotebook`：只含该本词与 `notebook_id=?` 的记录。
- `ExportedAt = time.Now().UTC()`；`Format="study-words"`、`Version=1`。
- 示例与时间字段用标准 JSON 编码。

- [ ] **Step 4: 实现导入**

- `ValidateExport`：`Format` 必须 `study-words`，`Version` 必须 1，至少 1 个生词本；本 ≤1000、每本词 ≤10000、记录 ≤100000；名称 1–100 字符；词 1–100；释义/例句长度 ≤300；每词例句 ≤5；熟练度 1–10；时间非零。`Import` 入口先调用它，失败直接返回、不开启事务。
- `Import` 在一个事务内执行：
  - 覆盖：`DELETE FROM recall_logs WHERE user_id=?`、`DELETE FROM notebooks WHERE user_id=?`（级联删词）。
  - 合并：按名称（NOCASE）匹配本；词按 `(notebook_id, word COLLATE NOCASE)` 匹配；熟练度取 max、`review_count` 取 max、`last_reviewed_at` 取较晚、`added_at` 取较早；本地释义/例句为空才补充。
  - 记录：覆盖全量重建；合并按 `(word, reviewed_at, known)` 去重追加（先把该用户现有记录的键读进内存 map，再逐条判断，避免逐条查询）。找不到本/词时 `notebook_id`/`word_id` 为 NULL，但保快照。
  - 结束前归一化默认本：保留现有默认；没有则文件默认；再没有则第一个。
- 任何 SQL/校验错误回滚。

- [ ] **Step 5: 实现 handler 与设置页数据区**

- `GET /export`：`notebook=all` 或数字；校验归属；`Content-Disposition: attachment; filename="study-words-YYYYMMDD.json"`；`Content-Type: application/json; charset=utf-8`。
- `POST /import`：`r.Body = http.MaxBytesReader(w, r.Body, 10<<20)`；`ParseMultipartForm`；`json.Decoder`；`ValidateExport`；mode 必须是 `merge`/`overwrite`；覆盖必须 `confirm=yes`，否则 400；调用 `Import`；flash 统计；303 `/settings`。错误重渲染设置页并显示原因。
- `settings.html` 增加数据区：导出全部、按本导出链接、导入表单（文件 + 合并/覆盖 + 覆盖确认文案）。

- [ ] **Step 6: 运行测试并提交**

```bash
go test ./internal/store ./internal/web -v
git add -A && git commit -m "feat: export/import vocabulary and recall history with merge/overwrite"
```

---

### Task 12: 设置页账户与关于

**Files:**
- Create: `internal/web/settings_account.go`, `internal/web/settings_account_test.go`
- Modify: `web/templates/settings.html`, `internal/web/server.go`, `internal/web/settings_handlers.go`
- Test: `internal/web/settings_account_test.go`

**Interfaces:**
- Consumes: `auth.CheckPassword`、`auth.HashPassword`、`store.UpdatePassword`、`store.DeleteSessionsForUserExcept`。
- Route: `POST /settings/password`。
- Produces settings view data：`Version string`、`RepoURL string`（AI 状态在 Task 13 追加）。

- [ ] **Step 1: 写测试**

```go
func TestChangePasswordKeepsCurrentSessionAndDropsOthers(t *testing.T) {
	tc := newAuthedTestClient(t)
	_, otherHash, _ := auth.NewToken()
	tc.store.CreateSession(context.Background(), otherHash, tc.user.ID, "csrf2", time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	resp := tc.postForm("/settings/password", url.Values{
		"current": {"secret1"}, "new": {"secret2"}, "confirm": {"secret2"}, "csrf_token": {tc.csrf},
	})
	if resp.Code != http.StatusSeeOther { t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String()) }
	if body := tc.get("/settings").Body.String(); !strings.Contains(body, "密码已更新") { t.Fatalf("flash missing") }
	if _, err := tc.store.SessionByToken(context.Background(), otherHash); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other session still present: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败** → `go test ./internal/web -run TestChangePassword -v`，FAIL。

- [ ] **Step 3: 实现**

- 修改密码：校验旧密码、新密码 6–72、两次一致；更新 hash；删除除当前 token 外的会话；flash「密码已更新」；303 `/settings`。
- 关于：版本、仓库链接 `https://github.com/yaofeng/kids-study-words`、数据来源（ECDICT/Tatoeba/htmx/Tailwind/Nunito）、MIT。
- 退出登录按钮（POST `/logout`）。AI 状态在 Task 13 接入客户端后追加。

- [ ] **Step 4: 运行测试并提交**

```bash
go test ./internal/web -v
git add -A && git commit -m "feat: settings account and about"
```

---

### Task 13: AI 例句客户端与缓存编排

**Files:**
- Create: `internal/ai/client.go`, `internal/ai/client_test.go`
- Create: `internal/store/ai_cache.go`, `internal/store/ai_cache_test.go`
- Modify: `internal/web/lookup_handlers.go`, `internal/web/server.go`, `internal/web/helpers_test.go`, `web/templates/settings.html`, `cmd/server/main.go`
- Test: `internal/ai/client_test.go`, `internal/store/ai_cache_test.go`, `internal/web/ai_examples_test.go`

**Interfaces:**
- Produces: `ai.Example{EN, ZH string}`；`ai.New(baseURL, apiKey, model string, timeout time.Duration) *Client`；`(*Client).Enabled() bool`；`(*Client).Model() string`；`(*Client).Host() string`；`(*Client).Examples(ctx, word, translation string, forms []string) ([]Example, error)`。
- Produces `Options.AI ExampleAI` 与接口：

```go
type ExampleAI interface {
	Enabled() bool
	Model() string
	Host() string
	Examples(ctx context.Context, word, translation string, forms []string) ([]ai.Example, error)
}
```

- Produces: `(*Store).CachedExamples(ctx, word string) ([]Example, bool, error)`、`SaveExamples(ctx, word, model string, ex []Example) error`。
- Adds: `(*Server).examplesFor(ctx, entry dict.Entry) []store.Example`，顺序：AI 缓存 → 本地句对（≥2 即返回）→ AI 生成（配置且本地不足）→ 本地余量 → 空。`snapshotExamples` 保留给加词（缓存 + 本地，不调 AI）。
- Modifies settings：显示 AI 是否启用、模型名、base host；永不渲染 key。

- [ ] **Step 1: 写 AI 客户端测试（httptest 假服务）**

```go
func TestAIClientParsesExamples(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" { t.Errorf("auth header") }
		io.WriteString(w, `{"choices":[{"message":{"content":"[{\"en\":\"I go home.\",\"zh\":\"我回家。\"}]"}}]}`)
	}))
	defer srv.Close()
	c := New(srv.URL+"/v1", "key", "test-model", 2*time.Second)
	ex, err := c.Examples(context.Background(), "go", "去", []string{"go", "went", "gone"})
	if err != nil || len(ex) != 1 || ex[0].ZH != "我回家。" { t.Fatalf("%+v %v", ex, err) }
}

func TestAIClientRejectsSentenceWithoutWordForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"[{\"en\":\"I like cats.\",\"zh\":\"我喜欢猫。\"}]"}}]}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "key", "m", time.Second)
	if _, err := c.Examples(context.Background(), "go", "去", []string{"go"}); err == nil {
		t.Fatal("expected error for sentence without the word")
	}
}
```

- [ ] **Step 2: 写缓存、编排与设置页测试**

```go
func TestExamplesPreferCacheThenAI(t *testing.T) {
	tc := newAuthedTestClient(t)
	fake := &fakeAI{enabled: true, model: "test-model", ex: []ai.Example{{EN: "I go home.", ZH: "我回家。"}}}
	tc.srv.ai = fake
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}},
	}
	body := tc.get("/lookup/examples?word=go").Body.String()
	if !strings.Contains(body, "I go home.") || fake.calls != 1 { t.Fatalf("body=%s calls=%d", body, fake.calls) }
	tc.get("/lookup/examples?word=go")
	if fake.calls != 1 { t.Fatalf("cache miss, calls=%d", fake.calls) }
}

func TestExamplesFallBackToLocalWhenAIErrors(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.ai = &fakeAI{enabled: true, model: "m", err: errors.New("boom")}
	tc.srv.dict = fakeDict{
		entry:    dict.Entry{Word: "go", Translation: "v. 去"},
		examples: []dict.Sentence{{EN: "Go now.", ZH: "现在走。"}},
	}
	body := tc.get("/lookup/examples?word=go").Body.String()
	if !strings.Contains(body, "Go now.") { t.Fatalf("local fallback missing: %s", body) }
}

func TestSettingsNeverRendersAPIKey(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.srv.ai = &fakeAI{enabled: true, model: "test-model"}
	body := tc.get("/settings").Body.String()
	if strings.Contains(body, "sk-secret") { t.Fatal("api key leaked") }
	if !strings.Contains(body, "test-model") { t.Fatal("model missing") }
}
```

`fakeAI` 定义在 `helpers_test.go`：`{enabled bool; model string; ex []ai.Example; err error; calls int}`，`Enabled/Model/Host/Examples` 按接口实现，`Examples` 递增 calls 并返回 `ex`/`err`。

- [ ] **Step 3: 运行确认失败** → `go test ./internal/ai ./internal/store ./internal/web -run 'TestAI|TestExamplesPrefer' -v`，FAIL。

- [ ] **Step 4: 实现 AI 客户端**

- `Enabled`：base URL、key、model 三者均非空。`Model()` 返回模型名；`Host()` 解析 base URL 的 host，失败返回空串。
- 请求 `POST {trimRight(base,"/")}/chat/completions`，头 `Authorization: Bearer <key>`、`Content-Type: application/json`；15 秒超时由调用方传入。
- Prompt：系统「你是儿童英语词典编辑，面向 8–12 岁；只用简单词；只输出 JSON」；用户给出单词、中文释义、要求 2 条 `{"en","zh"}`，句子必须包含该词或其词形。
- 解析 `choices[0].message.content`；从第一个 `[` 到最后一个 `]`（或 `{...}` 中的 `examples`）提取 JSON；最多 3 条，每条 EN/ZH 非空、≤300 字；至少一条包含 `forms` 中任一词（小写包含判断）。
- 错误信息不含 key。

- [ ] **Step 5: 实现缓存与编排**

- 缓存表 `ai_example_cache(word_lower PK, payload, model, created_at)`；保存用 `INSERT ... ON CONFLICT(word_lower) DO UPDATE`。
- `examplesFor`：`CachedExamples` → 本地 `dict.Examples` 取 2 → 若 `len(local)>=2` 返回 → AI（若启用）→ 成功则 `SaveExamples` 并返回 → 否则返回 local。
- `POST /words` 仍用 `snapshotExamples`（缓存 + 本地），不触发 AI，保证加入操作快。
- Settings 注入 AI 状态：`opts.AI != nil && opts.AI.Enabled()`，展示 `Model()` 与 `Host()`；key 永不渲染。`newTestServer` 默认 `AI=nil`。
- `main` 用环境变量构造 `ai.New`，超时 15s；未配置传 `nil`。

- [ ] **Step 6: 运行测试并提交**

```bash
go test ./internal/ai ./internal/store ./internal/web -v
git add -A && git commit -m "feat: optional AI examples with cache and graceful fallback"
```

---

### Task 14: 视觉打磨、响应式与静态资源

**Files:**
- Modify: `tailwind.config.js`, `web/static/input.css`, `web/static/app.js`, all `web/templates/*.html`
- Create: `web/static/fonts/OFL.txt`（Nunito 许可）
- Test: 手动/浏览器检查；`make css` 无报错

**Interfaces:**
- Produces final visual tokens and components; no API changes.

- [ ] **Step 1: 补齐视觉组件**

- 卡片：`rounded-2xl border-2 border-ink/10 bg-white shadow-sticker`；按钮统一 `.btn`（最小 44px 高）；`focus-visible` 环。
- 背景：`bg-cream` + 极淡云朵/星点（内联 SVG，低对比，`aria-hidden`）。
- 吉祥物：内联 SVG 小鸟/猫头鹰，仅登录、空状态、小结页；无持续动画；如加漂浮动画，包在 `motion-safe:` 内。
- 字体：英文单词/标题 `font-rounded`；回忆页单词 `text-4xl sm:text-6xl`。
- 星级：确认 full/half/empty 三态在浅色背景上清晰；半星用叠加裁切。
- 手机底部 Tab（5 项，`pb-[env(safe-area-inset-bottom)]`）；`md:` 侧边栏；内容 `max-w-4xl mx-auto`；320px 无横向滚动。
- 所有表单错误/成功消息用统一 `.alert` 样式；空状态配吉祥物和一句引导。
- 补字体许可：`curl -fsSL https://cdn.jsdelivr.net/npm/@fontsource/nunito@5.1.0/LICENSE -o web/static/fonts/OFL.txt`。

- [ ] **Step 2: 重建 CSS**

Run: `make css`
Expected: 生成/更新 `web/static/app.css`，无报错。

- [ ] **Step 3: 浏览器检查（需要应用可运行）**

用本地 fixture 词典启动应用，分别在 390×844 与 820×1180 检查：登录、查词、生词本、回忆、记录、设置无横向滚动；按钮可点；揭示区可读；字体加载；星图正确（1 级半星、5 级 2.5 星、10 级 5 星）。

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "style: polish playful responsive UI and star rendering"
```

---

### Task 15: Docker、Compose、词典准备脚本与 Makefile

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `.env.example`, `scripts/prepare-dict.sh`, `scripts/fetch-data.sh`
- Modify: `Makefile`
- Test: `bash -n scripts/prepare-dict.sh`、本地 `make dict`、CI 构建（本机无 Docker）

**Interfaces:**
- Produces image `study-words:local`，运行时 `/app/study-words`、`/app/dict.db`、`/data`、端口 8080、UID 1000。

- [ ] **Step 1: 写准备脚本**

`scripts/prepare-dict.sh`：

- `set -euo pipefail`。
- 若 `data-src/ecdict.csv` 存在则用之，否则 `curl -fsSL` 下载固定提交 URL 到 `/tmp/ecdict.csv`；用 `sha256sum -c` 校验 `d1ecbd...`。
- 句对同理，校验 `d60636...`（固定 `https://www.manythings.org/anki/cmn-eng.zip`）。
- 输出路径 `/out/dict.db`，调用 `go run ./cmd/dictbuild -ecdict <csv> -sentences <zip> -out /out/dict.db`。
- 任何校验失败退出非零。

- [ ] **Step 2: 写 Dockerfile（三阶段）**

- `golang:1.24` 构建阶段：`apk add --no-cache ca-certificates curl bash`；复制 `go.mod/go.sum`，`go mod download`；复制源码；`bash scripts/prepare-dict.sh`；`go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/study-words ./cmd/server`。
- 运行阶段 `alpine:3.20`：`apk add --no-cache ca-certificates tzdata`；`addgroup -g 1000 app && adduser -D -u 1000 -G app app`；`mkdir -p /data && chown app:app /data`；复制二进制与 `dict.db`；`USER app`；`ENV DATA_DIR=/data DICT_PATH=/app/dict.db`；`EXPOSE 8080`；`ENTRYPOINT ["/app/study-words"]`。
- `ARG VERSION=dev`。
- `.dockerignore`：排除 `.git`、`data/`、`node_modules`、本地二进制；保留 `data-src/`（离线构建需要）、`web/`、`cmd/`、`internal/`、`scripts/`。

- [ ] **Step 3: 写 compose 与 .env.example**

按 spec §10.3：`image: study-words:local` + `build: .`、命名卷 `study-words-data:/data`、环境变量、healthcheck `["CMD","/app/study-words","healthcheck"]`。`.env.example` 列全部变量并注明 AI key 留空、`COOKIE_SECURE`。

- [ ] **Step 4: Makefile 增加目标**

```make
css: ; npx --yes tailwindcss@3.4.17 -i web/static/input.css -o web/static/app.css --minify
fetch-data: ; bash scripts/fetch-data.sh   # 只下载到 data-src/，不建库
dict: ; go run ./cmd/dictbuild -ecdict data-src/ecdict.csv -sentences data-src/cmn-eng.zip -out data/dict.db
dev: ; go run ./cmd/server
test: ; go test ./...
build: ; go build -o study-words ./cmd/server
clean-data: ; rm -rf data-src
```

`fetch-data` 的逻辑与 prepare 脚本共享：把下载+校验抽到 `scripts/fetch-data.sh`，`prepare-dict.sh` 调用它；`data-src/` 存在时跳过下载。

- [ ] **Step 5: 静态检查与本地验证**

```bash
bash -n scripts/prepare-dict.sh scripts/fetch-data.sh
make fetch-data && make dict   # 本机需 66MB 下载；已有数据则复用
ls -lh data/dict.db
go build ./...
```

Docker 构建在本机无法执行（未安装 Docker），由 Task 16 的 CI 和 Task 17 的 NAS/CI 验证。

- [ ] **Step 6: 提交**

```bash
git add -A && git commit -m "build: add multi-stage Docker, compose, dictionary preparation and Makefile"
```

---

### Task 16: 开源文档、许可证、CI 与镜像发布 workflow

**Files:**
- Create: `README.md`, `LICENSE`, `THIRD_PARTY_NOTICES.md`, `.gitignore`（补全）
- Create: `.github/workflows/ci.yml`, `.github/workflows/docker.yml`
- Test: `go test ./...`、`gh workflow view`（推送后）、YAML 目检

**Interfaces:**
- Produces GitHub Actions：CI（vet/test/build）与 Docker（多架构推送 Docker Hub）。

- [ ] **Step 1: 写 LICENSE 与第三方声明**

- `LICENSE`：MIT，`Copyright (c) 2026 YaoFeng`。
- `THIRD_PARTY_NOTICES.md`：ECDICT（MIT）、Tatoeba 句对（CC-BY 2.0 FR）、htmx（BSD-2-Clause）、Tailwind CSS（MIT）、Nunito（OFL-1.1）。每条含来源链接与用途。
- `web/static/fonts/OFL.txt` 来自 fontsource 包。

- [ ] **Step 2: 写 README**

结构：项目名与一句话简介 → 功能截图（占位，Task 17 补真实截图）→ 功能列表 → 快速开始（Docker Hub 镜像 compose 片段，`<dockerhub-user>` 占位）→ AI 配置示例（DeepSeek/Kimi/OpenAI 兼容 base URL）→ 环境变量表 → 本地开发（Go、`make fetch-data`、`make dict`、`make css`、`make dev`）→ 数据备份与迁移 → GitHub Actions Secrets（`DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN`）→ 数据来源与许可 → 已知限制（单用户、无手动造词、AI 可选）→ License。

- [ ] **Step 3: 写 CI**

`ci.yml`：push/PR；`actions/checkout@v4`、`actions/setup-go@v5`（`go-version-file: go.mod`）、`gofmt -l` 检查、`go vet ./...`、`go test ./...`、`go build ./...`。不下载词典。

- [ ] **Step 4: 写 Docker workflow**

`docker.yml`：

- `on: push branches [main], tags ['v*'], workflow_dispatch`。
- job `test` 同上；job `build` `needs: test`。
- `docker/setup-qemu-action@v3`、`docker/setup-buildx-action@v3`、`docker/login-action@v3`（username/password 用 secrets）、`docker/metadata-action@v5`（tags：latest 仅默认分支、semver version、major.minor、sha）、`docker/build-push-action@v6`。
- `platforms: linux/amd64,linux/arm64`、`push: true`、`cache-from/to: type=gha`、`build-args: VERSION=${{ github.ref_name }}`。
- 镜像 `${{ secrets.DOCKERHUB_USERNAME }}/kids-study-words`。

- [ ] **Step 5: 本地检查并提交**

```bash
go test ./...
gofmt -l .   # 必须无输出
git add -A && git commit -m "docs: add README, MIT license, notices and CI workflows"
```

---

### Task 17: 端到端验证、开源发布与镜像发布

**Files:**
- Modify: `README.md`（截图）、`docs/superpowers/specs/2026-11-13-study-words-design.md`（如有偏差同步）
- Create: `docs/screenshots/*.png`
- Test: 浏览器流程 + `go test ./...` + GitHub Actions

- [ ] **Step 1: 准备本地数据并启动**

```bash
make fetch-data && make dict && make css
DATA_DIR=./data PORT=8080 go run ./cmd/server
```

- [ ] **Step 2: 浏览器端到端检查（390×844 与 820×1180）**

依次验证：首次设置账号 → 默认生词本存在 → 英译中查 apple 并加入 → 中译英查「苹果」并加入 → 生词本详情编辑例句 → 回忆答「认识」后熟练度 +1、揭示释义/例句/星级 → 答「不认识」后 -1 → 结束本轮看小结 → 记录页按天分组、加载更早 → 设置改密码 → 导出 JSON → 覆盖导入同一文件 → 数据一致。截图保存到 `docs/screenshots/`。

- [ ] **Step 3: 全量验证**

```bash
go vet ./...
gofmt -l .
go test ./...
go build ./...
```

Expected: 全部通过，`gofmt -l` 无输出。

- [ ] **Step 4: 更新 README 截图并提交**

```bash
git add -A && git commit -m "docs: add screenshots from end-to-end verification"
```

- [ ] **Step 5: 创建 GitHub 仓库（先不推送）**

```bash
gh repo create kids-study-words --public \
  --description "面向孩子的背单词 Web 应用 · Go + SQLite + Tailwind CSS" \
  --source=. --remote=origin
```

- [ ] **Step 6: 配置 Docker Hub Secrets、推送 main 与 tag**

由仓库所有者执行（不要在对话中粘贴 token）：

```bash
gh secret set DOCKERHUB_USERNAME --body "<dockerhub-username>"
gh secret set DOCKERHUB_TOKEN   # 交互式粘贴访问令牌
git push -u origin main
git tag v0.1.0 && git push origin v0.1.0
```

若暂时没有 Docker Hub 凭据，先只推 `main`；Docker workflow 会因缺 secret 明确失败，配好后再推送 tag 或手动 `gh workflow run docker.yml`。

推送 tag 后检查 Actions 成功，并验证镜像双架构：

```bash
docker buildx imagetools inspect <dockerhub-user>/kids-study-words:latest
```

Expected: 输出包含 `linux/amd64` 与 `linux/arm64`。

- [ ] **Step 7: 首次发布检查**

确认仓库为 Public、README 在 GitHub 正常渲染、LICENSE 被识别、CI 绿色、镜像可在 NAS 上 `docker compose pull && up -d` 启动并进入 `/setup`。

---

## Self-Review Notes

- Spec §7.1 的 `sessions.csrf_token` 已同步到设计文档。
- 所有 spec 功能均有对应任务：查词（7）、生词本（4/5）、回忆（8/9）、记录（10）、导入导出（11）、设置（12）、AI（13）、部署（15）、开源发布（16/17）。
- 交叉引用统一：`Store`、`dict.Entry`、`recall.Manager`、`web.Options` 的签名在各任务间一致。
- Review Focus 的五类风险分别由 Task 11、4、5、8/9、10 的测试覆盖。

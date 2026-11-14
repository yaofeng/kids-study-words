# 液态玻璃视觉改版 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把全部页面从「奶油底 + 黑描边贴纸」改为「晴空蓝 · 糖果玻璃」的液态玻璃视觉，并把版本号升到 1.0.0。

**Architecture:** 保留现有 Go + htmx + Tailwind 结构，主题色与玻璃配方集中写在 `tailwind.config.js` 和 `web/static/input.css`；模板只改 `layout.html` 的身体底色/涂鸦和 `partials.html` 的猫头鹰描边。新增两个静态契约测试，防止调色板和降级样式回退。

**Tech Stack:** Go、Tailwind CSS 3.4.17、htmx、`go:embed` 静态资源、Nunito 字体。

**Spec:** `docs/superpowers/specs/2026-11-14-liquid-glass-design.md`

## Global Constraints

- 版本号只写在 `.version`，且值为 `1.0.0`；代码与静态资源不得硬编码。
- 色板固定为：`sky #F2FAFF`、`paper #FFFFFF`、`ink #1E2D42`、`muted #5B6B80`、`blue #3D9BF0`、`blue-deep #1F73D6`、`blue-text #15529E`、`blue-soft #A9DCFF`、`sun #FFD966`、`mint #66D9C7`、`coral #FF8A73`、`coral-dark #A93A22`、`lilac #B9A7E6`。
- 不加新依赖、不引外部 CDN、不引入深色模式。
- 所有组件类名保持原样，只改视觉规则；模板的结构和文案不动。
- 每次改完 CSS 都要运行 `make css` 重新生成并提交 `web/static/app.css`。
- 所有提交沿用仓库前缀风格（`feat:`、`fix:`、`docs:`），并在消息末尾加 `Co-Authored-By: Claude Code <noreply@anthropic.com>`。

## Review Focus

- **不支持 `backdrop-filter` 的浏览器/旧平板**：面板必须退回清晰的不透明浅色，不能变成看不清的灰片。由 Task 3 的 `@supports` 契约测试覆盖。
- **浅蓝底上的小号文字**：徽章、选中标签、认识按钮必须用 `blue-text`/`blue-deep`，白字不能落在 `#3D9BF0` 上。由 Task 2 的对比度测试覆盖。
- **长列表滚动**：生词卡和记录行不能开 `backdrop-filter`，否则低端平板掉帧。由 Task 3 的规则检查覆盖。
- **375px 窄屏 + iPhone 安全区**：底部悬浮导航不能盖住内容，标签不能换行。由 Task 4 的安全区断言和 Task 7 的截图检查覆盖。
- **旧主题残留**：`teal`、`cream`、`#3B3A36` 若残留会让局部回到旧外观或直接编译失败。由 Task 5 的 grep 和 Task 6 的检查覆盖。

---

### Task 1: 版本号 1.0.0 与文档豁免

**Files:**
- Modify: `.version`
- Modify: `version_test.go`（`skipDirs`）

**Interfaces:**
- Consumes: 无
- Produces: `studywords.Version == "1.0.0"`；`docs/` 下的 Markdown 不再触发版本硬编码检查

- [ ] **Step 1: 把 `.version` 改为 `1.0.0`**

文件内容只有一行，末尾保留换行。

- [ ] **Step 2: 运行测试，确认文档触发失败**

Run: `go test . -run TestVersionLiteralOnlyInDotVersion -v`
Expected: FAIL，列出 `docs/superpowers/specs/2026-11-14-liquid-glass-design.md` 等文件硬编码了 `1.0.0`

- [ ] **Step 3: 让测试跳过 `docs/`**

在 `version_test.go` 的 `skipDirs` 中加入 `"docs": true`，并在注释里说明：文档可以记录发布号，代码和静态资源仍不得硬编码。

- [ ] **Step 4: 运行全部测试**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add .version version_test.go
git commit -m "release: 1.0.0"
```

---

### Task 2: 晴空蓝色板与页面底色

**Files:**
- Create: `web/palette_test.go`
- Modify: `tailwind.config.js`
- Modify: `web/static/input.css`
- Modify: `web/templates/layout.html:11-25`
- Regenerate: `web/static/app.css`

**Interfaces:**
- Produces: Tailwind 颜色键 `sky`、`paper`、`ink`、`muted`、`line`、`blue`、`blue-deep`、`blue-text`、`blue-soft`、`sun`、`mint`、`coral`、`coral-dark`、`lilac`；旧键 `cream`、`teal`、`teal-dark` 删除
- Produces: `body` 的晴空渐变背景；`.bg-doodles` 的白色/浅蓝涂鸦

- [ ] **Step 1: 写对比度契约测试**

创建 `web/palette_test.go`（包名 `assets`），读取 `../tailwind.config.js`，用正则 `['"]?([a-z][a-z-]*)['"]?:\s*['"](#[0-9A-Fa-f]{6})['"]` 提取色板（兼容带引号和不带引号的键），依次断言以下组合的 WCAG 对比度：

```go
pairs := []struct{ fg, bg string; min float64 }{
	{"ink", "sky", 4.5},
	{"muted", "sky", 4.5},
	{"paper", "blue-deep", 4.5},
	{"blue-text", "blue-soft", 4.5},
	{"coral-dark", "paper", 4.5},
	{"ink", "coral", 4.5},
}
```

对比度按 sRGB 相对亮度公式计算；任一不足或颜色键缺失即 `t.Fatalf`。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./web -run TestPaletteContrast -v`
Expected: FAIL，提示缺少 `sky`、`blue-deep` 等颜色

- [ ] **Step 3: 更新 `tailwind.config.js`**

`colors` 改为：

```js
colors: {
  sky: '#F2FAFF',
  paper: '#FFFFFF',
  ink: '#1E2D42',
  muted: '#5B6B80',
  line: '#1E2D42',
  blue: '#3D9BF0',
  'blue-deep': '#1F73D6',
  'blue-text': '#15529E',
  'blue-soft': '#A9DCFF',
  sun: '#FFD966',
  mint: '#66D9C7',
  coral: '#FF8A73',
  'coral-dark': '#A93A22',
  lilac: '#B9A7E6',
},
```

`boxShadow.sticker` 暂时保留，Task 3 完成后再删；`borderRadius.blob` 保留。

- [ ] **Step 4: 替换 `input.css` 里的旧颜色引用**

按下表逐个替换 `@apply` 中的颜色类（模板里只有 `bg-cream` 一处，稍后单独改）：

| 旧 | 新 |
| --- | --- |
| `bg-teal`（配 `text-white`） | `bg-blue-deep` |
| `bg-teal/15`、`bg-teal/20`、`bg-teal/25` | `bg-blue/15`、`bg-blue/20`、`bg-blue/25` |
| `text-teal-dark` | `text-blue-text` |
| `border-teal-dark` | `border-blue-deep` |
| `focus:border-teal-dark` | `focus:border-blue-deep` |
| `focus:ring-teal/40` | `focus:ring-blue/40` |
| `accent-teal` | `accent-blue` |
| `border-line/30` | `border-ink/15` |

再把 `input.css` 中裸写的颜色值改掉：

- 焦点轮廓 `#237a72` → `#1F73D6`
- 星星描边 `#3b3a36` → `#1e2d42`
- 空心星底色 `#e3dccd` → `#d7e6f5`

替换后运行 `grep -n "teal\|cream" web/static/input.css web/templates/layout.html`，应无输出。

- [ ] **Step 5: 更新 `layout.html` 的底色和涂鸦**

- `body` 的 `bg-cream` 改为 `bg-sky`。
- 云朵 fill 改为 `#FFFFFF`、opacity `0.45`；星星 fill 改为 `#FFFFFF`、opacity `0.50`；青色云 fill 改为 `#BFE3FF`、opacity `0.35`。
- 不再出现 `#B9A7E6`、`#FFD966`、`#4FB3A9` 作为涂鸦颜色。

- [ ] **Step 6: 用 CSS 渐变替换 `body` 背景**

把 `input.css` `@layer base` 里 `body` 的两条小圆点背景换成晴空渐变（不新增模糊层）：

```css
background-color: #F2FAFF;
background-image:
  radial-gradient(420px 320px at 8% 4%, rgba(120, 196, 255, 0.32), transparent 70%),
  radial-gradient(380px 300px at 96% 18%, rgba(255, 217, 102, 0.20), transparent 72%),
  radial-gradient(460px 360px at 86% 96%, rgba(102, 217, 199, 0.22), transparent 72%),
  linear-gradient(170deg, #F2FAFF 0%, #E2F3FF 52%, #EAF2FF 100%);
```

不要用 `background-attachment: fixed`：iOS Safari 上会抖动，渐变随页面滚动即可。

- [ ] **Step 7: 重新生成 CSS 并跑测试**

Run: `make css && go test ./web -run TestPaletteContrast -v`
Expected: `app.css` 生成成功；测试 PASS

- [ ] **Step 8: 提交**

```bash
git add tailwind.config.js web/static/input.css web/static/app.css web/templates/layout.html web/palette_test.go
git commit -m "feat: switch the theme to cartoon blue"
```

---

### Task 3: 玻璃表面系统与降级

**Files:**
- Create: `web/styles_test.go`
- Modify: `web/static/input.css`
- Regenerate: `web/static/app.css`

**Interfaces:**
- Consumes: Task 2 的颜色键
- Produces: 统一的玻璃配方；`.card`、`.btn`、`.input`、`.badge`、`.alert`、`.tab`、`.record-row`、`.settings-details` 保持类名但换成玻璃外观；`.auth-card`、`.recall-card`、`.summary-card` 额外开模糊

- [ ] **Step 1: 写玻璃契约测试**

创建 `web/styles_test.go`（包名 `assets`），读取 `static/input.css` 并断言：

```go
want := []string{
	"backdrop-filter",
	"@supports not",
	"prefers-reduced-transparency",
	"prefers-reduced-motion",
}
```

同时断言 `.card {` 和 `.record-row {` 的规则块内**不包含** `backdrop-filter`，而 `.app-nav {`、`.input {`、`.auth-card {` 的规则块内包含 `backdrop-filter`。规则块用「从选择器开始到下一个 `}`」截取。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./web -run TestGlassContract -v`
Expected: FAIL，缺少 `@supports not` 等

- [ ] **Step 3: 在 `input.css` 定义玻璃配方并应用**

在 `@layer base` 的 `:root` 写入：

```css
--glass-fill: linear-gradient(145deg, rgba(255,255,255,.62), rgba(255,255,255,.18) 46%, rgba(255,255,255,.34)), rgba(255,255,255,.20);
--glass-border: rgba(255,255,255,.75);
--glass-shadow: inset 0 1px 0 rgba(255,255,255,.95), inset 1px 0 0 rgba(255,255,255,.50), inset 0 -1px 0 rgba(255,255,255,.32), 0 12px 28px rgba(40,80,140,.16);
--glass-blur: blur(18px) saturate(180%);
```

规则：`.card`、`.btn`、`.input`、`.tab`、`.settings-details`、`.record-row`、`.badge`、`.alert` 使用 `--glass-fill`、`--glass-border`、`--glass-shadow`，圆角保留现有值并把 `.card` 改为 20px、`.btn`/`.input` 改为 16px；**这些规则不写 `backdrop-filter`**。

带模糊的规则各自在自身规则块内写入模糊，便于契约测试逐块检查：

```css
.app-nav { /* 原有布局 + */ -webkit-backdrop-filter: var(--glass-blur); backdrop-filter: var(--glass-blur); }
.input { -webkit-backdrop-filter: var(--glass-blur); backdrop-filter: var(--glass-blur); }
.auth-card { -webkit-backdrop-filter: var(--glass-blur); backdrop-filter: var(--glass-blur); }
.recall-card { -webkit-backdrop-filter: var(--glass-blur); backdrop-filter: var(--glass-blur); }
.summary-card { -webkit-backdrop-filter: var(--glass-blur); backdrop-filter: var(--glass-blur); }
```

`.input` 聚焦时保留 `border-blue-deep` 与 `ring-blue/40`。

同时删除所有组件规则里的 `shadow-sticker`（`.card`、`.btn`、`.tab`、`.nav-link.is-active` 等），并从 `tailwind.config.js` 删除 `boxShadow.sticker`；重新生成后确认没有规则再引用它。

实底按钮（不参与模糊）：

```css
.btn-primary, .btn-known {
  background-image: linear-gradient(160deg, #1F73D6, #0F56AD);
  border-color: rgba(255,255,255,.45);
  color: #fff;
}
.btn-danger, .btn-unknown {
  background: #FF8A73;
  color: #1E2D42;
}
```

- [ ] **Step 4: 写降级规则**

在 `input.css` 末尾（`@layer` 外）加入：

```css
@supports not ((backdrop-filter: blur(1px)) or (-webkit-backdrop-filter: blur(1px))) {
  .card, .input, .badge, .alert, .tab, .settings-details, .record-row, .app-nav,
  .btn:not(.btn-primary):not(.btn-danger):not(.btn-known):not(.btn-unknown) {
    background: rgba(255, 255, 255, .94);
    border-color: rgba(30, 45, 66, .12);
    -webkit-backdrop-filter: none;
    backdrop-filter: none;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .card, .input, .badge, .alert, .tab, .settings-details, .record-row, .app-nav,
  .btn:not(.btn-primary):not(.btn-danger):not(.btn-known):not(.btn-unknown) {
    background: rgba(255, 255, 255, .96);
    -webkit-backdrop-filter: none;
    backdrop-filter: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .btn, .tab, .nav-link { transition: none; }
}
```

实底按钮（`.btn-primary`、`.btn-known`、`.btn-danger`、`.btn-unknown`）不参与降级，保持实色。

- [ ] **Step 5: 重新生成 CSS 并跑测试**

Run: `make css && go test ./web -v`
Expected: 生成成功；`TestGlassContract`、`TestPaletteContrast` 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add web/static/input.css web/static/app.css web/styles_test.go
git commit -m "feat: add the liquid glass surface system"
```

---

### Task 4: 悬浮玻璃导航

**Files:**
- Modify: `web/static/input.css`（`.app-nav`、`.nav-link`、`.nav-icon`）
- Modify: `web/styles_test.go`
- Regenerate: `web/static/app.css`

**Interfaces:**
- Consumes: Task 3 的玻璃配方
- Produces: 手机端悬浮胶囊导航；桌面端玻璃侧栏；`@media (pointer: coarse)` 下 44px 命中区

- [ ] **Step 1: 扩展契约测试**

在 `web/styles_test.go` 增加 `TestNavHitAreasAndSafeArea`，断言 `input.css` 包含：

```go
want := []string{
	"env(safe-area-inset-bottom)",
	"pointer: coarse",
	"min-height: 44px",
}
```

并断言 `.app-nav {` 规则块包含 `backdrop-filter`。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./web -run TestNavHitAreasAndSafeArea -v`
Expected: FAIL

- [ ] **Step 3: 改造 `.app-nav` 和 `.nav-link`**

手机端改为悬浮胶囊：

```css
.app-nav {
  position: fixed;
  left: .75rem;
  right: .75rem;
  bottom: calc(.75rem + env(safe-area-inset-bottom));
  border-radius: 999px;
}
```

保留 `md:` 下的侧栏布局（`md:inset-y-0`、`md:right-auto`、`md:w-64`），并用媒体查询把胶囊的定位还原成侧栏：

```css
@media (min-width: 768px) {
  .app-nav {
    left: 0;
    right: auto;
    bottom: 0;
    border-radius: 0;
  }
}
```

侧栏同样使用玻璃配方。`.nav-link` 保留最小高度 56px，`md:` 下 48px；`.nav-link.is-active` 用 `bg-blue/25` + `text-blue-text`，桌面端用 `border-ink/20` 和 `bg-blue/15`。

- [ ] **Step 4: 加触摸命中区**

在 `@layer components` 之后加：

```css
@media (pointer: coarse) {
  .tab, .btn-small, .settings-summary, .choice input {
    min-height: 44px;
  }
}
```

`.tab` 用 flex 居中保持文字位置；`.btn-small` 只加最小高度，不加内边距，避免布局跳动。

- [ ] **Step 5: 重新生成并测试**

Run: `make css && go test ./web -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add web/static/input.css web/static/app.css web/styles_test.go
git commit -m "feat: float the navigation on glass"
```

---

### Task 5: 页面组件与状态色清理

**Files:**
- Modify: `web/static/input.css`
- Regenerate: `web/static/app.css`

**Interfaces:**
- Consumes: Task 2 色板、Task 3 玻璃配方
- Produces: 回忆、总结、记录、查词、生词本、设置、登录/注册页全部使用新视觉；无旧 token 残留

- [ ] **Step 1: 找出所有旧 token 残留**

Run: `grep -rn "#3b3a36\|#3B3A36\|teal\|cream\|shadow-sticker\|border-2 border-ink" web/static/input.css web/templates`
Expected: 只剩 `partials.html` 里猫头鹰的 `#3B3A36`（Task 6 处理）；其余为 0。`.recall-result` 等状态块若还有 `border-2 border-ink`，一并改成 1px `border-ink/10`。

- [ ] **Step 2: 逐组调整页面组件**

按设计文档 §5.5 调整以下规则块，类名不变：

- 回忆：`.recall-card`、`.recall-word`、`.recall-result.is-known`（`bg-blue/20` + `text-blue-text`）、`.recall-result.is-unknown`（`bg-coral/20` + `text-coral-dark`）、`.btn-known`、`.btn-unknown`
- 总结：`.summary-card`、`.stat-number.is-known`、`.stat-number.is-unknown`
- 记录：`.record-row`（无模糊）、`.records-day-head`（`border-ink/15`）、`.badge.is-known`/`.is-unknown`
- 查词：`.lookup-card`、`.lookup-actions`（虚线改 `border-ink/15`）、`.add-form`
- 生词本：`.notebook-card`、`.notebook-settings`、`.delete-form`、`.badge-default`（`bg-sun` 保留）、`.badge-notebook`（`bg-lilac/40` 保留）
- 设置：`.settings-details`、`.settings-summary`、`.alert-error`、`.alert-success`
- 登录/注册/引导/错误页：`.auth-card`（模糊）、`.auth-title`、`.auth-subtitle`、`.empty-state`、`.alert`

所有浅蓝底上的小字用 `text-blue-text`；所有白字只出现在 `#1F73D6` 或更深的底色上。

- [ ] **Step 3: 检查对比度**

Run: `go test ./web -run TestPaletteContrast -v`
Expected: PASS。若某个实际组合不在测试里，用浏览器取色确认，不足 4.5:1 就换 `blue-text` 或加深底色。

- [ ] **Step 4: 重新生成并跑全部测试**

Run: `make css && go test ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add web/static/input.css web/static/app.css
git commit -m "feat: restyle page components for glass"
```

---

### Task 6: 猫头鹰描边与模板收尾

**Files:**
- Modify: `web/templates/partials.html`（`{{define "owl"}}` 内的描边）

**Interfaces:**
- Consumes: Task 2 的 `ink #1E2D42`
- Produces: 猫头鹰所有 4px/3px 描边从 `#3B3A36` 改为 `#1E2D42`，造型不变

- [ ] **Step 1: 替换描边颜色**

把 `{{define "owl"}}` 中所有 `stroke="#3B3A36"` 改为 `stroke="#1E2D42"`；填充色（`#FFD966`、`#FF9F80`、`#FFFFFF`）不变。

- [ ] **Step 2: 确认整个 `web/` 不再有旧墨色**

Run: `grep -rn "#3B3A36\|#3b3a36" web/`
Expected: 无输出

- [ ] **Step 3: 跑测试**

Run: `go test ./...`
Expected: PASS（模板测试确认页面仍能渲染）

- [ ] **Step 4: 提交**

```bash
git add web/templates/partials.html
git commit -m "feat: tint the owl outlines to ink"
```

---

### Task 7: 视觉验收、截图与发布检查

**Files:**
- Modify: `docs/screenshots/lookup.png`、`recall.png`、`records.png`、`settings.png`、`summary.png`
- Modify: `web/static/app.css`（如果验收中发现需要微调）

**Interfaces:**
- Consumes: Task 1–6 的全部改动
- Produces: 与 README 一致的新截图；通过格式、静态检查、测试和构建

- [ ] **Step 1: 跑静态检查和测试**

Run: `gofmt -l . && go vet ./... && go test ./... && go build ./...`
Expected: `gofmt -l` 无输出；其余全部成功

- [ ] **Step 2: 用临时数据启动应用**

```bash
mkdir -p /tmp/sw-glass-demo
DATA_DIR=/tmp/sw-glass-demo DICT_PATH=./data/dict.db PORT=8099 go run ./cmd/server
```

首次打开完成建号，然后查 `panda`、`curious`、`apple` 并加入默认生词本，做一轮回忆。

- [ ] **Step 3: 三种宽度逐页验收**

在浏览器里按 375px、768px、1280px 检查：登录、查词、生词本、生词本详情、回忆选择、回忆、总结、记录、设置。

重点看：文字对比度、底部胶囊是否盖住内容、卡片边缘高光是否一致、长单词是否溢出。

- [ ] **Step 4: 验收降级**

在开发者工具里禁用 `backdrop-filter`（或临时加 `@supports` 反向规则），确认面板变成清晰的浅色实底；再模拟 `prefers-reduced-transparency: reduce` 和 `prefers-reduced-motion: reduce`。

- [ ] **Step 5: 重新截图**

把 375px 或平板视口下的查词、回忆、记录、设置、总结截到 `docs/screenshots/`，文件名保持 `lookup.png`、`recall.png`、`records.png`、`settings.png`、`summary.png`。README 已按这些文件名引用，无需改文案。

- [ ] **Step 6: 再跑一次测试并提交**

Run: `go test ./...`
Expected: PASS

```bash
git add docs/screenshots web/static/app.css
git commit -m "docs: refresh screenshots for the glass redesign"
```

- [ ] **Step 7: 检查工作区**

Run: `git status --short`
Expected: 无未提交改动

# 少儿背单词 — 设计文档

项目名：少儿背单词
GitHub 仓库：`yaofeng/kids-study-words`（公开，MIT）
日期：2026-11-13
状态：已定稿（2026-11-13）

## 1. 背景与意图

给家里的小孩（约小学阶段）做一个背单词的 Web 应用。它不是通用词典，也不是应试刷题工具，而是一个家长可以帮忙查词、小孩自己可以翻看和回忆的轻量个人工具。部署在家庭 NAS 上，手机和平板通过局域网访问，运行在 Docker 里。项目以 MIT 许可证开源，其他家庭也可以自行部署。

核心诉求：

- **查得到、存得下**：中译英、英译中都支持，查到就能一键存进生词本。
- **记得住**：生词本里的词能按熟练度和遗忘规律被重新抽到，而不是按顺序刷。
- **不喧宾夺主**：界面活泼、有卡通感，适合小孩，但装饰不抢单词的注意力，也不做高频动画。
- **自己的数据自己拿着**：SQLite 单文件，能导出、能导入，迁移设备不求人。

## 2. 目标与非目标

### 目标

- 首次启动引导创建唯一账号；之后用户名密码登录。
- 英→中、中→英双向查词，英→中结果附常用义项的例句。
- 多个生词本，可设默认；加词记录时间，熟练度 1–10，初始 5。
- 回忆模式：选生词本 → 只显示英文 → 「认识 / 不认识」→ 熟练度 ±1 并揭示释义、例句、星级。
- 每次回忆留下记录，可在专门页面按天分组回看：单词、时间、第几次、是否认识、答后熟练度。
- 加权随机：熟练度越低越容易被抽到，距上次回忆越近越不容易被抽到。
- 全部生词数据可导出为 JSON；导入支持合并或覆盖。
- 手机竖屏、平板横竖屏都顺手；断网时查词和回忆仍可用（AI 例句除外）。
- 以 MIT 许可证开源，提供可复现的 Docker 部署、README 与第三方数据来源声明。

### 非目标

- 不做多用户注册、社交、排行榜、家长监控报表。
- 不做单词发音音频合成、口音评测。
- 不做间隔重复的完整 SM-2 算法；本设计用一个可解释的加权随机模型。
- 不做原生 App、PWA 离线安装（局域网访问即可）。
- 不做词形变化练习、拼写测试、选择题等额外题型。
- 不做手动创建词典里没有的词条：加词只能来自查词结果，ECDICT 未收录的词无法加入生词本。

### 反目标

- 花哨到孩子只玩界面不背词。
- 依赖外部 CDN 或在线词典才能打开页面。
- 查词要等很久、回忆要跳转整页导致节奏断裂。
- 导入失败时半途写入、把本地数据弄坏。

## 3. 用户与场景

- **家长**：在平板上查一个词，确认中文意思和例句合适，加入孩子的生词本。
- **孩子**：自己打开「回忆」，选一本生词本，看到英文先想，点「认识」或「不认识」，看答案和星星。
- **迁移/备份**：家长在设置页导出 JSON；换设备后导入，选择合并或覆盖。

## 4. 约束

- 后端 Go；数据库 SQLite；前端 Tailwind CSS。
- 部署形态：单个 Docker 容器 + docker-compose，数据卷挂载到宿主机。
- NAS 可能是 ARM64 或 x86_64；构建产物应尽量无 CGO 依赖。
- 局域网环境可能没有外网，页面资源与词典必须随镜像提供。
- AI 例句为可选增强，通过 OpenAI 兼容接口接入；未配置时功能完整降级。

## 5. 方案选择

考虑过三条路：

1. **Go 服务端渲染 + htmx（采用）**：`net/http` + `html/template` 出页面，htmx 做局部刷新；Tailwind 在开发时编译为静态 CSS 并内嵌。单二进制、单容器、断网可用，交互足够。
2. Go JSON API + React/Vue SPA：交互上限更高，但要维护两套工具链和两套构建产物，对个人工具过重。
3. 纯服务端表单、无 JS：查词与回忆都会整页刷新，节奏差。

选择 1。理由：功能面固定、交互不复杂，htmx 足以覆盖「局部出结果 / 原地翻卡」；单容器最贴合 NAS 部署与离线要求。

词典采用**混合方案**：ECDICT 离线词典随镜像提供；例句优先取本地中英对照语料，配置 AI 后由 AI 生成并缓存；两者都没有时退回英文释义。

## 6. 技术架构

```
浏览器（Tailwind + htmx）
        │  HTML / 表单 / htmx 片段
        ▼
Go 单进程 (net/http)
 ├─ web: 路由、模板、会话、CSRF
 ├─ store: SQLite（modernc.org/sqlite，纯 Go）
 ├─ dict: 只读词典 SQLite + 例句检索
 └─ ai: OpenAI 兼容客户端 + 结果缓存
        │
        ├── /data/study.db      读写，Docker 卷
        └── /app/dict.db        只读，构建时生成并随镜像
```

- Go 1.24+，使用标准库 `net/http` 的 method+path 路由（Go 1.22 起）。
- SQLite 驱动：`modernc.org/sqlite`，纯 Go，方便交叉编译和 ARM NAS。
- 模板：`html/template` + `embed`；htmx 2.x 本地静态文件；Tailwind 构建出的 `app.css` 本地静态文件。
- 时间统一以 UTC 存 SQLite；展示按 `TZ` 环境变量（默认 `Asia/Shanghai`）格式化。
- SQLite 连接：开启 WAL、`foreign_keys=ON`、`busy_timeout=5000`，写连接串行化（`SetMaxOpenConns(1)` 或经单连接池），个人工具不存在高并发瓶颈。

## 7. 数据模型

### 7.1 用户库 `/data/study.db`

```sql
users (
  id            INTEGER PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,           -- bcrypt
  created_at    TEXT NOT NULL            -- UTC RFC3339
)

sessions (
  token_hash TEXT PRIMARY KEY,           -- SHA-256(cookie token)
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,              -- 会话内 CSRF token
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
)

notebooks (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (user_id, name COLLATE NOCASE)
)
-- 每个用户至多一个默认本
CREATE UNIQUE INDEX notebooks_one_default
  ON notebooks(user_id) WHERE is_default = 1;

words (
  id               INTEGER PRIMARY KEY,
  notebook_id      INTEGER NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  word             TEXT NOT NULL,
  phonetic         TEXT NOT NULL DEFAULT '',
  translation      TEXT NOT NULL DEFAULT '',   -- 中文释义，多行
  definition       TEXT NOT NULL DEFAULT '',   -- 英文释义，可空
  examples_json    TEXT NOT NULL DEFAULT '[]', -- [{en, zh, source}]
  proficiency      INTEGER NOT NULL DEFAULT 5 CHECK (proficiency BETWEEN 1 AND 10),
  added_at         TEXT NOT NULL,
  last_reviewed_at TEXT,                       -- NULL = 从未回忆
  review_count     INTEGER NOT NULL DEFAULT 0,
  UNIQUE (notebook_id, word COLLATE NOCASE)
)
-- 应用层统一以规范化的小写形式入库
CREATE INDEX words_due ON words(notebook_id, last_reviewed_at);

recall_logs (
  id                INTEGER PRIMARY KEY,
  user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  notebook_id       INTEGER REFERENCES notebooks(id) ON DELETE SET NULL,
  notebook_name     TEXT NOT NULL,      -- 快照，生词本删除后仍可读
  word_id           INTEGER REFERENCES words(id) ON DELETE SET NULL,
  word              TEXT NOT NULL,      -- 快照，单词删除后历史仍可读
  known             INTEGER NOT NULL,   -- 1 认识 / 0 不认识
  proficiency_after INTEGER NOT NULL,
  review_count      INTEGER NOT NULL,   -- 该词累计回忆次数（含本次）
  reviewed_at       TEXT NOT NULL
)
CREATE INDEX recall_logs_user_time ON recall_logs(user_id, reviewed_at DESC);

ai_example_cache (
  word_lower TEXT PRIMARY KEY,
  payload    TEXT NOT NULL,   -- [{en, zh}]
  model      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
)
```

`examples_json` 存的是加入时的快照，离线也能展示；之后词典更新不影响已存词。`recall_logs` 同样是快照：单词或生词本被删除后，历史记录仍完整可读。

### 7.2 词典库 `/app/dict.db`（构建产物，只读）

```sql
entries (
  word        TEXT PRIMARY KEY,
  phonetic    TEXT,
  translation TEXT,     -- 中文释义，换行分隔
  definition  TEXT,     -- 英文释义
  pos         TEXT,
  collins     INTEGER,
  oxford      INTEGER,
  tag         TEXT,     -- zk gk cet4 cet6 ...
  bnc         INTEGER,
  frq         INTEGER,
  exchange    TEXT      -- 词形变化，如 p:went/d:gone/i:going/3:goes/s:...
)

sentences (
  id  INTEGER PRIMARY KEY,
  en  TEXT NOT NULL,
  zh  TEXT NOT NULL,
  src TEXT NOT NULL DEFAULT 'tatoeba'
)
CREATE INDEX sentences_en ON sentences(en);
```

- `entries` 建 `word COLLATE NOCASE` 索引，支持英文精确/前缀查询。
- 中文查询对 `translation` 做 `LIKE '%关键词%'`，并优先只在有频率信息或常见标签（zk/gk/cet4/cet6）的词条中搜索，按 `frq/bnc` 升序（数值越小越常见）取前 30 条。若实测超过 300ms，再引入 FTS5 trigram 或预分词索引；接口契约（300ms 内返回）不变。
- 例句匹配：把查询词及 ECDICT `exchange` 中的词形，在 `sentences` 中用 `LIKE` + 词边界匹配，优先选包含原形的、较短的句子，最多取 2 句。全表约 3 万行，可接受。

### 7.3 数据来源与构建

- ECDICT（MIT）：`ecdict.csv` 约 66MB，构建时下载并转成 `dict.db`。
- Tatoeba 中英对照句对（CC-BY 2.0 FR，manythings.org 的 `cmn-eng.zip`，约 1.4MB）：构建时下载、去重、清洗后写入 `sentences`。
- 构建工具：`cmd/dictbuild`（Go）。Dockerfile 的多阶段构建里下载原始数据并运行转换；最终镜像只带 `dict.db`，不带原始 CSV。
- 许可信息放在设置页「关于/数据来源」中。

## 8. 功能设计

### 8.1 首次启动与登录

- 应用启动时检查 `users` 是否为空。
  - 为空：除 `/healthz` 和静态资源外，所有 GET 都重定向到 `/setup`；`/setup` 只有一条创建账号表单（用户名、密码、确认密码），创建成功后自动登录，并创建「默认生词本」（`is_default=1`）。
  - 非空：`/setup` 返回 404；访问任何页面未登录则跳 `/login`。
- 登录成功写 `sessions`，下发随机 32 字节 token 的 HttpOnly Cookie（SameSite=Lax，`COOKIE_SECURE` 可开），有效期 30 天。退出登录删除会话。
- 所有非 GET 请求校验 CSRF token（会话内随机值，藏在表单隐藏字段和 htmx 请求头里）。
- 登录失败每 IP 每分钟最多 5 次（内存计数即可），超限返回「请稍后再试」。

### 8.2 查单词

顶部两个模式：「英 → 中」和「中 → 英」。

**英 → 中**

- 输入英文，htmx 即时请求（防抖 250ms），精确匹配优先，其次前缀匹配，按词频排序，最多 20 条。
- 结果卡片展示：单词、音标、中文释义（按词性分行）、英文释义、例句、词频标签（如「小学 / 初中 / CET4」）。
- 点开某条结果（或直接展示第一条）可展开例句；例句区域异步加载：
  1. 优先返回该词已存的 AI 缓存；
  2. 否则取本地 `sentences` 中最多 2 句；
  3. 若本地不足且 `AI_API_KEY` 已配置，调用 AI 生成 2 句（英文 + 中文翻译），写入缓存后返回；
  4. 都不满足则显示「暂无例句」，并仍展示英文释义兜底。
- 结果卡片上有「加入生词本」：下拉选择生词本（默认本预选），提交后该词以当前熟练度 5、`added_at=now` 存入；重复加入时提示「已在《xx》中」，不重复插入。
- 加入时把音标、中文释义、英文释义、已获取的例句一并快照进 `words`。

**中 → 英**

- 输入中文，防抖后搜索；返回多条英文候选，按常用度排序，每条显示英文、音标、包含关键词的中文释义片段。
- 同样可展开详情与例句、加入生词本。
- 空结果给友好提示并建议换关键词。

**AI 例句约定**

- OpenAI 兼容 `POST {AI_BASE_URL}/chat/completions`，`model=AI_MODEL`，服务端调用，超时 15 秒。
- Prompt 要求：面向 8–12 岁学习者，围绕该词最常用的 1–2 个义项，各给 1 句简短英文例句和自然中文翻译；只输出 JSON。
- 返回校验：必须是 JSON 数组且句子包含目标词（或词形变化）；不合法则视为失败。
- 失败静默降级，不向小孩展示技术错误；设置页可查看 AI 是否已启用。

### 8.3 生词本

**列表页**：卡片展示每个生词本的名称、词数、默认标记、最近加入时间；可新建、重命名、删除、设为默认。

- 默认本有且只有一个；把 A 设为默认时自动取消其他本。
- 删除默认本时，若还有其他本，自动把最近更新的一个设为默认；不允许删除最后一个生词本，提示「至少保留一个生词本」。

**详情页**：单词列表，显示英文、中文释义首行、熟练度星级、加入时间、最后回忆时间。

- 排序切换：加入时间（新→旧）、熟练度（低→高）、最近回忆。
- 单词可编辑释义与例句，方便家长修正：
  - 释义是一个多行文本域，保存到 `translation`；
  - 例句按结构编辑，每条有「英文」「中文」两个输入框，可增删，最多 5 条，每段不超过 300 字；
  - 已有例句的 `source` 原样保留，新增例句记为 `manual`；英文与中文都为空的行在保存时丢弃，其余行必须两栏都有内容；
  - 编辑不改动熟练度、加入时间和最后回忆时间。
- 单词可移出生词本（需二次确认；已产生的回忆记录保留）。
- 顶部按钮「开始回忆」进入该本的回忆模式。

### 8.4 单词回忆

**选本页**：列出所有生词本，显示词数；词数为 0 的本禁用并提示「先加几个词吧」。

**回忆页**：

- 从所选生词本按权重随机取一个词，**只显示英文单词**（不显示音标、释义）。
- 两个大按钮：「认识」「不认识」。
- 点击任一按钮后：
  - 「认识」：熟练度 +1（上限 10）；「不认识」：熟练度 −1（下限 1）。
  - 更新 `last_reviewed_at=now`、`review_count += 1`。
  - 在同一事务里写入一条 `recall_logs`：单词、`reviewed_at`、累计回忆次数（含本次）、是否认识、答后熟练度、生词本名称快照。
  - 原地揭示：中文释义（按词性分行）、例句（英文 + 中文）、当前熟练度星级，并出现「下一个」按钮。
  - 揭示区不自动消失；「下一个」才抽新词。
- 星级展示：5 个星位表示 1–10 级，每级 0.5 星。实心 = 1 星，半实心 = 0.5 星，空心 = 0 星；旁边显示文字「熟练度 3.5 / 5」。
- 会话内小计数：「本次认识 x · 不认识 y」，不打断。
- 一轮没有固定长度：一直点「下一个」继续，直到点右上角「结束本轮」。
- 会话统计只存在内存里（用户 → 本次认识/不认识计数、上一次抽到的词），进程重启即丢，不影响已写入的熟练度和历史记录。
- 点「结束本轮」进入小结页：显示本次认识数、不认识数、总答题数，配吉祥物和「再来一轮 / 返回生词本」；一题未答时显示「还没开始呢」的轻松文案。「再来一轮」清空本次计数并重新抽题。
- 抽词时排除本会话上一次抽到的词（本里只有 1 个词时除外）。
- 无例句时显示「暂无例句」，不阻塞流程。

**加权随机算法**

对生词本内每个词计算权重：

```
熟练度因子 = (11 - proficiency)^2        // 1 级=100，5 级=36，10 级=1
间隔小时数 = last_reviewed_at ? now - last_reviewed_at : 30 天
时间因子   = clamp(间隔小时数 / 24, 0.05, 30)
权重       = 熟练度因子 × 时间因子
```

- 从未回忆过的词按 30 天处理，优先被抽到。
- 刚答过的词时间因子接近 0.05，几乎不会立刻重复；隔一天回到 1，隔一周到 7，一个月以上封顶 30。
- 按权重做一次加权抽样（Efraimidis–Spirakis 或前缀和 + 随机数），O(n)。个人生词本规模（几千词以内）足够。
- 实现为纯函数并单测：熟练度越低权重越大；间隔越长权重越大；边界不出现负数或零概率。

### 8.5 导入 / 导出

**导出**：设置页提供「导出全部」和「导出某个生词本」，下载 `study-words-YYYYMMDD.json`。导出单个生词本时只带该本的单词和回忆记录（按当前生词本 id 过滤）；导出全部时包含所有记录，包括已删除生词本留下的历史。

```json
{
  "format": "study-words",
  "version": 1,
  "exported_at": "2026-11-13T12:00:00Z",
  "notebooks": [
    {
      "name": "默认生词本",
      "is_default": true,
      "words": [
        {
          "word": "apple",
          "phonetic": "/ˈæpl/",
          "translation": "n. 苹果",
          "definition": "a round fruit",
          "examples": [{"en": "I ate an apple.", "zh": "我吃了一个苹果。"}],
          "proficiency": 5,
          "added_at": "2026-11-13T12:00:00Z",
          "last_reviewed_at": null,
          "review_count": 0
        }
      ]
    }
  ],
  "recall_logs": [
    {
      "notebook": "默认生词本",
      "word": "apple",
      "known": true,
      "proficiency_after": 6,
      "review_count": 1,
      "reviewed_at": "2026-11-13T12:30:00Z"
    }
  ]
}
```

**导入**：选择一个 JSON 文件，二选一模式：

- **合并**：按生词本名称匹配，没有就新建。词（忽略大小写）已存在时：
  - 熟练度取两者较高值；
  - `last_reviewed_at` 取较晚的非空值；
  - `review_count` 取较大值；
  - 本地释义/例句为空时才用导入内容补充，不覆盖家长改过的内容；
  - `added_at` 取较早值。
  文件里的默认本标记不覆盖当前默认本；仅当当前没有默认本时采用。
- **覆盖**：清空当前用户全部生词本、单词和回忆记录，再按文件重建。二次确认弹窗明确写「将删除现有 N 个生词本、M 个单词和全部回忆记录」。

**回忆记录**：

- 导出包含 `recall_logs` 数组；文件没有该字段时按空数组处理（兼容旧导出）。
- 合并：按 (word, reviewed_at, known) 去重后追加，不合并、不改写已有记录。
- 覆盖：先清空再按文件重建；记录里找不到对应生词本或单词时仍保留快照，只是 `notebook_id` / `word_id` 为空。

**默认本归一化**：导入结束后确保恰好一个默认本。合并模式保留现有默认本；若当前没有默认本，采用文件中的默认本，文件中也没有则用第一个。覆盖模式优先采用文件中标记的默认本；没有标记或标记了多个时，用第一个生词本。默认标记异常不导致整个导入失败。

**校验与事务**：文件上限 10MB；校验 `format`、`version`、必填字段和取值范围（熟练度 1–10，时间为可解析的 RFC3339）；全部校验通过后在一个事务里写入，任何失败整体回滚并提示具体原因。导入完成显示统计：新增本数、新增词数、更新词数、跳过数。

### 8.6 回忆记录

- 记录页按天分组，最近的在前；每天一个小标题（如「11 月 13 日 · 周四」）和当日小计「认识 x · 不认识 y」。
- 每条记录显示：时间（HH:mm）、单词、第 N 次回忆、认识/不认识标记、答后熟练度星级。
- 支持按生词本筛选；记录中的生词本名称是当时的快照，生词本或单词后来被删除也不影响历史。
- 分页：默认加载最近 200 条，底部「加载更早」按时间游标继续取。
- 按 `TZ`（默认 Asia/Shanghai）分组，跨天以本地时间 00:00 为界。
- 数据只读，不提供编辑/删除单条记录；导入的覆盖模式会清空历史。

### 8.7 设置

- 账户：修改密码（需旧密码）、退出登录。忘记密码不在网页端重置，改用容器内 CLI（见 §11）。
- 数据：导出全部/单本（含回忆记录）、导入（合并/覆盖）。
- AI：显示是否已启用（只显示模型名和 base host，不显示 key）、说明通过环境变量配置。
- 关于：版本、词典与例句数据来源及许可。

## 9. 界面与响应式设计

### 9.1 视觉方向

- 关键词：温暖、圆润、像绘本，但信息优先。
- 配色（低饱和、足够对比度）：
  - 背景：暖奶油 `#FFF8EE`；纸面卡片 `#FFFFFF`
  - 主色：柔和青绿 `#4FB3A9`；强调：珊瑚橙 `#FF9F80`；辅助：淡黄 `#FFD966`、浅紫 `#B9A7E6`
  - 正文近黑 `#3B3A36`，次要文字 `#7A756C`
- 形状：大圆角卡片（16–24px）、2px 深色描边 + 轻微偏移的柔和阴影，营造贴纸/绘本质感。
- 字体：英文单词和标题用随包提供的圆体（Nunito woff2），中文用系统无衬线（避免引入数 MB 中文字库）；正文 16–18px，回忆页单词 40–64px。
- 卡通元素：一只简单的小猫头鹰/小鸟吉祥物（内联 SVG），只出现在登录、空状态、会话结束三处；背景有极淡的云朵/星点。**不做持续动画**；如需轻微漂浮，必须尊重 `prefers-reduced-motion`。
- 按钮触控区 ≥ 44px，回忆页「认识/不认识」为整宽大按钮，小孩单手可点。

### 9.2 布局

- **手机（< 768px）**：单列；底部固定 Tab 栏：查词 / 生词本 / 回忆 / 记录 / 设置。内容区左右 16px 留白，卡片纵向堆叠。
- **平板/桌面（≥ 768px）**：左侧固定侧边栏（Logo + 五项导航 + 生词本快捷入口），主内容居中，最大宽度约 960px；回忆页单词与按钮更大。
- 页面布局在 320px 宽也不横向滚动；表格类信息在手机上改为卡片。

### 9.3 关键页面草图

**查词页**

```
┌───────────────────────────────┐
│  [英 → 中] [中 → 英]          │
│  ┌─────────────────────────┐  │
│  │ 输入单词或中文……        │  │
│  └─────────────────────────┘  │
│  ┌ apple ─────────────────┐   │
│  │ /ˈæpl/      n. 苹果     │   │
│  │ 例句：I ate an apple.   │   │
│  │ 我吃了一个苹果。        │   │
│  │ [加入生词本 ▾]          │   │
│  └────────────────────────┘   │
└───────────────────────────────┘
```

**回忆页**

```
┌───────────────────────────────┐
│  《动物》      本次 3 · 1  结束本轮 │
│                               │
│           elephant            │
│                               │
│  ┌───────────┐ ┌───────────┐  │
│  │   认识     │ │  不认识   │  │
│  └───────────┘ └───────────┘  │
│                               │
│  （点击后揭示）                │
│  n. 大象                      │
│  An elephant has a long nose. │
│  大象有长鼻子。                │
│  ★★☆◐☆   熟练度 2.5 / 5      │
│           [ 下一个 ]          │
└───────────────────────────────┘
```

**结束本轮小结**

```
┌───────────────────────────────┐
│            (吉祥物)           │
│          本轮完成！           │
│      认识 12 · 不认识 3       │
│   [ 再来一轮 ]  [ 返回生词本 ]│
└───────────────────────────────┘
```

**回忆记录页**

```
┌───────────────────────────────┐
│  记录            [全部生词本 ▾]│
│  ── 11 月 13 日 · 周四 ──     │
│  认识 8 · 不认识 1            │
│  20:14  elephant  第 3 次     │
│         认识 · ★★◐☆☆         │
│  20:12  apple     第 1 次     │
│         不认识 · ★★☆☆☆       │
│  ── 11 月 12 日 · 周三 ──     │
│  ...                          │
│         [ 加载更早 ]          │
└───────────────────────────────┘
```

## 10. 部署

### 10.1 Dockerfile

- 阶段 1（`dictbuild`）：基于 `golang:1.24`，运行 `scripts/prepare-dict.sh`：优先使用构建上下文 `data-src/` 中预先放好的 `ecdict.csv` 与 `cmn-eng.zip`；缺失时才联网下载。下载地址固定到具体提交/版本，并用仓库中记录的 SHA-256 校验。随后运行 `cmd/dictbuild` 生成 `/out/dict.db`。
- 阶段 2（`build`）：编译 `CGO_ENABLED=0` 的静态二进制，模板与静态资源用 `embed` 打入。
- 阶段 3（runtime）：`alpine:3.20`，创建 UID/GID 1000 的 `app` 用户，把 `/data` 的属主设为 `app`；拷贝二进制和 `dict.db`；以 `app` 运行；`EXPOSE 8080`；`VOLUME /data`。
- 支持 `docker buildx` 构建 `linux/amd64` 与 `linux/arm64`。
- 前端资源：Tailwind 输出 `web/static/app.css` 并提交到仓库（`make css` 可重建），Docker 构建不依赖 Node；htmx 与字体同样放 `web/static`。
- 产物镜像名 `study-words:local`。

### 10.2 获取镜像的三种方式

- **直接拉取已发布镜像（NAS 首选）**：CI 自动构建的镜像在 Docker Hub，compose 里写 `image: <dockerhub-user>/kids-study-words:latest` 并去掉 `build:`，`docker compose pull && docker compose up -d` 即可，NAS 不需要构建环境。
- **在 NAS 上构建**：`docker compose up -d --build`。首次构建需要外网下载词典与句对；如果 NAS 没有外网，先在能上网的机器执行 `make fetch-data`，把生成的 `data-src/` 一起拷到 NAS 后再构建。构建完成后可用 `make clean-data` 删除源码数据。
- **在别处构建再搬运**：用 `docker buildx build --platform linux/amd64,linux/arm64` 生成镜像；通过 NAS 可访问的本地镜像仓库推送，或 `docker save study-words:local | gzip > study-words.tar.gz`，拷到 NAS 后 `docker load` 导入。此时 compose 使用 `image: study-words:local`，可去掉 `build`。
- 各方式产出的容器和数据格式一致，部署文档同时给出步骤。

### 10.3 docker-compose.yml

```yaml
services:
  study-words:
    image: study-words:local
    build: .            # 用预构建镜像分发时删掉这一行
    container_name: study-words
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      TZ: Asia/Shanghai
      DATA_DIR: /data
      DICT_PATH: /app/dict.db
      COOKIE_SECURE: "false"
      AI_BASE_URL: ""       # 例如 https://api.deepseek.com/v1
      AI_API_KEY: ""
      AI_MODEL: ""
    volumes:
      - study-words-data:/data
      # 想直接访问 SQLite 文件时改用目录挂载，并先执行：
      #   sudo mkdir -p ./data && sudo chown -R 1000:1000 ./data
      # - ./data:/data
    healthcheck:
      test: ["CMD", "/app/study-words", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3

volumes:
  study-words-data:
```

- 默认用 Docker 命名卷，镜像里 `/data` 的属主会带到新卷上，非 root 进程可直接写。目录挂载必须把宿主机目录的属主改为 `1000:1000`，否则启动会因无法写库而失败并给出提示。
- 用 Docker Hub 已发布镜像时，把 `image:` 改成 `<dockerhub-user>/kids-study-words:latest`、删掉 `build:`，数据卷和其余配置不变。
- 提供 `.env.example`；密钥只放 `.env`，不进镜像。
- 首次访问 `/setup` 创建账号。
- 若放在反向代理后启用 HTTPS，把 `COOKIE_SECURE` 设为 `true`。

### 10.4 开源发布

- 项目名「少儿背单词」，公开仓库 `yaofeng/kids-study-words`，许可证 MIT，版权人 YaoFeng。
- 发布物：`README.md`（中文为主，附简短英文介绍）、`LICENSE`、`THIRD_PARTY_NOTICES.md`、`.env.example`、`docker-compose.yml`、截图。
- README 覆盖：功能截图、用 Docker Hub 镜像快速开始、本地构建、环境变量表、AI 配置示例、Actions Secrets 配置、开发与测试、数据来源与许可、备份/迁移、已知限制。
- 第三方许可：ECDICT（MIT）、Tatoeba 句对（CC-BY 2.0 FR）、htmx（MIT）、Tailwind CSS（MIT）、Nunito 字体（OFL-1.1）。在 `THIRD_PARTY_NOTICES.md` 和设置页「关于」中注明。
- 仓库元数据：描述「面向孩子的背单词 Web 应用 · Go + SQLite + Tailwind CSS」，topics 包括 `golang`、`sqlite`、`flashcards`、`vocabulary`、`kids`、`self-hosted`、`docker`、`tailwindcss`。
- CI：GitHub Actions 在 push/PR 时运行 `go vet ./...`、`go test ./...`、`go build ./...`；不下载 66MB 词典，避免拖慢 CI。
- 发布流程：本地初始化 git（`main` 分支），实现与测试通过后提交；用 `gh repo create kids-study-words --public --source=. --remote=origin --push` 创建并推送。之后按功能小步提交。
- `.gitignore`：`data/`、`data-src/`、`.env`、构建产物和本地二进制。

### 10.5 镜像自动发布（GitHub Actions → Docker Hub）

- 触发：push 到 `main`、推送 `v*` 标签、手动 `workflow_dispatch`。
- 流程：workflow 先跑 vet/test/build，通过后用 buildx + QEMU 构建 `linux/amd64` 与 `linux/arm64`，推送到 Docker Hub 的 `${{ secrets.DOCKERHUB_USERNAME }}/kids-study-words`。
- 标签用 `docker/metadata-action` 生成：`main` → `latest`、`sha-<短哈希>`；`v1.2.3` → `1.2.3`、`1.2`、`latest`。
- 认证：仓库 Secrets 配 `DOCKERHUB_USERNAME` 与 `DOCKERHUB_TOKEN`（Docker Hub 访问令牌，权限 Read & Write）。未配置时 workflow 明确失败，不会误推。
- 缓存：BuildKit `type=gha` 缓存，避免每次重新下载 66MB 词典；Go 依赖同样走缓存。
- 构建时用 `-ldflags -X main.version` 注入版本号，设置页「关于」显示。
- 发布前检查：`docker buildx imagetools inspect` 确认 amd64 和 arm64 两个平台都存在。
- 可选：同时推 GHCR（`ghcr.io/yaofeng/kids-study-words`，用内置 `GITHUB_TOKEN`，无需额外 secret）；默认先只上 Docker Hub，需要再加。

## 11. 安全与数据保护

- 密码 bcrypt（cost 12）；不记录明文。用户名唯一，大小写不敏感比较。
- 会话 token 只存 SHA-256 哈希；Cookie HttpOnly、SameSite=Lax；30 天过期。
- 仅允许创建一个用户；`/setup` 在已有用户后失效，防止局域网内被再次注册。
- 提供容器内 CLI 重置密码：`docker compose exec study-words /app/study-words reset-password <用户名>`，新密码从标准输入读取（不走命令行参数，避免留在 shell 历史），成功后删除该用户全部会话。只有能执行容器命令的人才能使用，网页端不暴露此能力。
- 所有写操作校验 CSRF；所有 SQL 参数化。
- 登录限速（内存计数，每 IP 5 次/分钟）。
- 导入文件大小与字段严格校验，事务写入；覆盖模式需二次确认。
- AI 请求只在服务端发生；key 不写日志、不返回前端。
- 静态资源与模板随二进制提供，不依赖外部 CDN。
- 日志不记录密码、会话 token、AI key。

## 12. 错误与边界

- 词典查不到：展示空状态与建议，不报错。
- AI 超时/失败：静默降级为本地例句或「暂无例句」。
- 重复加词：提示已存在，不覆盖熟练度。
- 删除最后一个生词本：拒绝并提示。
- 导入版本不认识：拒绝并提示支持的版本。
- 导入覆盖中途失败：事务回滚。
- 导入后没有或多个默认本：归一化为一个，不中断。
- 删除单词或生词本：回忆记录保留（快照），记录页仍可查看。
- 数据卷不可写：启动失败，日志明确提示检查挂载目录属主。
- 数据库文件损坏/无可写卷：启动失败并打印明确错误。
- 无例句、无音标：隐藏对应区块，不留空框。
- 星数渲染读到越界熟练度：前端 clamp 到 1–10（数据库也有 CHECK 约束）。

## 13. 测试策略

- 单元测试（`go test ./...`）：
  - 加权随机：熟练度越低权重越大；间隔越久权重越大；刚回忆的词近乎不重复；单词语料本可用；权重恒正。
  - 导入合并：取高熟练度、较晚回忆时间、较早上架时间、空字段补充；覆盖模式清空重建；回忆记录去重追加/清空重建；默认本归一化；非法 JSON/越界熟练度拒绝且库不变。
  - 星级换算：1→0.5，2→1，5→2.5，9→4.5，10→5。
  - 认证：首次 setup 可用、二次 setup 拒绝、未登录跳转、CSRF 校验、会话过期；CLI 重置密码后旧密码失败、新密码成功、旧会话失效。
  - 回忆记录：认识/不认识各写一条且字段正确；`review_count` 递增；按 `TZ` 跨天分组；当日小计正确；筛选生词本正确。
  - 例句编辑：增删行、`source` 保留、空行丢弃、两栏不完整时拒绝保存。
  - 词典：英文精确/前缀排序、中文子串搜索、词形变化例句匹配。
- 集成测试：`httptest` + 临时 SQLite 跑完整流程（setup → 登录 → 查词 → 加词 → 回忆答题 → 查看记录 → 导出 → 覆盖导入）。
- 浏览器手测：390×844（手机）与 820×1180（平板）两档，检查无横向滚动、按钮可点、揭示区可读。
- 视觉检查不进入自动化；以截图人工确认。
- CI：GitHub Actions 在 push/PR 时跑 `go vet`、`go test`、`go build`，不访问外网下载词典。
- 发布 workflow：main 与 tag 触发多架构构建，检查 `docker buildx imagetools inspect` 中 amd64/arm64 都存在，标签规则符合预期。

## 14. 项目结构

```
study-words/
├── cmd/
│   ├── server/main.go          # Web 服务入口；子命令 healthcheck / reset-password
│   └── dictbuild/main.go       # 词典/例句构建工具
├── internal/
│   ├── auth/                   # 密码、会话、CSRF、限速
│   ├── store/                  # SQLite 访问、迁移、回忆记录查询
│   ├── dict/                   # 词典查询、例句匹配
│   ├── ai/                     # OpenAI 兼容客户端与缓存
│   ├── recall/                 # 加权抽词、内存会话计数
│   └── web/                    # 路由、handler、模板渲染
├── web/
│   ├── templates/*.html
│   └── static/                 # app.css, htmx.min.js, 字体, 图标
├── scripts/
│   └── prepare-dict.sh         # 优先用 data-src/，否则下载并校验
├── docs/superpowers/specs/2026-11-13-study-words-design.md
├── data-src/                   # 可选：预下载的 ecdict.csv / cmn-eng.zip（.gitignore）
├── data/                       # 运行时卷（.gitignore）
├── .github/workflows/ci.yml    # push/PR：vet / test / build
├── .github/workflows/docker.yml # main/tag：多架构镜像 → Docker Hub
├── README.md
├── LICENSE                     # MIT
├── THIRD_PARTY_NOTICES.md
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── .gitignore
├── Makefile                    # make css / fetch-data / dev / test / build
└── go.mod
```

## 15. 已定决策与留给构建者的空间

### 已定（来自对话或本设计）

- 混合词典：ECDICT 离线 + 本地句对 + 可选 AI 例句；AI 走 OpenAI 兼容接口。
- 单容器、docker-compose、NAS 局域网部署。
- 服务端渲染 + htmx；Tailwind 构建期生成，静态内嵌。
- 熟练度 1–10，对应 5 星、每级半星；两个按钮点击后都揭示答案。
- 单用户实例；首次运行引导创建，之后无注册；忘记密码用容器内 CLI 重置。
- 回忆无限继续，手动「结束本轮」看小结；会话计数只在内存，历史记录持久化并按天分组查看。
- 导入合并规则、覆盖语义、默认本归一化如上；回忆记录随导出/导入一起迁移。
- 部署支持在 NAS 上构建或预构建镜像搬运；默认命名卷，目录挂载需把属主改为 1000:1000。
- 不做手动造词；例句按结构化「英文/中文」行编辑。
- 开源：项目名「少儿背单词」，公开仓库 `yaofeng/kids-study-words`，MIT 许可证，用 `gh` 创建并推送。
- 镜像自动发布：GitHub Actions 在 main/tag 上构建 amd64 + arm64 并推 Docker Hub；凭据只放仓库 Secrets（`DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN`）。

### 留给构建者

- 具体 CSS 变量、间距和插画细节（在本文视觉方向内自行决定）。
- 若中文搜索实测超时，选择 FTS5 trigram 或预分词索引；接口 300ms 目标不变。
- Tatoeba 句对清洗规则（长度上限、去重、明显不适合儿童内容的过滤）。
- AI prompt 的具体措辞与输出校验的宽松程度。
- 测试文件组织与辅助函数。

## 16. 关键权衡记录

- **为什么不用 SPA**：交互面小，htmx 已够；少一套构建链，NAS 部署和离线更稳。
- **为什么 AI 例句要缓存**：避免重复计费与等待；同一词多次查看结果一致。
- **为什么中文搜索不直接上 FTS**：先保证简单可维护；仅在有性能证据时加索引，避免提前复杂化。
- **为什么熟练度用乘性权重**：可解释、易测试；时间因子封顶避免「越久越无限大」。
- **为什么把例句快照进生词本**：复习时不依赖词典更新或网络，离线体验一致。
- **为什么回忆记录存单词/生词本快照**：删除或重命名生词本后历史仍应可读，记录是事实日志，不跟随业务数据删除。

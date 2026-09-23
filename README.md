# coreinsight-cli

面向 AI Agent / 脚本的 Core Insight 命令行工具。实现方式参考 `czb1/cli`：Go 1.21、零第三方依赖、本地 Cookie 会话、JSON-RPC 2.0 输出。

## 构建

```bash
go build -o coreinsight-cli ./cmd/coreinsight-cli
```

## 1. 登录

认证功能直接复用/移植 `czb1/cli` 的认证实现模式（`auth.go`、`session.go`、`prompt_*.go`），仅适配 CLI 名称、环境变量前缀和本地会话目录。

```bash
# 与原命令兼容
coreinsight-cli auth login --username U --password P

# 脚本 / CI 推荐：密码不进入命令历史
printf '%s\n' "$PASS" | coreinsight-cli auth login --username U --password-stdin

# 也可只执行登录，在交互终端输入用户名和密码；密码尽可能不回显
coreinsight-cli auth login

# 查看本地认证状态
coreinsight-cli auth status

# 清除本地会话
coreinsight-cli auth logout
```

登录凭证解析规则与参考 CLI 保持一致：

1. 如果传入 `--body-file` 或 `--body`，直接使用原始 JSON 请求体（兼容旧调用）。
2. 用户名按 `--username/-u` → `COREINSIGHT_AUTH_USERNAME` → 交互输入解析。
3. 密码按 `--password-stdin` → `--password/-p` → `COREINSIGHT_AUTH_PASSWORD` → 交互式无回显输入解析。
4. 登录请求本身不携带历史 Cookie，避免旧会话干扰。
5. 成功后从响应 `Set-Cookie` 提取 Cookie 和过期时间，保存到 `~/.coreinsight-cli/session.json`（目录 0700、文件 0600）；后续业务请求自动携带未过期 Cookie。
6. `auth status` 的退出码为：`0` 已认证，`3` 未认证或本地会话已过期。

登录用户名即用户工号，登录后会保存并供后续命令复用，无需重复传工号：

- `qa`、`retrieve`、`experience upload` 的 `user_id`，以及 `skill download/upload/parse` 的 `userId`，默认取登录工号。
- 同时包含 `user_id` 和 `caller_id` 的经验检索，仅 `caller_id` 默认取登录工号；`user_id` 未指定时传空字符串，需要指定目标用户时使用 `--user_id`。
- 显式传入的 `--user_id` / `--caller_id` 优先；没有有效登录会话时，需要显式提供对应的工号参数。

> `auth login` 与 `czb1/cli` 保持一致：默认请求 `POST https://omtool.rnd.huawei.com/api/auth/login`，请求体为 `{userName, passwd}`，登录请求不携带历史 Cookie；HTTP 2xx 且业务 `code=0` 后，从响应 `Set-Cookie` 提取 Cookie/过期时间并建立本地会话。可用 `COREINSIGHT_AUTH_SERVER` 或 `--auth-server` 覆盖；为兼容参考 CLI，显式 `--server` 也会覆盖登录 server。

## 2. 知识问答 / 检索

四个路由参数 `--kb_sns`、`--products`、`--departments`、`--scenes` 必须且只能选择一个；多个值用逗号分隔。`user_id` 默认取登录用户名，也可以显式传 `--user_id`。

```bash
coreinsight-cli qa \
  --query "问题" \
  --kb_sns 123,456 \
  --prompt_template SDD

coreinsight-cli retrieve \
  --query "问题" \
  --products UPCF,UDG
```

## 3. Skill

```bash
coreinsight-cli skill scenes --repo .

coreinsight-cli skill search \
  --query "数据库" \
  --page 1 \
  --page_size 6 \
  --sort_by downloads \
  --sort_order desc

# 兼容旧命令名，与 skill search 行为一致
coreinsight-cli skill search-smart --query "数据库"

# 场景搜索也使用 AI Community 智能检索
coreinsight-cli skill scene-search --query "333"

# 兼容旧参数，按产品、一级场景、二级场景顺序用空格拼接为 keyword
coreinsight-cli skill scene-search \
  --product "UNC USMF" \
  --first_scene "需求开发" \
  --second_scene "MML开发"

coreinsight-cli skill download \
  --skill_id "bca61fb0-3734-49c0-906e-0209d17032fd" \
  --version "0.0.1" \
  --output ./skills

coreinsight-cli skill upload --file ./skill.zip --business_dimension "产品级"
coreinsight-cli skill parse  --file ./skill.zip --business_dimension "产品级"
```

`skill scenes` 会读取 `git remote get-url origin`，把 SSH/SCP 地址规范化成 HTTP(S)，分页查询产品，再通过 Chat 服务的 `POST /experience/harness/scenes` 查询场景。默认完整 URL 为 `https://coreinsight.rnd.huawei.com/chat/experience/harness/scenes`。产品接口固定使用 `pageSize=20`；`offering_cn_name` 原样作为产品名使用，不做 trim。

`skill search` 调用 AI Community 的 `GET /aiapp-v2/api/skills`，固定 `searchMode=smart`，并发送 `pageNum`、`pageSize`、`sortBy`、`sortOrder`、`keyword`。`skill search-smart` 保留为兼容别名。`skill scene-search` 使用相同的 GET 接口和分页、排序默认值。支持 `--query`；未提供非空 `--query` 时，将 `--product`、`--first_scene`、`--second_scene` 的非空值按顺序用空格拼接为 `keyword`（智能关键词搜索，不是结构化场景过滤）。默认完整 URL 为 `https://aicommunity.coreai.rnd.huawei.com/aiapp-v2/api/skills`。

`skill download` 先从 AI Community 获取下载 URL，再使用普通 GET 下载 ZIP。现有接口材料没有定义这个动态下载 URL 的额外 Header/鉴权要求，因此 CLI 不向该 URL 转发 Core Insight Cookie。

## 4. 经验

```bash
coreinsight-cli experience search \
  --query "智能客服系统" \
  --page 1 \
  --page_size 5 \
  --scene ALL

coreinsight-cli experience upload \
  --scene "test" \
  --scene_id "scene-001" \
  --title "在U2020上执行MML命令" \
  --summary "完整字段示例，包含向量化文本与元数据" \
  --experience "在该场景下积累的经验点" \
  --rag_search_text "MML命令 U2020 执行"
```

经验上传的核心请求字段为 `user_id`、`title`、`summary`、`scene_id`、`scene`、`experience`、`rag_search_text`。`user_id` 默认取登录用户名，也可使用 `--user_id` 指定，不写死工号。成功响应中的 `data` 为经验 ID 字符串，CLI 会原样保留在 JSON-RPC 的 `result` 中，不要求版本信息对象。旧的 `--doc_id` 和产品字段参数保留透传兼容；新接口是否支持这些扩展字段、是否有覆盖语义，需以后端契约为准。

```json
{"code":200,"msg":"success","data":"3659f967-a733-4045-9b09-c3ba2ad8d9a1"}
```

## 5. OKF 知识中心

当前材料只给出了“知识配置”标题，没有提供对应 Endpoint、Method、请求体或响应契约，因此本版本没有臆造 OKF 命令。补充 API 契约后可以按相同模式继续扩展。

经验检索调用 `POST /chat/experience/search`，请求体使用 `user_id`、`caller_id`、`show_personal`、`page`、`page_size`、`source`、`title`、`scene`。其中 `title` 取 `--query`，`caller_id` 默认使用登录工号，`user_id` 仅使用显式传入的值，未指定时为空字符串。指定目标 `--user_id` 不会改变默认调用方工号。

所有 HTTP/HTTPS 请求均使用统一客户端，并按当前内部环境要求关闭 TLS 证书校验。

## 环境变量

- `COREINSIGHT_SERVER`
- `COREINSIGHT_CHAT_SERVER`
- `COREINSIGHT_AUTH_SERVER`
- `COREINSIGHT_AI_COMMUNITY_SERVER`
- `COREINSIGHT_CORE_HARNESS_SERVER`
- `COREINSIGHT_TIMEOUT`
- `COREINSIGHT_AUTH_USERNAME`（同时兼容 `OMRES_AUTH_USERNAME`）
- `COREINSIGHT_AUTH_PASSWORD`（同时兼容 `OMRES_AUTH_PASSWORD`）

同名全局参数可覆盖服务地址和超时，例如：

```bash
coreinsight-cli --server https://example.internal --timeout 300 qa ...
```

## 405 / 问答超时排查

经验上传调用 `POST https://coreinsight.rnd.huawei.com/chat/experience/experience_add`，使用 Chat 服务基址。通过 `--chat-server` 或 `COREINSIGHT_CHAT_SERVER` 可覆盖基址（需包含 `/chat`）；CLI 追加 `/experience/experience_add`。此前临时新增的 `--memory-server` / `COREINSIGHT_MEMORY_SERVER` 已移除，请改用 Chat 配置。上传不自动重试。

```bash
# 下面地址是占位示例，必须替换成后端确认的真实服务基址
coreinsight-cli --chat-server https://coreinsight.rnd.huawei.com/chat experience upload \
  --scene test --scene_id scene-001 --title 标题 --summary 摘要 --experience 内容

coreinsight-cli --chat-server https://chat.example.internal/chat --debug qa \
  --query "问题" --kb_sns 123
```

- `--chat-server` 指向 Chat 基址（默认 `<COREINSIGHT_SERVER>/chat`），CLI 会追加 `/app/support/app/chat` 或 `/app/support/app/retrieve`。环境变量末尾的斜杠会先移除，避免生成 `//chat`。
- JSON 业务请求遇到会把 POST 改为 GET 的 301/302/303 时，直接报告重定向及目标地址；同方法的 307/308 仍可跟随。登录请求保持参考 CLI 的行为。
- 405 错误包含实际请求方法、URL 和后端返回的 `Allow`（若存在）。应结合网关路由检查，不能仅凭 405 判定为“接口限制”。
- 网络错误区分 DNS、连接失败和一般超时，并保留底层错误。一般超时不能直接证明服务器不可达，也可能是后端等待过久。核对内网/VPN、DNS、代理及 `NO_PROXY`，确认地址正确后再考虑 `--timeout`。
- `--debug` 会输出业务请求体，请对分享的日志脱敏。

回归测试使用本地 HTTP 服务检查上传、三个问题中的问答/检索请求及重定向行为。它们不验证华为内网服务是否可达，也不验证部署侧网关映射。

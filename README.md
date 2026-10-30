# coreinsight-cli

面向 AI Agent / 脚本的 Core Insight 命令行工具。Go 1.21、零第三方依赖、本地会话、JSON-RPC 2.0 输出。

## 构建

```bash
go build -o coreinsight-cli ./cmd/coreinsight-cli
```

## 1. 登录

默认通过浏览器授权登录（复用 agentcenter devUC 流程），无需在命令行输入账号密码。登录后 token 永久有效，业务请求通过 `X-Auth-Token` 头鉴权。

```bash
# 默认：自动打开浏览器完成授权
coreinsight-cli auth login

# 无头 / CI 环境，或浏览器未自动弹出时，仅打印授权链接供手动复制
coreinsight-cli auth login --no-browser

# 自定义授权轮询超时秒数（默认 60）
coreinsight-cli auth login --timeout 120

# 查看本地认证状态
coreinsight-cli auth status

# 清除本地会话
coreinsight-cli auth logout
```

登录流程：

1. CLI 向市场服务请求 devUC 授权页 URL：`GET <market>/mcpService/external/v1/auth/devUC/authorizeUrl?fromSource=<key>`。
2. 自动打开默认浏览器（`--no-browser` 则仅打印链接供手动复制）。Windows 上用 `powershell Start-Process` 启动浏览器，使其绑定到用户交互桌面会话；无桌面会话环境（Agent 沙箱、CI）请使用 `--no-browser`。
3. 用户在浏览器完成授权，CLI 轮询后端 `GET <market>/mcpService/external/v1/auth/devUC/token?fromSource=<key>` 获取 token。
4. token 本地保存到 `~/.coreinsight-cli/session.json`（目录 0700、文件 0600），不设过期时间；后续业务请求通过 `X-Auth-Token` 头发送。
5. `auth status` 的退出码为：`0` 已认证，`3` 未认证。

devUC token 是市场域凭证，由后端直接作为业务鉴权，无需额外 Cookie。`fromSource` 由 `SHA256(username + hostname + salt)` 派生，使用 coreinsight-cli 自有 salt，与 agentcenter 会话隔离。可用 `--market-server` 或 `COREINSIGHT_MARKET_SERVER` 覆盖市场服务地址（默认 `https://agent.huawei.com`）。

> **登录前置要求**：`rag`、`skill`、`experience` 的所有业务命令都必须在本地存在登录会话后才能执行。未登录或会话过期时，命令在发出任何网络请求前即被拦截，返回 JSON-RPC 错误 `-32002 Not Authenticated`，退出码为 `3`。`auth`、`version` 及所有 `--help` 帮助输出不受影响。

## 2. 知识问答 / 检索（rag）

`rag` 命令同时提供问答模式和纯检索模式，通过互斥开关 `--qa` / `--retrieve` 区分，两者必须指定其一且不能同时指定：`--qa` 走问答接口，`--retrieve` 走纯检索接口。旧命令 `qa`、`retrieve` 已合并移除。

四个路由参数 `--kb_sns`、`--products`、`--departments`、`--scenes` 必须且只能选择一个；多个值用逗号分隔。`user_id` 需使用 `--user_id` 显式指定（浏览器登录不保存工号）。

```bash
# 问答模式
coreinsight-cli rag \
  --qa \
  --query "问题" \
  --kb_sns 123,456 \
  --prompt_template SDD

# 纯检索模式
coreinsight-cli rag \
  --retrieve \
  --query "问题" \
  --products UPCF,UDG
```

## 3. Skill

```bash
coreinsight-cli skill scenes --repo .

# 仅查询代码仓归属的产品列表（第一步）
coreinsight-cli skill offering --repo .
coreinsight-cli skill offering --http_url https://codehub-y.huawei.com/CSP/CSPCertSDK_C

# 按产品 offering_id 查询场景（第二步，dim_code 取上一步返回的 offering_id）
coreinsight-cli skill product-scenes --dim_code P001

# 查询某场景下的 skill 列表（第三步）
coreinsight-cli skill scene-skills \
  --first_scene "需求开发" \
  --second_scene "MML开发" \
  --dim_type "产品级" \
  --dim_name "UPCF"

coreinsight-cli skill search \
  --query "数据库" \
  --page 1 \
  --page_size 6 \
  --sort_by downloads \
  --sort_order desc

coreinsight-cli skill download \
  --skill_id "bca61fb0-3734-49c0-906e-0209d17032fd" \
  --version "0.0.1" \
  --output ./skills

coreinsight-cli skill upload --file ./skill.zip --business_dimension "产品级"
coreinsight-cli skill verify --file ./skill.zip --business_dimension "产品级"
```

`skill scenes` 会读取 `git remote get-url origin`，把 SSH/SCP 地址规范化成 HTTP(S)，分页查询产品，再通过 Chat 服务的 `POST /experience/harness/scenes` 查询场景。默认完整 URL 为 `https://coreinsight.rnd.huawei.com/chat/experience/harness/scenes`。产品接口固定使用 `pageSize=20`；`offering_cn_name` 原样作为产品名使用，不做 trim。

`skill offering` 只执行 `skill scenes` 的第一步：把代码仓 URL（由 `--repo` 解析 origin，或由 `--http_url` 直接指定）发送到 Core Harness `POST /core-harness/api/v1/offering/list`，body 为 `{page, pageSize=20, http_url}`，返回该代码仓归属的产品列表。该产品列表请求不携带会话 Cookie，但仍需通过本地登录校验（与 `skill scenes` 一致）。

`skill product-scenes` 执行第二步：用 `--dim_code`（取上一步返回的 `offering_id`）调用 Chat `POST /experience/harness/scenes`，body 为 `{dimCode}`，携带会话 Cookie，返回该产品的场景列表。

`skill scene-skills` 执行第三步：用 `--first_scene`、`--second_scene`、`--dim_type`、`--dim_name` 调用 Chat `POST /experience/harness/scene/skills`，body 为 `{firstScene, secondScene, dimType, dimName}`，携带登录会话凭证，返回该场景下的 skill 列表。四个参数均必填并独立透传；产品级查询使用 `--dim_type "产品级"`，`--dim_name` 取产品名（`offering_cn_name`），场景名称取该产品返回的场景列表。

`skill scene-search` 已移除，不保留别名或关键词拼接逻辑。已有场景查询调用请迁移到 `skill scene-skills`，将原 `--product` 改为 `--dim_name`，并补充 `--dim_type "产品级"`。

`skill search` 用于通用关键词检索，调用 AI Community 的 `GET /aiapp-v2/api/skills`，固定 `searchMode=smart`，并发送 `pageNum`、`pageSize`、`sortBy`、`sortOrder`、`keyword`。默认完整 URL 为 `https://aicommunity.coreai.rnd.huawei.com/aiapp-v2/api/skills`。需要查询指定产品和场景下的 Skill 时使用 `skill scene-skills`。

`skill download` 先从 AI Community 获取下载 URL，再使用普通 GET 下载 ZIP。现有接口材料没有定义这个动态下载 URL 的额外 Header/鉴权要求，因此 CLI 不向该 URL 转发 Core Insight Cookie。

## 4. 经验

```bash
coreinsight-cli experience search \
  --query "智能客服系统" \
  --page 1 \
  --page_size 5 \
  --scene ALL

# 指定检索字段为 rag_search_text（默认 title，按标题检索）
coreinsight-cli experience search \
  --query "智能客服系统" \
  --search_field rag_search_text \
  --scene ALL

# 按部门过滤（后端映射 product.pdu_name，多个用逗号分隔）
coreinsight-cli experience search \
  --query "智能客服系统" \
  --departments "云核心网维护部,网络产品部" \
  --scene ALL

# 按产品名称过滤（后端映射 product.product_name，多个用逗号分隔）
coreinsight-cli experience search \
  --query "智能客服系统" \
  --products "UPCF,UDG" \
  --scene ALL

coreinsight-cli experience upload \
  --scene "test" \
  --scene_id "scene-001" \
  --title "在U2020上执行MML命令" \
  --summary "完整字段示例，包含向量化文本与元数据" \
  --experience "在该场景下积累的经验点" \
  --rag_search_text "MML命令 U2020 执行"
```

经验上传的核心请求字段为 `user_id`、`title`、`summary`、`scene_id`、`scene`、`experience`、`rag_search_text`。`user_id` 需使用 `--user_id` 显式指定（浏览器登录不保存工号，无法默认推导）。成功响应中的 `data` 为经验 ID 字符串，CLI 会原样保留在 JSON-RPC 的 `result` 中，不要求版本信息对象。旧的 `--doc_id` 和产品字段参数保留透传兼容；新接口是否支持这些扩展字段、是否有覆盖语义，需以后端契约为准。

```json
{"code":200,"msg":"success","data":"3659f967-a733-4045-9b09-c3ba2ad8d9a1"}
```

## 5. OKF 知识中心

当前材料只给出了“知识配置”标题，没有提供对应 Endpoint、Method、请求体或响应契约，因此本版本没有臆造 OKF 命令。补充 API 契约后可以按相同模式继续扩展。

经验检索调用 `POST /chat/experience/search`，请求体使用 `user_id`、`caller_id`、`page`、`page_size`、`source`、`title`、`search_field`、`scene`，以及可选过滤字段 `dept`、`products`。其中 `title` 取 `--query`（查询文本），`search_field` 取 `--search_field`（检索字段，默认 `title`，可指定 `rag_search_text` 按补充检索文本检索），`caller_id` 为调用方工号（需使用 `--caller_id` 显式指定，浏览器登录不保存工号），`user_id` 为按贡献者过滤的经验（仅使用显式传入的值，未指定则为空，表示检索所有用户的经验）。`--user_id` 用于过滤目标贡献者的经验，不会改变调用方工号。不指定 `--search_field` 时默认按 `title` 检索。

`--departments` 映射请求体 `dept`（后端转发为 `product.pdu_name` 过滤），`--products` 映射请求体 `products`（后端转发为 `product.product_name` 过滤）；两者均支持逗号分隔的多个值，且**可同时指定叠加过滤**（dept 与产品同时命中才返回）。两者都是可选过滤条件，未指定时按现有逻辑检索全部经验。

所有 HTTP/HTTPS 请求均使用统一客户端，并按当前内部环境要求关闭 TLS 证书校验。

## 环境变量

- `COREINSIGHT_SERVER`
- `COREINSIGHT_CHAT_SERVER`
- `COREINSIGHT_AUTH_SERVER`
- `COREINSIGHT_MARKET_SERVER`（devUC 浏览器授权登录的市场服务地址，默认 `https://agent.huawei.com`）
- `COREINSIGHT_AI_COMMUNITY_SERVER`
- `COREINSIGHT_CORE_HARNESS_SERVER`
- `COREINSIGHT_TIMEOUT`

同名全局参数可覆盖服务地址和超时，例如：

```bash
coreinsight-cli --server https://example.internal --timeout 300 rag --qa ...
```

## 405 / 问答超时排查

经验上传调用 `POST https://coreinsight.rnd.huawei.com/chat/experience/experience_add`，使用 Chat 服务基址。通过 `--chat-server` 或 `COREINSIGHT_CHAT_SERVER` 可覆盖基址（需包含 `/chat`）；CLI 追加 `/experience/experience_add`。此前临时新增的 `--memory-server` / `COREINSIGHT_MEMORY_SERVER` 已移除，请改用 Chat 配置。上传不自动重试。

```bash
# 下面地址是占位示例，必须替换成后端确认的真实服务基址
coreinsight-cli --chat-server https://coreinsight.rnd.huawei.com/chat experience upload \
  --scene test --scene_id scene-001 --title 标题 --summary 摘要 --experience 内容

coreinsight-cli --chat-server https://chat.example.internal/chat --debug rag \
  --qa --query "问题" --kb_sns 123
```

- `--chat-server` 指向 Chat 基址（默认 `<COREINSIGHT_SERVER>/chat`），CLI 会追加 `/app/support/app/chat` 或 `/app/support/app/retrieve`。环境变量末尾的斜杠会先移除，避免生成 `//chat`。
- JSON 业务请求遇到会把 POST 改为 GET 的 301/302/303 时，直接报告重定向及目标地址；同方法的 307/308 仍可跟随。登录请求保持参考 CLI 的行为。
- 405 错误包含实际请求方法、URL 和后端返回的 `Allow`（若存在）。应结合网关路由检查，不能仅凭 405 判定为“接口限制”。
- 网络错误区分 DNS、连接失败和一般超时，并保留底层错误。一般超时不能直接证明服务器不可达，也可能是后端等待过久。核对内网/VPN、DNS、代理及 `NO_PROXY`，确认地址正确后再考虑 `--timeout`。
- `--debug` 会输出业务请求体，请对分享的日志脱敏。

回归测试使用本地 HTTP 服务检查上传、rag 问答/检索请求及重定向行为。它们不验证华为内网服务是否可达，也不验证部署侧网关映射。

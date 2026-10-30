# coreinsight-cli

面向 AI Agent / 脚本的 Core Insight 命令行工具。Go 1.21、零第三方依赖、本地会话、JSON-RPC 2.0 输出。

## 构建

```bash
go build -o coreinsight-cli ./cmd/coreinsight-cli
```

Windows 使用 `go build -o coreinsight-cli.exe ./cmd/coreinsight-cli`。更新源码后需重新构建并替换实际使用的可执行文件，同时更新 Agent 加载的 `SKILL.md`。

`coreinsight-cli version` 不硬编码发布版本：优先使用发布流水线通过 `-ldflags "-X coreinsight-cli/internal/cli.Version=<实际发布版本>"` 注入的值，其次使用 Go 模块版本；源码构建使用 `dev+<Git 提交前 12 位>`，有未提交变更时追加 `.dirty`。没有构建元数据时只显示 `dev`，不冒充已发布版本。输出同时包含可用的 `commit`、`commit_time`、`modified` 和 `go_version`。流水线还可注入 `coreinsight-cli/internal/cli.Commit` 和 `coreinsight-cli/internal/cli.BuildTime`；提交时间与构建时间分开报告。

## 1. 登录

CLI **默认尝试打开浏览器**完成 devUC 授权，用 `--caller_id` 输入一次当前 SSO 账号的工号。也支持显式选择账号密码直接登录：传入 `--username`（或 `-u`）、`--password`（或 `-p`）、`--password-stdin` 中任一账密参数，或使用 `--method password`。仅设置账号密码环境变量不会改变默认浏览器方式；`--method browser` 不能与账密参数混用。

浏览器登录每次创建新的授权事务，业务请求使用保存的 token，通过 `X-Auth-Token` 头鉴权；账号密码登录保存 Cookie，业务请求使用 `Cookie` 头。切换登录方式成功后替换旧凭证，失败时保留原会话。

```bash
# 默认：自动打开浏览器完成授权
coreinsight-cli auth login --caller_id "<当前 SSO 账号工号>"

# 用户明确要求手动打开，或已确认无桌面环境时：打印链接，继续等待本次授权
coreinsight-cli auth login --caller_id "<当前 SSO 账号工号>" --no-browser

# 自定义授权轮询超时秒数（默认 60）
coreinsight-cli auth login --caller_id "<当前 SSO 账号工号>" --timeout 120

# 仅检查本地凭证，不请求服务端验证
coreinsight-cli auth status

# 仅清除本地会话，不撤销服务端 token 或浏览器 SSO
coreinsight-cli auth logout
```

浏览器登录流程：

1. 使用加密随机源生成本次登录专用的 256 位 `fromSource`（64 位十六进制字符串），请求 `GET <market>/mcpService/external/v1/auth/devUC/authorizeUrl?fromSource=<key>`。不再使用用户名和主机名派生的固定标识。
2. 校验授权 URL 后，尝试打开默认浏览器，并向 stderr 打印链接。Windows 使用 `powershell Start-Process`；CLI 等待启动命令退出并报告错误。启动命令成功不保证窗口可见，也不证明用户已确认。不能仅因命令由 Agent/Bash 子进程执行就默认加 `--no-browser`。
3. 用本次相同的 `fromSource` 轮询 `GET <market>/mcpService/external/v1/auth/devUC/token?fromSource=<key>`。已有浏览器 SSO 可能自动完成授权，不一定再次要求输入密码或点击确认。`--no-browser` 仅关闭自动打开，不跳过授权；应在命令等待期间手动打开本次链接。
4. 严格检查 HTTP 状态、响应类型及 JSON 业务成功状态。空响应、JSON `null`、成功响应中的空 `data` / `data: null` 继续等待；HTTP/业务错误、HTML、对象/数字类型 token、无效 token 格式均不会保存。保留纯字符串 token 格式兼容，不把错误 JSON 整体转成 token。
5. 只在收到格式有效的 token 后，将 token 与本次输入的 `caller_id` 一起保存到 `~/.coreinsight-cli/session.json`（请求创建目录 0700、文件 0600；实际权限遵循操作系统）。授权失败保留已有会话，不能据旧会话的 `auth status` 把这次失败说成成功。

devUC token 是市场域凭证，无需额外 Cookie。可用 `--market-server` 或 `COREINSIGHT_MARKET_SERVER` 覆盖市场地址（默认 `https://agent.huawei.com`）。`auth login --caller_id <工号> --timeout 120` 设置 token 轮询总时限；放在命令之前的 `--timeout` 设置单次 HTTP 请求超时，例如 `coreinsight-cli --timeout 15 auth login --caller_id "<工号>" --timeout 120`。

账号密码直接登录：

```bash
# 推荐：在本机交互终端无回显输入密码，不打开浏览器
coreinsight-cli auth login --username "<工号>"

# 兼容直接传入账号密码（密码可能留在命令历史和进程参数中）
coreinsight-cli auth login --username "<工号>" --password "<密码>"

# 脚本：从标准输入读取密码，例如由凭证管理器通过管道提供
coreinsight-cli auth login --username "<工号>" --password-stdin

# 已设置 COREINSIGHT_AUTH_USERNAME / COREINSIGHT_AUTH_PASSWORD 时，显式选用密码方式
coreinsight-cli auth login --method password
```

密码登录恢复既有接口：`POST https://omtool.rnd.huawei.com/api/auth/login`，JSON 请求体为 `{"userName":"<工号>","passwd":"<密码>"}`。可用 `--auth-server` / `COREINSIGHT_AUTH_SERVER` 覆盖基址，`--server` 也保留对该基址的覆盖兼容；`--market-server` 只影响浏览器授权。HTTP 超时使用命令前的 `--timeout`，例如 `coreinsight-cli --timeout 30 auth login --username "<工号>"`；密码方式不能混用 `--no-browser` 或登录后的轮询 `--timeout`。

用户名优先取 `--username/-u`，未传时取 `COREINSIGHT_AUTH_USERNAME`。密码优先取显式 `--password/-p` 或 `--password-stdin`（二者互斥），未传时取 `COREINSIGHT_AUTH_PASSWORD`，最后尝试终端无回显输入；无法关闭回显时返回错误。不会保存密码，登录调试日志和错误不输出密码或响应原文。密码请求不跟随重定向；必须收到成功 HTTP 状态、明确的 JSON 业务成功标记和有效 Cookie 才保存。Cookie 的有效期取服务端声明，不把未提供有效期解释为永久有效。

密码登录将 `--username` 工号自动绑定为 `caller_id`；不必另传 `--caller_id`，若传入则必须与用户名一致。后续仍自动携带调用者工号，`--user_id` 只用于检索过滤。旧的原始请求体 `--body` / `--body-file` 和 `OMRES_AUTH_*` 环境变量不属于恢复的登录入口。

认证输出说明：

| 字段 / 退出码 | 含义 |
| --- | --- |
| `authenticated: true`、`credential_state: "stored"` | 为兼容旧脚本保留；表示本地持有格式有效且未标记过期的凭证，不证明业务接口接受它 |
| `server_verified: false` | 未通过业务接口或身份接口验证凭证；CLI 没有臆造在线验证接口 |
| `browser_launcher_succeeded` | 本次登录的浏览器启动命令是否成功退出，不等于窗口可见或用户点击了授权 |
| `saved_at` | 本地写入凭证的时间，不是浏览器授权时间 |
| `auth status` 退出码 `0` / `3` | 本地凭证通过检查 / 缺失、格式无效或过期；不请求服务端 |

调用者与过滤字段分开处理：

| 字段 | 来源与用途 |
| --- | --- |
| 会话 `caller_id` | 浏览器方式从 `--caller_id` 输入，与 token 共同保存（devUC 未核验其与 SSO 身份的一致性）；密码方式自动取登录 `username`，与 Cookie 共同保存。后续调用自动读取 |
| 检索 `--user_id` | 可选过滤条件；不传则不按用户过滤，绝不自动填登录工号 |
| 业务命令 `--caller_id` | 可选的一致性校验参数；只有与会话相同才允许执行，不能覆盖调用者身份 |

`auth status` 输出 `caller_id` 和 `caller_id_bound`。旧版只有 token 的会话会显示 `caller_id_bound: false`；执行需要调用者身份的任务前，重新登录绑定工号。旧 Cookie 会话可继续读取已保存的 `username` 作为调用者。新登录不沿用上一次会话的工号；登录失败时保留原凭证和原工号。

升级后，旧版本误存的 `<nil>`、错误 JSON 等明显无效凭证会被本地检查拦截。用户要求重新登录时执行 `auth login --caller_id <工号>`；正常执行已授权的业务任务时可以复用现有本地凭证。服务端仍可撤销或拒绝凭证，以业务请求的实际响应为准。

> **登录前置要求**：`rag`、`skill`、`experience` 的所有业务命令都必须通过本地会话检查。会话缺失、无效或过期时，命令在发出网络请求前返回 JSON-RPC 错误 `-32002`，退出码 `3`。`auth`、`version` 及所有 `--help` 帮助输出不受影响。

## 2. 知识问答 / 检索（rag）

`rag` 命令同时提供问答模式和纯检索模式，通过互斥开关 `--qa` / `--retrieve` 区分，两者必须指定其一且不能同时指定：`--qa` 走问答接口，`--retrieve` 走纯检索接口。旧命令 `qa`、`retrieve` 已合并移除。

四个路由参数 `--kb_sns`、`--products`、`--departments`、`--scenes` 必须且只能选择一个；多个值用逗号分隔。`caller_id` 自动取登录会话；`--user_id` 是可选的检索过滤字段，未指定时请求中的 `user_id` 为空，不按用户过滤。

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
  --output ./skills

coreinsight-cli skill upload --file ./skill.zip --business_dimension "产品级"
coreinsight-cli skill verify --file ./skill.zip --business_dimension "产品级"
```

`skill scenes --repo` 的参数必须是本地 Git 仓库目录（例如 `.` 或 `D:\projects\uscdb`），不能传远程 URL；只有 URL 时使用上面的 `offering --http_url` 和 `product-scenes` 两步查询。`skill scenes` 会读取 `git remote get-url origin`，把 SSH/SCP 地址规范化成 HTTP(S)，分页查询产品，再通过 Chat 服务的 `POST /experience/harness/scenes` 查询场景。默认完整 URL 为 `https://coreinsight.rnd.huawei.com/chat/experience/harness/scenes`。产品接口固定使用 `pageSize=20`；`offering_cn_name` 原样作为产品名使用，不做 trim。

`skill offering` 只执行 `skill scenes` 的第一步：把代码仓 URL（由 `--repo` 解析 origin，或由 `--http_url` 直接指定）发送到 Core Harness `POST /core-harness/api/v1/offering/list`，body 为 `{page, pageSize=20, http_url}`，返回该代码仓归属的产品列表。该产品列表请求不携带会话 Cookie，但仍需通过本地登录校验（与 `skill scenes` 一致）。

`skill product-scenes` 执行第二步：用 `--dim_code`（取上一步返回的 `offering_id`）调用 Chat `POST /experience/harness/scenes`，body 为 `{dimCode}`，携带登录凭证（浏览器登录使用 `X-Auth-Token`，账号密码登录使用 Cookie），返回该产品的场景列表。

`skill scene-skills` 执行第三步：用 `--first_scene`、`--second_scene`、`--dim_type`、`--dim_name` 调用 Chat `POST /experience/harness/scene/skills`，body 为 `{firstScene, secondScene, dimType, dimName}`，携带登录凭证（浏览器登录使用 `X-Auth-Token`，账号密码登录使用 Cookie），返回该场景下的 skill 列表。四个参数均必填并独立透传；产品级查询使用 `--dim_type "产品级"`，`--dim_name` 取产品名（`offering_cn_name`），场景名称取该产品返回的场景列表。

`skill scene-search` 已移除，不保留别名或关键词拼接逻辑。已有场景查询调用请迁移到 `skill scene-skills`，将原 `--product` 改为 `--dim_name`，并补充 `--dim_type "产品级"`。

`skill search` 用于通用关键词检索，调用 AI Community 的 `GET /aiapp-v2/api/skills`，固定 `searchMode=smart`，并发送 `pageNum`、`pageSize`、`sortBy`、`sortOrder`、`keyword`。默认完整 URL 为 `https://aicommunity.coreai.rnd.huawei.com/aiapp-v2/api/skills`。需要查询指定产品和场景下的 Skill 时使用 `skill scene-skills`。

`skill download` 的 `--version` 可选：需要固定版本时使用真实检索结果中的版本，不传时交由服务端选择，CLI 不补固定版本或 `latest`；输出 `version: null` 表示没有显式指定，不代表已经查明服务端最终选择的版本。

Skill 下载/上传/校验自动从会话读取 `caller_id`，并映射为市场接口既有的 `userId` 参数；这些命令不接受 `--user_id`。

`skill download` 先从 AI Community 获取下载 URL，再使用普通 GET 下载 ZIP。现有接口材料没有定义这个动态下载 URL 的额外 Header/鉴权要求，因此 CLI 不向该 URL 转发 Core Insight Cookie。

## 4. 经验

```bash
coreinsight-cli experience search \
  --query "智能客服系统" \
  --page 1 \
  --page_size 5 \
  --scene ALL

# 仅按指定贡献者过滤，调用者仍取当前登录会话
coreinsight-cli experience search --query "智能客服系统" --user_id "<目标贡献者工号>"

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

经验上传的核心请求字段为 `user_id`、`title`、`summary`、`scene_id`、`scene`、`experience`、`rag_search_text`。该写入接口的既有 `user_id` 字段表示作者，由 CLI 将会话 `caller_id` 内部映射填入；上传命令不再接受 `--user_id` 覆盖作者。检索命令的 `--user_id` 仍只表示过滤条件。成功响应中的 `data` 为经验 ID 字符串，CLI 会原样保留在 JSON-RPC 的 `result` 中，不要求版本信息对象。旧的 `--doc_id` 和产品字段参数保留透传兼容；新接口是否支持这些扩展字段、是否有覆盖语义，需以后端契约为准。

```json
{"code":200,"msg":"success","data":"3659f967-a733-4045-9b09-c3ba2ad8d9a1"}
```

经验检索调用 `POST /chat/experience/search`，请求体使用 `user_id`、`caller_id`、`page`、`page_size`、`source`、`title`、`search_field`、`scene`，以及可选过滤字段 `dept`、`products`。其中 `title` 取 `--query`（查询文本），`search_field` 取 `--search_field`（检索字段，默认 `title`，可指定 `rag_search_text` 按补充检索文本检索），`caller_id` 为调用方工号（自动取登录会话，业务参数只能校验一致性，不能覆盖），`user_id` 为按贡献者过滤的经验（仅使用显式传入的值，未指定则为空，表示检索所有用户的经验）。`--user_id` 用于过滤目标贡献者的经验，不会改变调用方工号。不指定 `--search_field` 时默认按 `title` 检索。

`--departments` 映射请求体 `dept`（后端转发为 `product.pdu_name` 过滤），`--products` 映射请求体 `products`（后端转发为 `product.product_name` 过滤）；两者均支持逗号分隔的多个值，且**可同时指定叠加过滤**（dept 与产品同时命中才返回）。两者都是可选过滤条件，未指定时按现有逻辑检索全部经验。

## 5. OKF 知识中心

当前材料只给出了“知识配置”标题，没有提供对应 Endpoint、Method、请求体或响应契约，因此本版本没有臆造 OKF 命令。补充 API 契约后可以按相同模式继续扩展。

所有 HTTP/HTTPS 请求均使用统一客户端，并按当前内部环境要求关闭 TLS 证书校验。

## 环境变量

- `COREINSIGHT_SERVER`
- `COREINSIGHT_CHAT_SERVER`
- `COREINSIGHT_AUTH_SERVER`（账号密码登录基址，默认 `https://omtool.rnd.huawei.com`）
- `COREINSIGHT_AUTH_USERNAME` / `COREINSIGHT_AUTH_PASSWORD`（仅在显式选择密码登录后读取）
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
- JSON 业务请求遇到会把 POST 改为 GET 的 301/302/303 时，直接报告重定向及目标地址；同方法的 307/308 仍可跟随。账号密码登录请求不跟随任何重定向，浏览器授权流程保持不变。
- 405 错误包含实际请求方法、URL 和后端返回的 `Allow`（若存在）。应结合网关路由检查，不能仅凭 405 判定为“接口限制”。
- 网络错误区分 DNS、连接失败和一般超时，并保留底层错误。一般超时不能直接证明服务器不可达，也可能是后端等待过久。核对内网/VPN、DNS、代理及 `NO_PROXY`，确认地址正确后再考虑 `--timeout`。
- `--debug` 会输出业务请求体，请对分享的日志脱敏。

回归测试使用本地 HTTP 服务检查上传、rag 问答/检索请求及重定向行为。它们不验证华为内网服务是否可达，也不验证部署侧网关映射。

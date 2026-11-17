# Copilot 式输入补全与模型可配置性外部调研

## 调研问题

Zakki 想在用户输入笔记时提供 AI 辅助补全(ghost text / inline suggestions),并允许用户配置所选模型/服务商(自定义 API key、本地模型),体验对标 VSCode 中 GitHub Copilot 的效果。本报告调研四件事:(1) VSCode + Copilot 的 inline suggestions 交互方式、设置项与模型配置现状;(2) Cursor、Continue、Zed、JetBrains 等产品在「补全/聊天模型可配置」上的先例;(3) 若 Zakki 要做模型可配置的补全,后端可以接哪些 API(端点、流式协议、FIM 支持、可用状态);(4) 触发/防抖/IME 的通用参数与做法,以及输入时切换模型的 UI 形态。调研以官方文档、官方 changelog、API reference 为准,信息截至 2026-11,过时内容已随文标注。

---

## 一、VSCode/GitHub Copilot 的输入体验与模型配置

### 1.1 Inline suggestions(ghost text / NES)的工作方式

Copilot 在编辑器内提供两类建议 [1]:

- **Ghost text suggestions**:光标处以灰色暗淡文本提示如何补全当前行或整段代码;
- **Next edit suggestions(NES)**:基于最近编辑,建议文件内(光标处或其他位置)的下一处修改,装订线(gutter)以箭头指示建议位置。

交互约定 [1]:

| 动作 | 键位/命令 |
|---|---|
| 接受整条建议 | `Tab` |
| 拒绝 | `Esc`(继续打字也会隐去) |
| 部分接受(词级) | `⌘→`(Windows/Linux `Ctrl+Right`) |
| 部分接受(行级) | 命令面板 **Accept Next Line Of Inline Suggestion** |
| 多个候选择一 | 悬停建议弹出工具条,前/后切换 alternative suggestions |
| 临时关闭 | 状态栏 Copilot 菜单 **Snooze**(每次 +5 分钟) |
| 手动触发 | 命令 **Trigger Inline Suggestion**(计量网络下自动请求被停时仍可用) |

NES 专有:NES 先 `Tab` 跳到建议处、再 `Tab` 接受;`editor.inlineSuggest.edits.showCollapsed` 可让改动先折叠。计量连接(metered connection)下 Copilot 不发起新的自动 ghost text / NES 请求,进行中的请求继续,可手动触发 [1]。Copilot 的上下文是当前文件与打开的文件 [1]。触发防抖:官方文档未公开 Copilot 内部防抖值;VSCode 提供编辑器侧设置 `editor.inlineSuggest.minShowDelay`(建议显示前等待,默认 `0`)[1],Zed 以 75ms 作为其 Copilot provider 的默认防抖(见第四节)。

### 1.2 相关 VSCode 设置项(官方默认值)[1][40]

| 设置 | 作用 | 默认值 |
|---|---|---|
| `github.copilot.enable` | 按语言开/关 inline completions(对象,键为语言 id) | 文档未列默认值;按语言或 `*` 设 true/false |
| `github.copilot.nextEditSuggestions.enabled` | 启用 NES | 需确认开启 |
| `github.copilot.nextEditSuggestions.fixes` | 基于诊断(波浪线)的 NES,如缺失 import | — |
| `editor.inlineSuggest.enabled` | 编辑器 inline suggestion 总开关(第三方补全也依赖,Continue 故障排查清单要求其为 true)[15] | `true` |
| `editor.inlineSuggest.fontFamily` | ghost text 字体 | — |
| `editor.inlineSuggest.showToolbar` | 建议上方工具条 | — |
| `editor.inlineSuggest.syntaxHighlightingEnabled` | ghost text 语法高亮 | — |
| `editor.inlineSuggest.minShowDelay` | 建议显示前等待毫秒数 | `0` |
| `editor.inlineSuggest.edits.showCollapsed` | NES 改动先折叠 | — |
| `editor.inlineSuggest.edits.allowCodeShifting` | 允许 NES 平移现有代码 | — |
| `editor.inlineSuggest.edits.renderSideBySide` | 大块 NES 建议 side-by-side 展示(`auto`/`never`) | `auto` |

注:`github.copilot.inlineSuggest.delay` 等旧设置已不在现行文档中,不要引用。

### 1.3 模型可配置性

**补全(inline suggestions)模型可换,但仅限 Copilot 服务端提供的「补全专用模型」**:

- VSCode 中通过命令 **GitHub Copilot: Change Completions Model**(或 Chat 菜单 → Configure Inline Suggestions… → Change Completions Model…)切换;设置里对应 **GitHub > Copilot: Selected Completion Model**,留空即用默认模型 [4]。JetBrains/Visual Studio 也有对应下拉 [4]。
- 可选列表随时间变化;只有一个补全模型时 picker 只显示它;Business/Enterprise 需管理员开启 Editor Preview Features [4]。
- 补全模型演进:2025-08-27 起 code completion 默认使用 **GPT-4.1 Copilot** 模型(经 RLHF 按用户反馈精调)[5];2025-10-17 又发布其 10 月更新版 [6]。

**Chat 的模型选择器(model picker)能力更丰富** [2]:

- 聊天输入框内的 model picker 切换对话/编辑模型;**Auto** 自动按任务复杂度与实时可用性路由(2025-09-15 预览 [8],2025-12-10 GA,可路由到 GPT-5.1-Codex-Max、GPT-5 mini、GPT-4.1、Sonnet 4.5、Haiku 4.5 等,随计划与策略变化)[7];Copilot Fusion 分层(Standard/High/Max);推理模型可在 picker 内配 thinking effort 与 context size;组织可托管默认模型。
- **Language Models 管理编辑器**:查看全部模型、按 `@provider:`/`@capability:` 过滤、眼睛图标控制 picker 可见性、置顶收藏 [2]。

**BYOK(Bring Your Own Key)——关键结论:仅作用于 chat,不作用于 inline completions** [2]:

> "Some features still require a GitHub account: semantic search, inline suggestions (code completions), and features that rely on embeddings. BYOK applies to the chat experience and utility tasks only."

- 接入方式三选一 [2]:内置 provider(Azure、Anthropic、Gemini、OpenAI 等);model provider 扩展(如官方 **Ollama 扩展**——内置 Ollama provider 已废弃,建议装 Ollama 官方扩展;AI Toolkit/Foundry 等);**Custom Endpoint**(vendor `customendpoint`,支持 `chat-completions` / `responses` / `messages` 三种 API 类型,配置写在 `chatLanguageModels.json`,可配 `modelOptions`(temperature 等)、`requestHeaders`、context window;旧的 "OpenAI Compatible" provider 与 `github.copilot.chat.customOAIModels` 已废弃)。
- BYOK 模型在 Local harness 会话下无需 GitHub 登录、无需 Copilot 订阅,可离线用 Ollama [2]。
- **本地/端侧**:除 BYOK 外,Copilot 还提供官方 on-device 模型(当前为 MAI Code 1.1 Flash,VSCode 用 llama.cpp 本地起服务;要求 NVIDIA GPU ≥96GB 显存、约 55GB 磁盘;可被 Auto 路由并回落云端)[2]——说明「云端服务 + 本地推理」可以共存于一个 picker。

**对 Zakki 的启示**:Copilot 的形态是「补全模型 = 服务方管理的专用模型池内可选;BYOK/本地 = 仅聊天」。若 Zakki 想让补全也 BYOK/本地,业界已有 Zed/Continue/JetBrains 先例(见第二、三节),即补全走 OpenAI-compatible completions + 客户端拼 FIM 模板,或接专用 FIM 端点。

---

## 二、其他产品先例(对比表)

| 产品 | 补全(inline)可选模型 | 聊天/Agent 可选模型 | BYOK(自定义 API key) | 本地模型 | 补全是否区分「专用模型」角色 |
|---|---|---|---|---|---|
| **GitHub Copilot**(VSCode) | 可选,但仅 Copilot 服务端补全模型(GPT-4.1 Copilot 等)[4][5] | 可选(Copilot 模型 + Auto/Fusion)[2][7] | 仅 chat;**不**作用于补全 [2] | on-device 模型(聊天,AI 0 credits);BYOK 本地(Ollama)仅 chat [2] | 是,补全模型独立于 chat 模型 |
| **Cursor** | **不可换**:Tab 用内置专用模型 | 可选(内置 + BYO key 模型进 picker) | OpenAI、Anthropic、Google、Azure、AWS Bedrock;**仅 chat** [13] | 不支持 [13] | 是(Tab 专用模型,用户不可配)[13][14] |
| **Continue** | 可选:config 里任意 `roles: [autocomplete]` 模型 | 可选(`roles: [chat]` 等) | 广泛(各 provider + OpenAI 兼容端点)[15][16] | Ollama 本地(官方推荐 qwen2.5-coder)[15] | 是:明确 `autocomplete` 角色,要求 FIM 模型,chat 模型「即使提示再充分也常表现差」[17] |
| **Zed** | 可选:`edit_predictions.provider` = zeta(默认)/copilot/mercury/codestral/ollama/open_ai_compatible_api | 可选:Zed 托管、API access、订阅登录(ChatGPT/Claude/Copilot)、网关(OpenRouter 等)、本地 [20][21] | 是(agent 走 provider API key;补全走 Mercury/Codestral key) [19][21] | llama.cpp、LM Studio、Ollama、任意本地 OpenAI 兼容 server(chat + 补成都支持)[19][21] | 是:edit_predictions 与 agent 完全独立配置,且按模型适配 FIM prompt 模板(prompt_format)[19] |
| **JetBrains AI Assistant** | 可选:AI Completion 默认 Mellum(JetBrains 自研),可切换为任一 **OpenAI 兼容端点**的补全模型 [22] | 可选(JetBrains AI 云端 + BYOK + 本地)[22][23] | Anthropic、Google(Gemini API/Vertex)、OpenAI、任意 OpenAI 兼容端点(如 LiteLLM);本地 Ollama/LM Studio [22][23] | Ollama、LM Studio(chat/功能);补全需走 OpenAI 兼容端点 [22] | 是:独立「AI Completion」配置项,要求 FIM(code completion model)或 FIM+edit(next edit prediction model),并按模型自动选 Prompt schema;明确「通用 chat 模型通常不合适」[22] |

要点补充:

- **Cursor**:Tab 支持多行修改、补 import、接受后再按 `Tab` 跳到下一个编辑点(jump-in-file)甚至跨文件编辑;`Tab` 接受、`Esc` 拒绝、`Cmd+→` 词级接受;可按文件类型/全局/定时 Snooze 关闭 [14]。BYOK 的 key 随请求发给 Cursor 后端(最终 prompt 在其服务器拼装,不落盘存储)[13];Teams/Enterprise 对第三方模型按 Token Rate 计费 [13]。
- **Continue**:`config.yaml` 的 models 块用 `roles` 声明用途(chat / autocomplete / edit / apply / embed / rerank),面板内可按角色下拉切换当前生效模型 [16];autocomplete 推荐 Qwen2.5-Coder(1.5B/7B)、Codestral、Mercury Coder,强调「补全模型 ≤10B 足够,更大不显著提升」[15][17];可自定义 `promptTemplates.autocomplete`(Handlebars,prefix/suffix 变量)[17];thinking 模型不适合补全,可用 `extraBodyProperties: {think: false}` 关闭 [15]。
- **Zed**:补全显示分 `eager`(默认,默认)与 `subtle`(按住 alt 才显示)两种模式;接受 `alt-tab`/`tab`,词级 `ctrl-cmd-right`/`alt-k`,行级 `ctrl-cmd-down`/`alt-j`;Copilot provider 下还可在多个候选项间循环(alt-tab/alt-])[19]。open_ai_compatible_api 直接 POST `/v1/completions`,由客户端拼好 FIM prompt(`prompt_format` 支持 zeta/zeta2/code_llama/star_coder/deepseek_coder/qwen/code_gemma/codestral/glm/sweep/infer 自动识别)[19]。
- **JetBrains**:BYOK 于 2025-12 正式上线 [23];模型按「功能 → 分配/回退模型列表」匹配,不匹配则该功能不可用;OpenAI 兼容端点与本地模型需手动分配到 Core features / Instant helpers [22]。

---

## 三、可接入的 API 面

### 3.1 逐项现状

**OpenAI Chat Completions**(`POST /v1/chat/completions`):当前主力接口,`stream: true` 走 SSE(`content-type: text/event-stream`,`data: [DONE]` 结束)[24][25];无 FIM 参数,做 inline completion 需客户端自行把 prefix/suffix 拼进 prompt 并设 stop 序列(见 Anthropic 段的通用做法)。另有更新的 Responses API 可选 [25]。

**OpenAI 传统 Completions + suffix(原生 FIM)——已名存实亡(2026-09 关停)**:`POST /v1/completions` 支持 `suffix` 参数(prompt 视为 prefix,模型填充中间),官方指南至今保留该用法与调参建议 [25]。但该端点唯一兼容的模型 `gpt-3.5-turbo-instruct`、`babbage-002`、`davinci-002` 已于 **2026-09-28 关停**(其微调版本 2026-10-23 关停),官方指引迁移到 Responses API 或 Chat Completions [25]。⚠️ 结论:不要再基于「OpenAI + suffix」设计 FIM 功能。

**Mistral Codestral(专用 FIM 端点,GA)**:`POST /v1/fim/completions` [27][29]。参数:`model`(`codestral-latest` / `codestral-2508`)、`prompt`、`suffix`、`stream`(SSE,`data: [DONE]`)、`stop`、`temperature`(建议 0–0.2)、`top_p`、`max_tokens`、`min_tokens`、`random_seed`、`prompt_cache_key` [29]。Codestral 25.08:GA、128k 上下文、定价 input $0.3 / output $0.9 每百万 token,能力列表含 `fim`、`prefix` [28]。注意 Codestral 专用 key 与 Mistral 通用 key 不同;共用 `api.mistral.ai/v1` 时需改 apiBase(Continue 文档提示)[15]。

**DeepSeek(原生 FIM,Beta)**:`POST https://api.deepseek.com/beta/completions`,文档明确 "User must set base_url=https://api.deepseek.com/beta to use this feature" [30]。参数:`model`(`deepseek-flash` / `deepseek-v4-pro`)、`prompt`、`suffix`、`stream`(SSE)、`stream_options.include_usage`、`stop`(≤16)、`echo`(与 suffix 互斥)、`logprobs`(≤20)、temperature/top_p [30]。日常 chat 走 `https://api.deepseek.com`(OpenAI 格式)或 `https://api.deepseek.com/anthropic`(Anthropic Messages 格式)[31]。⚠️ 状态仍为 Beta,接口面可能变动。

**Anthropic(无 FIM)**:Messages API `POST /v1/messages`,SSE 流式,支持 assistant prefill(预填 assistant 消息开头)[32]。官方没有 fill-in-the-middle 端点/参数;社区工具的通用做法是:把 prefix/suffix 写进结构化 user 消息(或用 prefill 承载 prefix),配 stop 序列截断,流式接收,并在取消时 AbortController 中止在途请求 [33][34]。这正是 Continue/JetBrains 所说「chat 模型不适合补全」的场景——能用但质量与延迟都不如 FIM 模型 [17][22]。

**Ollama(本地/自托管)**:
- 原生 API `POST /api/generate`:`prompt` + `suffix`(原生 FIM,文档示例即 codellama 补全)、`raw: true` 跳过模板(适合客户端自己拼 FIM token)、`stream`(默认 true,NDJSON 行流)、`keep_alive`、`think`、`options`(temperature 等)[35]。
- OpenAI 兼容层 `POST /v1/completions`:官方特性表明确支持 **`suffix`** 字段与 `stream`、`stop`、`stream_options.include_usage` 等(prompt 目前仅接受字符串)[36];另有 `/v1/chat/completions`、`/v1/responses`。
- 新增 Anthropic 兼容层 `POST /v1/messages`(本地无需 key,占位即可;云端 `ollama.com/v1` 需 `OLLAMA_API_KEY`)[37]。
- ⚠️ 旧文档 `docs/openai.md` 已重构为 `docs/api/openai-compatibility.mdx`,引用时以新路径为准。

**llama.cpp server(本地)**:
- `POST /completion`:`prompt`(string/token 数组)、`suffix`、`cache_prompt`(默认 true,KV 缓存复用公共前缀,只重算差异部分——对逐键增量请求极友好)、`stop`、SSE 流式(`stream: true`)、`n_predict` 等 [38]。
- `POST /infill` 专用 FIM:`input_prefix`、`input_suffix`、`input_extra`(额外文件上下文 `[{filename,text}]`)、`prompt`(接在 FIM_MID 后),按模型 FIM token(`<PRE>…<SUF>…<MID>`)拼装,流式返回;`--spm-infill` 可切 SPM 模式 [38]。
- OpenAI 兼容:`/v1/chat/completions`、`/v1/completions`、router 模式 `llama serve` 按需加载模型并暴露 `/models/sse` [21][38]。

**LM Studio(本地)**:OpenAI 兼容端点(`/v1/chat/completions` 等);`/v1/completions` 按 "legacy" 提供并明确标注 "This endpoint is no longer supported by OpenAI. LM Studio continues to support it",不套用 prompt 模板、建议配 base 模型 [39]。

### 3.2 对比表

| 服务商/运行时 | 端点 | 请求形态 | 流式协议 | 原生 FIM | 当前状态(2026-11) |
|---|---|---|---|---|---|
| OpenAI Chat Completions | `POST /v1/chat/completions` | messages | SSE(`data: [DONE]`) | 无(客户端拼 prompt+stop) | 可用,主力 [24][25] |
| OpenAI legacy Completions | `POST /v1/completions` | prompt + suffix | SSE | 有(suffix) | **兼容模型已关停(2026-09-28),不可用** [25] |
| Mistral Codestral | `POST /v1/fim/completions` | prompt + suffix | SSE | 有 | GA(codestral-2508,128k ctx)[27][28] |
| DeepSeek FIM | `POST /completions` @ `api.deepseek.com/beta` | prompt + suffix | SSE | 有 | Beta [30] |
| Anthropic Messages | `POST /v1/messages` | messages(可 prefill) | SSE | 无 | 可用;补全需自行拼装 [32][33] |
| Ollama 原生 | `POST /api/generate` | prompt + suffix(+raw) | NDJSON | 有 | 可用(本地/云)[35] |
| Ollama OpenAI 层 | `POST /v1/completions` | prompt + suffix | SSE | 有(suffix 字段) | 可用 [36] |
| llama.cpp | `POST /completion`、`POST /infill` | prompt(+suffix)/input_prefix+input_suffix+input_extra | SSE | 有(/infill;/completion 亦有 cache_prompt) | 可用 [38] |
| LM Studio | `POST /v1/completions`(legacy)等 | prompt | SSE | 文档未列 suffix | 可用,标注 legacy [39] |

**OpenAI-compatible 生态约定**:Zed 的实践代表客户端侧通用模式——无论后端是谁,客户端按目标模型的 FIM 模板把 prefix/suffix 拼成完整 prompt,直接 `POST /v1/completions`,附 `max_tokens`、`temperature`(Zed 默认示例 0.2)、`stop`(FIM 结束 token 等)[19]。这使「任意 OpenAI 兼容 server(vLLM、llama.cpp、LocalAI、LM Studio)」都能当补全后端,比依赖各家 suffix 参数更通用。

### 3.3 时延与成本要点(官方/一手数值)

- **防抖(输入停顿后发请求)**:Zed 按 provider 设默认防抖——Copilot 75ms、Codestral 150ms、Zeta/Mercury/Ollama/OpenAI 兼容 0ms,均可经 `prediction_debounce` 覆盖,设 0 关闭额外等待;手动触发命令绕过防抖 [19]。Continue 源码默认 `debounceDelay: 350`(2026 main 分支;文档示例曾为 250),另有 `modelTimeout: 150`(毫秒,超时即放弃该次建议)[15][18]。VSCode 编辑器侧 `editor.inlineSuggest.minShowDelay` 默认 0(控制显示节奏而非请求节流)[1]。
- **上下文裁剪**:Continue 默认 `maxPromptTokens: 1024`,prefix 占比 0.3、suffix 占比上限 0.2(`prefixPercentage`/`maxSuffixPercentage`)[15][18];即补全请求通常只带 ~1k token,远小于 chat 请求。
- **输出长度与采样**:Zed 默认示例 `max_tokens: 256`、`temperature: 0.2` + FIM stop token [19];OpenAI suffix 用法建议 `max_tokens > 256`、优先 `finish_reason=="stop"`、困难场景重采样 3–5 次(成本随之放大)[25]。Continue 提醒:补全模型 ≤10B 足够,不必用大模型 [17]。
- **成本/性能杠杆**:Codestral 提供 `prompt_cache_key` [29];llama.cpp `cache_prompt` 默认开启,公共前缀免重算 [38];Ollama `keep_alive` 控制模型驻留 [35];Copilot 在计量网络下自动停发请求、仅手动触发 [1]。
- **请求取消**:VSCode 在新输入到来/建议作废时取消在途补全(第三方 provider 同样受 `Trigger Inline Suggestion` 等编辑器机制管理)[1];社区实现的惯例是每个请求绑定 AbortController,新请求开始或编辑器取消时中止 [33]。
- **TTFB 目标**:无单一官方数值;可从上述参数反推——防抖 75–350ms + 模型超时 ~150ms(Continue)意味着首 token 需在数百毫秒量级才有可用体验;这也是 Continue 推荐 qwen2.5-coder:1.5b、Codestral 宣传 low-latency 高频任务专用 [28]、JetBrains 要求补全模型而非 chat 模型 [22] 的共同原因。

---

## 四、触发、防抖与 IME 的通用做法

**触发与防抖**

- 通行模型:键入 → 防抖窗口(几十到几百 ms)→ 发请求 → 流式接收 → 逐块渐进显示 → 新键入即取消在途请求 [1][19][33]。
- 参考值:Zed 各 provider 默认 75ms(Copilot)/150ms(Codestral)/0ms(本地)[19];Continue 源码默认 350ms [18];两者都提供设置项覆盖,且都提供「手动触发」命令绕过自动触发 [1][19]。
- 显示节流与请求节流分开:VSCode 的 `editor.inlineSuggest.minShowDelay` 只控制显示节奏 [1];Zed 说明 `prediction_debounce` 之外另有 "normal request throttling" [19]。
- 降级开关:计量网络自动停用自动触发 [1];Snooze(定时暂停)[1][14];按语言/目录/文件类型禁用(VSCode `github.copilot.enable` [1]、Zed `show_edit_predictions`/`disabled_globs` [19]、Cursor 按扩展名 [14])。

**与 IME(中文输入法)的交互**

- VSCode 已实现「组合输入(composition)期间禁用 inline completions」:输入法候选浮层打开时,中间态字母会被提交后替换,此时显示 ghost text 无意义且干扰,故 #233208/#235707 两个 issue 提出并在后续版本关闭(编辑器在 composition 期间不请求/不显示 inline suggestion)[9][10]。Zakki 若支持中文输入,应在合成态暂停请求并隐藏建议,合成结束后再触发。
- 反向影响也成立:inline completion 长期占据编辑器注意力,会挤压普通建议(推荐词)弹窗。VSCode 2026-05 曾把 `editor.quickSuggestions.other` 默认改为 `offWhenInlineCompletions`(有 inline completion 时不再弹普通建议),一周后因「几乎永久抑制了 snippet/IntelliSense 弹窗」的回归而回滚为 `'on'` [11][12]——说明「AI 补全与本地补全并存时的优先级/互斥策略」需要谨慎设计并给用户开关。
- 接受键位冲突:合成态下 `Tab` 归输入法;建议被接受后紧接合成输入的场景需测试(Zed 专门文档化了 `tab` 与 LSP completion 菜单的冲突处理 [19],同理适用于 IME)。

---

## 五、输入时切换模型的 UI 形态

- **VSCode Copilot Chat**:模型选择器内联在聊天输入框(model picker 显示当前模型名;显示 "Models" 时提示需登录/信任工作区),入口还包括:picker 内齿轮 → **Manage Language Models**(模态管理编辑器:可见性、置顶、按 provider/能力过滤)、推理模型名旁 `>` 展开参数子菜单(thinking effort / context size)、**Auto** 自动档(悬停响应可查看实际用的模型)[2][7]。补全模型不在输入框内切——走 Chat 菜单 → Configure Inline Suggestions… → Change Completions Model,或命令面板 `Change Completions Model` [1][4]。
- **Cursor**:模型选择器同样内联在 Agent/Chat 输入框,BYO key 的模型会出现在同一 picker;Tab 补全无模型选择器(状态栏 Tab 图标只做开关/Snooze)[13][14]。
- **Zed**:聊天侧在 Agent 面板有模型下拉;**补全侧把 provider 切换器放在状态栏**——点编辑预测图标弹出 provider 菜单(可在已配 key 的 Mercury/Codestral 等之间直接切换,Configure Providers 入口也在菜单里),另有 `edit_predictions.provider` 配置与 eager/subtle 显示模式切换 [19]。这是最接近「输入时切换补全模型」的既有形态。
- **JetBrains**:聊天模型选择器在 Chat 面板;补全模型(provider/model/Prompt schema)只在 Settings 的 AI Completion 区配置,不在编辑器内联切换 [22]。
- **形态小结**:聊天模型普遍做成「输入框内下拉 + 管理入口」;补全模型/服务商切换有三种先例——(a) 命令面板/设置深页(Copilot、JetBrains),(b) 状态栏快切菜单(Zed),(c) 无(Cursor 锁定)。对 Zakki(便签应用、无命令面板惯性):设置页选默认模型 + 状态栏/标题栏快切菜单是低摩擦组合;若提供多个补全模型,「Auto(自动选可用)」可参考 Copilot Auto 的可解释性设计(悬停显示实际使用的模型)[7]。

---

## 参考来源

1. VSCode Docs — Inline suggestions(AI-powered suggestions):https://code.visualstudio.com/docs/copilot/ai-powered-suggestions (页面日期 2026-11-04)
2. VSCode Docs — AI language models in VS Code(模型 picker、BYOK、on-device):https://code.visualstudio.com/docs/agent-customization/language-models
3. VSCode Blog — Expanding Model Choice in VS Code with Bring Your Own Key(2025-10-22):https://code.visualstudio.com/blogs/2025/10/22/bring-your-own-key
4. GitHub Docs — Changing the AI model for GitHub Copilot inline suggestions:https://docs.github.com/en/copilot/how-tos/copilot-in-your-ide/copilot-for-common-tasks/change-the-completion-model
5. GitHub Changelog — Copilot code completion now uses the GPT-4.1 Copilot model(2025-08-27):https://github.blog/changelog/2025-08-27-copilot-code-completion-now-uses-the-gpt-4-1-copilot-model/
6. GitHub Changelog — GPT-4.1 Copilot code completion model October update(2025-10-17):https://github.blog/changelog/2025-10-17-gpt-4-1-copilot-code-completion-model-october-update/
7. GitHub Changelog — Auto model selection GA in GitHub Copilot in VS Code(2025-12-10):https://github.blog/changelog/2025-12-10-auto-model-selection-is-generally-available-in-github-copilot-in-visual-studio-code/
8. GitHub Changelog — Auto model selection for Copilot in VS Code public preview(2025-09-15):https://github.blog/changelog/2025-09-15-auto-model-selection-for-copilot-in-vs-code-in-public-preview/
9. vscode Issue #233208 — Disable inline completions when in composition mode:https://github.com/microsoft/vscode/issues/233208
10. vscode Issue #235707 — Disable inline completions when composing via an IME:https://github.com/microsoft/vscode/issues/235707
11. vscode PR #316900 — Change the default for quickSuggestions to be offWhenInlineCompletions(2026-05-17):https://github.com/microsoft/vscode/pull/316900
12. vscode PR #318789 — Restore editor.quickSuggestions.other default to 'on'(2026-05-28):https://github.com/microsoft/vscode/pull/318789
13. Cursor Help — Bring your own API key:https://cursor.com/help/models-and-usage/api-keys.md
14. Cursor Help — Tab completion:https://cursor.com/help/ai-features/tab.md
15. Continue Docs — Autocomplete deep dive(配置项、推荐模型、FAQ):https://docs.continue.dev/customize/deep-dives/autocomplete
16. Continue Docs — Intro to Roles:https://docs.continue.dev/customize/model-roles/00-intro
17. Continue Docs — Autocomplete role(FIM 说明、prompt 模板):https://docs.continue.dev/customize/model-roles/autocomplete
18. continuedev/continue — core/util/parameters.ts(debounceDelay 默认 350 等):https://github.com/continuedev/continue/blob/main/core/util/parameters.ts
19. Zed Docs — Edit Prediction(provider、防抖默认值、prompt_format、键位):https://zed.dev/docs/ai/edit-prediction
20. Zed Docs — LLM Providers(模型接入路径总览):https://zed.dev/docs/ai/llm-providers
21. Zed Docs — Use a Local Model(llama.cpp/LM Studio/Ollama/本地 OpenAI 兼容):https://zed.dev/docs/ai/use-a-local-model
22. JetBrains AI Assistant Help — Use third-party and local models(2026.2,页面日期 2026-10-13):https://www.jetbrains.com/help/ai-assistant/use-custom-models.html
23. JetBrains Blog — Bring Your Own Key (BYOK) is now live in JetBrains IDEs(2025-12):https://blog.jetbrains.com/ai/2025/12/bring-your-own-key-byok-is-now-live-in-jetbrains-ides/
24. OpenAI API Reference — Create completion(Chat Completions,SSE):https://developers.openai.com/api/reference/resources/chat/methods/create
25. OpenAI API Docs — Completions API(legacy 格式、suffix 用法与关停日期):https://developers.openai.com/api/docs/guides/completions
26. OpenAI API Reference — Completions create(suffix/stop/stream 等参数):https://developers.openai.com/api/reference/resources/completions/methods/create
27. Mistral API Docs — FIM endpoint(POST /v1/fim/completions):https://docs.mistral.ai/api/endpoint/fim
28. Mistral Docs — Codestral 25.08(GA、128k、$0.3/$0.9 per M、fim/prefix 能力):https://docs.mistral.ai/models/codestral-25-08
29. mistralai/client-python — FIM SDK 文档(参数明细):https://github.com/mistralai/client-python/blob/main/docs/sdks/fim/README.md
30. DeepSeek API Docs — FIM Completion API (Beta)(base_url 必须为 /beta):https://api-docs.deepseek.com/api/create-completion
31. DeepSeek API Docs — Your First API Call(OpenAI/Anthropic 兼容 base_url、模型名):https://api-docs.deepseek.com/guides/fill_in_the_blank(跳转至 Quick Start 页)
32. Anthropic Docs — Messages API(SSE、prefill;无 FIM 端点):https://platform.claude.com/docs/en/api/messages
33. 社区示例 TabComplete — CLAUDE.md("Anthropic has no native fill-in-the-middle API" 及 AbortController 取消实践):https://github.com/androsland/TabComplete/blob/master/CLAUDE.md
34. notebook-intelligence Issue #129 — LiteLLM inline completions with chat-only models(FIM vs messages 回退方案):https://github.com/notebook-intelligence/notebook-intelligence/issues/129
35. Ollama Docs — API(/api/generate 的 prompt/suffix/raw/stream/keep_alive):https://github.com/ollama/ollama/blob/main/docs/api.md
36. Ollama Docs — OpenAI compatibility(/v1/completions 支持 suffix):https://github.com/ollama/ollama/blob/main/docs/api/openai-compatibility.mdx
37. Ollama Docs — Anthropic compatibility(/v1/messages):https://github.com/ollama/ollama/blob/main/docs/api/anthropic-compatibility.mdx
38. llama.cpp — server README(/completion、/infill、cache_prompt、--spm-infill):https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
39. LM Studio Docs — Completions (Legacy) OpenAI 兼容端点:https://lmstudio.ai/docs/developer/openai-compat/completions
40. VSCode Docs — IntelliSense(quickSuggestions 等编辑器建议设置默认值):https://code.visualstudio.com/docs/editor/intellisense

---

## 六、Zakki 本地可行性(MyGo 编辑器能力盘点)

以下为基于本仓库源码(vendor/github.com/egoist/mygo v0.5.1 + internal/app)的核对结果,证据均标注文件位置。**结论:整体可行,无阻塞性风险;唯一硬缺口是 MyGo 没有真正的「ghost text 装饰层」API,短期用应用层缓冲方案可验证。**

### 6.1 编辑器能力与缺口

| 能力 | 现状 | 证据 |
|---|---|---|
| 灰字渲染 | `ui.TextRange` 支持 `Color`/`Background`/`Weight`/`Font`/`Italic`/`Underline` 等,每帧按当前文本重算;IME 组合段强制无样式 | `ui/textinput.go:314-328` |
| Tab/Esc/→ 拦截 | `OnTextKey` 文档原文列出 "Tab that accepts a completion, Escape that cancels",即为此场景设计;未被消费的键回落原生流程 | `ui/textinput.go:100-120` |
| Esc 冲突 | Esc 已被窗口级 `c.Shortcut(0, ui.KeyEscape)` 登记,claimed-keys 补丁使其优先于 `OnTextKey`,AI 取消需挂到现有窗口 Esc 分支链首 | `view.go:38-46`、`patches/mygo-shortcut-claimed-keys.patch` |
| IME 组合态 | `Composing()` 布尔可查;组合期按键归输入法;Zakki 现有管线已用 `mdComposing` 屏蔽触发 | `ui/textinput.go:241-247`、`view_live.go:263-265, 320` |
| 行内对象(U+FFFC) | 定宽盒子参与布局,适合 chip/徽标,不适合整句流式灰字 | `ui/textobjects.go`、`live_timeview.go` |
| **ghost text 装饰层** | **不存在**。「不进缓冲、不参与撤销/IME/保存」的临时文字无现成 API,是唯一硬缺口 | 全文检索无此机制 |

短期 ghost 落点:把建议文字追加进活动块投影缓冲 `a.mdVisible` 并套灰 `TextRange{Color}`,接受前任何改动/切换都先剥离——「`@` 触发时剥占位符、时间 chip 不落盘」已是同类先例(`live_timepopover.go:417-437`)。应用层需自守两点:Changed 防重入、光标不得进入 ghost 区。长期建议向 MyGo 提议真正的 ghost text API。

### 6.2 可直接复用的管线

- **键入触发**:`tryTimeTrigger`(行尾键入 `@` 唤起时间弹层)就是「检测输入 → 弹层」的模板;防抖可抄 docStats/reminders 的 250ms `time.AfterFunc` + `Window.Update`(`view_status.go:55`、`reminders.go:154`)。
- **提交与撤销**:建议接受走既有文档事务(`commitDraft → applyDocEditAt`,`editKind` 已区分 input/IME);拒绝即剥离,不产生事务。
- **后台请求回主线程**:`Window.Update` + 可注入 deliver(`syncDeliver`,`sync_bridge.go:52`);HTTP 参照 fonts/sync 的 `http.NewRequestWithContext` + 可注入 client + 错误如实上抛(`internal/fonts/store.go:136-160`、`sync_oauth.go`)。取消用 context:新键入作废在途请求。
- **新纯逻辑包**(如 `internal/aiassist`):禁 import mygo 与 internal/app,需同步 engineering-standard.md §3 依赖表与 Makefile `ARCH_RULES`,过 `make arch-check`。

### 6.3 配置面(「模型可配置」的落点)

- **prefs**:照抄版本 14 `sync` 节先例——bump 15 + 新 `aiassist` struct(开关、provider 预设、baseURL、model、防抖 ms、apiKeyRef),缺键解码即零值,无需迁移(`internal/prefs/prefs.go:44, 120-127`)。
- **API key**:复用 Keyring envelope 机制,换 service/account,绝不写偏好文件(铁律 R7,`internal/notes/sync_keyring.go:23-43`);macOS 钥匙串/Windows DPAPI 实现现成。
- **设置页**:`settingsSectionTitle`/`settingsRow`/`boundedSelect`(限高下拉)全部现成(`settings_ui.go:20-36`、`settings_select.go`);文案走 locales 双文件 + `go tool mygo generate`。
- **输入时切换**(对应第五节 UI 先例):Zakki 无命令面板惯性,Zed 的「状态栏快切菜单」形态最贴合——底栏(状态栏)已有图标按钮体系(`chromeIcon`),加一个补全来源菜单即可;默认模型在设置页配置。

### 6.4 建议 MVP 与风险

1. 默认关闭;`aiassist` 偏好节 + 设置页分区(provider/baseURL/model + Keyring 存 key)。
2. Provider 抽象两个必选:OpenAI 兼容 `POST /v1/completions`(客户端按模型拼 FIM 模板,Zed 模式,覆盖 Ollama/llama.cpp/LM Studio/自托管)+ Codestral `POST /v1/fim/completions`;SSE 流式,`temperature 0.2`、`max_tokens 256`、FIM stop token,上下文裁剪 ~1k token(Continue 参数,见 3.3)。
3. 触发:`Changed() && !Composing()` → 250ms 防抖 → 后台请求;回主线程时按草稿代际校验,编辑已变则丢弃;IME 组合态不请求不显示(与 VSCode #233208 同款策略)。
4. 接受/拒绝:Tab 经 `OnTextKey` 写入真正事务;Esc 挂窗口分支链首;继续键入即作废。
5. 实时预览块编辑与源码模式两个编辑器都要接,先做活动块路径。

| 风险 | 缓解 |
|---|---|
| MyGo 无 ghost text 装饰层 | 短期缓冲方案验证;长期向 MyGo 提 API |
| 双模式(预览块/源码)重复接线 | 先活动块,源码模式复用同一 aiassist 服务 |
| 隐私/成本敏感(便签含私人内容) | 默认关闭 + 明确的「发送前缀」提示 + 按标签禁用(参考 `github.copilot.enable` 按语言粒度) |
| 补全打断中文输入 | 组合态禁触发(VSCode 已验证该策略) |
| 与搜索高亮/时间 chip 的 TextRange 抢占 | 区间判交沿用 `editorRanges` 位图先例(`view_highlight.go`) |

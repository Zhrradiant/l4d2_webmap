<div align="center">

<img src="./logo.png" alt="L4D2 WebMap" width="112" height="112" />

# L4D2 WebMap

**求生之路2（Left 4 Dead 2）浏览器换图插件**

[![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)](https://go.dev) [![SourceMod](https://img.shields.io/badge/SourceMod-1.11-FF6A00)](https://www.sourcemod.net) [![Frontend](https://img.shields.io/badge/Frontend-原生HTML%2FCSS%2FJS-000000)](https://developer.mozilla.org) [![Platform](https://img.shields.io/badge/Platform-Windows%20%2F%20Linux-0078D6?logo=windows&logoColor=white)](#环境要求) [![Version](https://img.shields.io/badge/version-0.1.4-333333)]()

</div>

玩家在游戏内输入 `!webmap`，聊天框回复**含 4 位验证码的 URL**；浏览器打开该 URL 即可自动登录，在网页上点选地图并**发起游戏内原生投票换图**（`L4D2NativeVote` ，多数通过触发换图）

## 功能特性

- **网页触发原生投票** — 浏览器选图 → 后端 RCON 下发 `sm_web_vote` → 游戏内弹板 Yes/No 投票，默认赞成多于反对即换图（可经 `webmap_vote_pass_mode` 切换为严格过半数）
- **验证码登录，无需注册** — 4 位去易混字符集（去除 `0/O/1/I/L`），TTL 300 秒、一次性、绑定 `server_key + player`
- **一个 URL 服务多台服务器** — 每台游戏服务器一个唯一 `server_key`，浏览器登录后自动落在对应服务器上
- **终局地图评分** — 终局向真人玩家弹出原生 1–5 星菜单，分数写入站点 `l4d2_map_ratings`（与网站 / zhrradiant-srvmap 同一套 REST 契约）
- **评论 / 标签 / 评分代理** — 后端同源转发到 `https://zhrradiant.com`，绕开浏览器 CORS
- **VPK 拖拽比对** — 把 VPK 拖进浏览器即可纯前端解析（v1/v2 格式，文件不上传），提取 `missions/*.txt` 的 Map 字段与全服地图统合比对，快速确认哪些服务器装载对应地图
- **SSE 实时推送** — 网页实时接收服务器状态、投票结果等事件
- **Mixmap 图池接入** — 网页图池编辑器通过 RCON 与 `l4d2_mixmap` 插件对接（见 `l4d2_mixmap/` 接入案例）
- **单二进制部署** — Go 后端内嵌前端静态资源（`go:embed`），部署无需附带 `web/` 目录
- **RCON 密码永不下发浏览器** — 只存后端 `data/rcon.json`，无任何 API 暴露

## 三端架构

| 端 | 实现 | 职责 |
|---|---|---|
| 游戏服务器 | SourceMod 1.11 插件 + REST in Pawn + l4d2_nativevote + l4d2_source_keyvalues | 取地图数据、生成验证码、回复 `!webmap`、HTTP 推送、发起原生投票 |
| 后端 | Go 单二进制（标准库，零第三方依赖） | 接收推送、JSON 存储、RCON 代理、会话鉴权、托管前端 |
| 浏览器 | 原生 HTML/CSS/JS（无构建步骤） | 验证码登录、地图浏览、触发游戏内投票 |

## 工作原理

```
 1. 服务器上报   插件 GetAllMissions 遍历 ──REST in Pawn POST──> 后端写 servers/<key>.json
 2. 验证码登录   玩家 !webmap ──推送码+回复URL──> 浏览器打开URL──> 自动登录颁发 token
 3. 网页投票     浏览器选图 ──POST /api/action──> 后端 RCON sm_web_vote ──> 原生投票弹板 ──> 多数通过换图
```

- **地图数据来自游戏内存** — 插件通过 gamedata 内存签名（`GetAllMissions`）遍历当前战役，翻译文件自动补写缺失条目，首次推送同步到后端
- **投票只在游戏内生效** — 网页不直接执行任何换图权限——后端只做 RCON 转发，最终裁决是游戏内原生的投票面板
- **JSON 文件存储** — 每台服务器一个文件（`data/servers/<server_key>.json`），读写加文件锁，无数据库依赖

## 技术栈

| 层级 | 技术 |
|---|---|
| 后端 | Go 1.22（标准库，零第三方依赖，单二进制） |
| 游戏插件 | SourceMod 1.11 · REST in Pawn · l4d2_nativevote · l4d2_source_keyvalues · left4dhooks |
| 前端 | 原生 HTML/CSS/JS（HarmonyOS Sans SC 字体子集化，`unicode-range` 分片加载） |
| 数据存储 | JSON 文件（每服务器一个，存于后端 `data/servers/`） |

## 环境要求

- **[Go](https://go.dev/dl/) 1.22+** —— 构建后端（Windows / Linux 均可，`build-linux.bat` 可交叉编译 Linux 版）
- **[SourceMod](https://www.sourcemod.net) 1.11 + spcomp-1.11** —— 编译游戏插件
- **游戏服务器依赖扩展**（缺一不可）：
  - [REST in Pawn](https://github.com/ErikMinekus/sm-ripext)（Linux 扩展 `rip.ext.so` 已随 `plugin/extensions/` 附带，Windows 版需自备）
  - [l4d2_nativevote](https://github.com/fdxx/l4d2_nativevote)（原生投票面板）
  - [l4d2_source_keyvalues](https://github.com/fdxx/l4d2_source_keyvalues)（遍历 missions）
  - [left4dhooks](https://forums.alliedmods.net/showthread.php?t=321696)（终局判定 `L4D_IsMissionFinalMap()`，评分功能需要）

## 快速开始

### 1. 构建并启动后端

```bash
cd backend
go build -o webmap.exe .
```

> 也可以双击 `build-windows.bat` 一键构建 Windows 版；双击 `build-linux.bat` 交叉编译出 Linux 版 `l4d2_webmap`

双击构建出的 `webmap.exe` 进入交互式控制台菜单——配置向导 / 启动服务 / 查看状态，无需记忆任何命令行参数：

```
════════════════════════════════════════════════
   L4D2 WebMap  ·  后端控制台
════════════════════════════════════════════════
请选择操作
  1) 启动服务       （前台运行，Ctrl+C 停止并返回菜单）
  2) 配置向导       （监听端口 / 推送密钥 / 在线地图CSV / RCON 密码）
  3) 编辑 RCON 密码
  4) 查看运行状态与已连接服务器
  5) 查看插件侧应填写的配置
  6) 选择性配置     （自选要修改的项，无需全部重配）
  0) 退出
```

首次使用先选 `2` 完成配置向导，再选 `1` 启动服务。启动后访问 `http://你的IP:11223/` 即可看到前端页面

### 2. 编译并安装游戏插件

```bash
cd plugin/scripting
spcomp-1.11 -iinclude l4d2_webmap.sp
```

安装到服务器 `left4dead2/addons/sourcemod/` 对应位置：

```
plugins/l4d2_webmap.smx
extensions/rip.ext.so
gamedata/l4d2_webmap.txt
translations/webmap.missions.phrases.txt
translations/webmap.chapters.phrases.txt
translations/chi/webmap.missions.phrases.txt
translations/chi/webmap.chapters.phrases.txt
```

### 3. 配置插件

编辑 `cfg/sourcemod/webmap.cfg`（参考 `config.example/webmap.cfg`）：

```
webmap_backend_url "http://你的后端IP:11223"
webmap_server_key "cn-01"          // 多服务必填唯一值；留空自动用「对外IP_端口」
webmap_push_secret "与后端config.json一致"
```

重启地图或 `sm_webmap_reload` 生效

> [!TIP]
> 修改 `config.json` 的 `web_dir` 指向磁盘上的前端目录后，可热改 `index.html` / `style.css` / `app.js` 而无需重新编译后端；想重新生成字体分片（`backend/web/fonts/` 的 woff2 + font.css）时，在 `tools/font-slice/` 下执行 `npm install && npm run slice`

### 4. 玩家使用

1. 游戏内输入 `!webmap`
2. 聊天框回复 `打开 http://你的后端/?code=AB3K （300秒内有效）`，在浏览器输入/粘贴该地址
3. 打开后自动登录进入地图面板（验证码失效时页面会显示输入框，供手动重试）
4. 点选地图卡片 → 「发起投票」→ 游戏内弹出原生投票面板
5. 同服玩家投票，多数通过触发换图

## 终局地图评分

- 终局地图向真人玩家弹出原生 1–5 星菜单（0 跳过），触发时机由 `webmap_rating_trigger` 三选一：
  - `0` 终局章节开始（`round_start`，默认）：建立本图评分会话，玩家进服后由事件驱动逐人补发
  - `1` 触发终局救援（`FinaleStart`）：延迟后全员扫描一次，菜单被占用可按 `menu_retry_*` 重试
  - `2` 确认救援成功（`finale_vehicle_leaving`）：撤离瞬间一次性弹出，不重试
- 也可在终局输入 `!rate` / `sm_rate` 手动打开
- 分数写入站点 `l4d2_map_ratings`（REST 契约对齐 zhrradiant-srvmap，站点固定为 `https://zhrradiant.com`）
- `map_id` = 当前战役 mission 名（需与站点地图列表 `identifier` 一致）；`rater_key` = `s` + SteamID64

## 配置与数据

- **示例配置** — `config.example/`（`config.json` / `rcon.json` 为占位符需替换，`webmap.cfg` 为可直接使用的默认配置，复制改名即可用）
- **运行时数据** — （后端 `data/`，不入库）：`config.json`、`rcon.json`、`servers/*.json`、`online_maps.json`、`permissions.json`、`mixmap_presets/`

> [!IMPORTANT]
> `data/rcon.json` 包含 RCON 密码，`data/config.json` 包含推送密钥——已在 `.gitignore` 屏蔽，**切勿暴露到仓库**

## API 一览

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/push` | X-Api-Key | 游戏插件上报地图数据 |
| POST | `/api/code` | X-Api-Key | 游戏插件上报验证码 |
| POST | `/api/vote_result` | X-Api-Key | 游戏插件上报投票结果 |
| GET | `/api/servers` | 无 | 公开服务器列表 |
| GET | `/api/search` | 无 | 统合搜索（跨服检索地图） |
| POST | `/api/login` | 无 | 验证码登录，返回 token |
| GET | `/api/config` | 无 | 前端背景配置 |
| GET | `/api/comments` | 无 | 评论列表代理（上游 v2） |
| POST | `/api/comments` | 无 | 提交评论代理（上游 v1） |
| POST | `/api/comments/like` | 无 | 点赞/取消点赞代理 |
| POST | `/api/comments/delete` | 无 | 删除评论代理 |
| GET | `/api/ratings` | 无 | 评分列表代理 |
| GET | `/api/tag-defs` | 无 | 标签定义代理 |
| GET | `/api/tags` | 无 | 地图标签代理 |
| GET | `/api/me` | Bearer token | 当前会话 |
| GET | `/api/server/:key/state` | 无（游客只读） | 该服地图列表（带 token 时校验跨服） |
| POST | `/api/action` | Bearer token | 发起游戏内投票 |
| GET | `/api/events` | Bearer token | SSE 实时状态推送 |
| POST | `/api/mixmap/start` | Bearer token | 下发图池并发起组图投票 |
| POST | `/api/mixmap/auto` | Bearer token | 按类型发起自动组图投票 |
| GET | `/api/mixmap/presets` | Bearer token | 预设列表（分页/搜索） |
| POST | `/api/mixmap/presets` | Bearer token | 保存预设 |
| DELETE | `/api/mixmap/presets/{name}` | Bearer token | 删除预设 |

## 安全

- **RCON 密码只存后端** `data/rcon.json`，无 API 暴露
- 推送接口需 `X-Api-Key`（= `push_secret`）防伪造
- 验证码 4 位去易混、TTL 300s、一次性、绑定 `server_key + player`
- 会话 token 绑定 `server_key`，跨服操作直接拒绝

> [!WARNING]
> `data/rcon.json` 中的 RCON 密码与 `config.json` 中的 `push_secret` 建议妥善保存,一旦泄露即应更换

## 项目结构

```
webmap/
├── README.md                ← 本文件
├── plugin/                  ← SourceMod 1.11 插件（镜像 addons/sourcemod/ 布局）
│   ├── scripting/           ← l4d2_webmap.sp 主源码 + include/（第三方头文件 + webmap/ 自研模块）
│   ├── extensions/          ← rip.ext.so（REST in Pawn Linux 扩展，已附带）
│   ├── gamedata/            ← l4d2_webmap.txt（内存签名）
│   └── translations/        ← missions / chapters phrases (en + chi)
├── backend/                 ← Go 后端（单二进制）
│   ├── main.go              ← 入口（交互菜单；init / serve 子命令）
│   ├── launcher.go          ← 交互式控制台菜单（配置 / 启动 / 状态一体）
│   ├── internal/            ← api / config / onlinemap / perm / rcon / rconcfg / session / store
│   ├── web/                 ← 前端静态资源（index.html / style.css / app.js / fonts），经 //go:embed 内嵌
│   ├── build-windows.bat   ← 一键构建 Windows 版 webmap.exe
│   └── build-linux.bat     ← 交叉编译 Linux 二进制脚本
├── config.example/          ← 示例配置（config.json / rcon.json / webmap.cfg / README_WEBMAP_CFG.md）
├── l4d2_mixmap/             ← 对其他插件的接入案例（blueblur0730 / kimika 的 WebMap RCON 对接）
├── tools/                   ← font-slice 字体切片工具（生成 web/fonts/ 的 woff2 分片与 font.css）
└── docs/                    ← 文档
```

## 接入案例：l4d2_mixmap

`l4d2_mixmap/` 汇集了 `l4d2_mixmap` 插件的多个版本，并为它们提供统一的 **WebMap RCON 对接**：网页端图池编辑器通过 RCON 下发命令，触发插件的组图投票。对接被重构为一个自包含模块 `webmap_rcon.sp`，可在不同版本间无痛移植。详见该目录下的 `README.md`

## 致谢

本项目的**游戏内地图数据获取与原生投票换图**底层方案，参考并复用自 SourceMod 插件 **[L4D2 Map vote](https://github.com/Hatsune-Imagine/l4d2-plugins/tree/main/l4d2_map_vote)**（作者：**fdxx、sorallll、HatsuneImagine**）——包括 gamedata 内存签名、SDKCall 准备、排除列表、翻译文件自动补写，以及原生投票通过后的切图逻辑。在此之上，WebMap 新增了 Go 后端、网页前端、REST in Pawn HTTP 上报、验证码登录，以及「网页触发 → 后端 RCON → 游戏内原生投票」的整套桥接架构

同时感谢以下第三方库 / 扩展：

- [l4d2_nativevote](https://github.com/fdxx/l4d2_nativevote) · [l4d2_source_keyvalues](https://github.com/fdxx/l4d2_source_keyvalues)（fdxx）
- [REST in Pawn](https://github.com/ErikMinekus/sm-ripext) · `colors.inc`
- [font-slice](https://github.com/voderl/font-slice)（字体切片）
- [樱花二次元图片API - Dmoe](https://www.dmoe.cc/) — 前端默认背景图链

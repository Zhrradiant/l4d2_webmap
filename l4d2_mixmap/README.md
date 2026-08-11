# l4d2_mixmap · WebMap RCON 对接

本目录汇集了 `l4d2_mixmap` 插件的多个版本，并为它们提供统一的 **WebMap RCON 对接**能力：网页端图池编辑器通过 RCON 下发命令，触发插件的组图投票。

## 目录结构

| 目录 | 说明 |
| --- | --- |
| `blueblur0730/` | 官方原版（blueblur0730），基线，不含改动。 |
| `blueblur0730-zhr/` | 官方版 + WebMap RCON 对接。 |
| `kimika/` | kimika fork 原版，基线，不含改动。 |
| `kimika-zhr/` | kimika 版 + WebMap RCON 对接。 |

`-zhr` 后缀的目录是加入了 WebMap 对接的可用版本；无后缀的是各自的上游基线，仅用于对照。

## 核心：从高绑定到可自由转换

最初的 WebMap 对接是**侵入式**的：直接改写 `vote.sp`、`actions.sp`、`util.sp`、`commands.sp` 等多个文件（改函数签名、抽公共函数、加返回码宏）。这使得对接代码与某一个具体版本高度绑定——一旦要移植到另一个 fork，就得逐处比对差异、手工适配，尤其是各版本日志体系（log4sp / SourceMod 内置 Logger）完全不同，适配成本很高。

现在整套对接被重构为**一个自包含模块** `scripting/l4d2_mixmap/webmap_rcon.sp`，实现了在各版本间**无痛自由转换**。做到这一点靠三条设计约束：

1. **全程只用 `PrintToServer` 输出单行状态码，绝不调用 `g_hLogger`。**
   这直接绕开了各版本最大的分歧点——日志体系，因此模块内部没有任何 `#if REQUIRE_LOG4SP` 之类的条件编译。
2. **不修改任何现有函数。**
   模块自带私有建池（`MixmapRcon_BuildPool`）与真人计数（`MixmapRcon_CountVoters`），只读写主文件已有的全局变量，只调用官方 `void` 版 `CreateMixmapVote` 发起投票（投票通过后由原版投票 handler 自行调用 `InitiateMixmap`，模块不介入）。原版一行都不用动。
3. **移植 = 三步胶水，且各版本字节一致。**
   见下。

这套设计能成立，是因为该插件是**单编译单元 + 共享全局状态**：主文件用 `#include` 把各 `.sp` 拼成一个整体编译，所有关键状态都是主文件的全局变量。模块因此可以直接读写这些全局、直接调用其它模块函数，无需谁给谁传参、无需改任何签名。模块所做的，本质是**复刻原版发起一次投票时对全局状态的那套操作**，而非调用原版函数代劳。

## 移植到一个新版本（三步）

以移植到某个 `xxx` 版本为例，三处改动在各版本里内容完全相同：

1. 复制模块文件到 `xxx/l4d2_mixmap/scripting/l4d2_mixmap/webmap_rcon.sp`。
2. 主文件 `l4d2_mixmap.sp` 的 `#include "l4d2_mixmap/vote.sp"` 之后加一行：
   ```sourcepawn
   #include "l4d2_mixmap/webmap_rcon.sp"
   ```
3. `setup.sp` 的 `SetupCommands()` 里注册两条命令：
   ```sourcepawn
   // WebMap 分层对接：仅 RCON/服务器可调
   RegServerCmd("sm_mixmap_load_pool", cmdMixmapLoadPool, "RCON: load external map pool and start mixmap vote");
   RegServerCmd("sm_mixmap_start_auto", cmdMixmapStartAuto, "RCON: start auto mixmap vote by type");
   ```

移植前请确认目标版本存在模块引用的符号（一般各版本均一致）：全局变量 `g_hArrayPools` / `g_hArraySurvivorSets` / `g_hArrayBlackList` / `g_bManullyChoosingMap` / `g_bMapsetInitialized` / `g_iMapsetType` / `g_sPresetName`；函数 `CheckBlackList`、`TheMatchExt.GetMapInfoByBspName`、`void CreateMixmapVote(int, MapSetType)`；ConVar `g_hCvar_Enable` / `g_hCvar_EnableBlackList`；枚举 `MapSet_None/Official/Custom/Mixtape/Preset`。

## 命令

两条命令均为 `RegServerCmd`，仅 RCON / 服务器控制台可调用，客户端无法触发。

```
sm_mixmap_load_pool "<发起人名>" "<map1 map2 ... mapN>" ["<预设名>"]
```
从外部下发的地图序列建池，并发起一次 preset 类型的组图投票。预设名可选，默认 `WebMap`。

```
sm_mixmap_start_auto "<发起人名>" "<type>"
```
按类型发起自动组图投票，`type` 取 `official` / `custom` / `mixtape`。

## 状态码

命令成功或失败时一律只 `PrintToServer` 一行状态码，避免污染 RCON reply：

| 状态码 | 含义 |
| --- | --- |
| `pool_vote_started` | 图池投票已发起 |
| `auto_vote_started` | 自动投票已发起 |
| `mixmap_disabled` | 插件被 `l4d2mm_enable` 关闭 |
| `mixmap_already_started` | 已有一局 mixmap 在进行中 |
| `vote_in_progress` | 已有投票进行中 |
| `manual_select_in_progress` | 正在手动选图 |
| `player_not_found` | 找不到指定发起人 |
| `pool_empty` | 参数缺失或所有地图名无效 |
| `no_voters` | 没有可参与投票的真人 |
| `bad_type` | `start_auto` 的类型参数无效 |

## 编译

改动仅涉及源码。修改后需用 spcomp 重新编译 `scripting/l4d2_mixmap.sp` 生成 `l4d2_mixmap.smx` 才会生效。

各版本插件本身的说明见其子目录内的 `README-cn.md` / `README.md`。

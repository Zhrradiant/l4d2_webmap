/**
 * L4D2 WebMap - 浏览器换图插件
 *
 * 致谢 / Credits:
 *   本插件的地图数据遍历(GetAllMissions)与原生投票换图底层方案,
 *   参考并复用自 SourceMod 插件《L4D2 Map vote》
 *   (作者: fdxx, sorallll, HatsuneImagine)。
 *   涉及: Init() 的 SDKCall 准备、SetFirstMapString()、g_smExclude 排除列表、
 *   IsMapValidEx()、翻译文件自动补写(OnPhrasesReady 思路),以及原生投票通过后的切图逻辑;
 *   gamedata/l4d2_webmap.txt 的内存签名亦与其一致。
 *
 * 终局评分:
 *   数据本体在 l4d2_server_status（表 l4d2_map_ratings）；
 *   REST 契约对齐 zhrradiant-srvmap（GET v2 / POST v1 /maps/ratings）。
 *
 * 模块拆分（include/webmap/）:
 *   globals.inc  - 常量、ConVar、全局状态
 *   maps.inc     - gamedata/SDKCall、地图遍历、翻译、推送 JSON
 *   backend.inc  - 后端 HTTP POST
 *   commands.inc - !webmap / sm_web_vote / sm_webmap_reload
 *   vote.inc     - 原生投票换图
 *   rating.inc   - 终局评分触发 / 菜单 / REST
 *   mixmap.inc   - Mixmap 在线探测（图池编辑器显隐）
 */

#pragma semicolon 1
#pragma newdecls required

#include <sourcemod>
#include <sdktools>
#include <ripext>
#include <l4d2_nativevote>
#include <l4d2_source_keyvalues>
#include <left4dhooks>
#include <colors>

#include "include/webmap/globals.inc"
#include "include/webmap/mixmap.inc"
#include "include/webmap/maps.inc"
#include "include/webmap/backend.inc"
#include "include/webmap/commands.inc"
#include "include/webmap/vote.inc"
#include "include/webmap/rating.inc"

public Plugin myinfo = {
    name        = PLUGIN_NAME,
    author      = "Zhrradiant - based on \"L4D2 Map vote\" (fdxx, sorallll, HatsuneImagine)",
    description = "浏览器换图插件 - 网页触发游戏内原生投票；终局地图评分",
    version     = PLUGIN_VERSION,
    url         = "https://github.com/Zhrradiant/l4d2_webmap"
};

// ===========================================================================
// 插件启动
// ===========================================================================

public void OnPluginStart() {
    Init();

    g_smExclude = new StringMap();
    g_smExclude.SetValue("credits", 1);
    g_smExclude.SetValue("HoldoutChallenge", 1);
    g_smExclude.SetValue("HoldoutTraining", 1);
    g_smExclude.SetValue("parishdash", 1);
    g_smExclude.SetValue("shootzones", 1);

    g_smFirstMap = new StringMap();
    g_smMissionEn = new StringMap();
    g_smMissionChi = new StringMap();
    g_smChapterEn = new StringMap();
    g_smChapterChi = new StringMap();
    g_smValidMaps   = new StringMap();
    g_smRatingDelivered = new StringMap();
    g_smRatingHandled   = new StringMap();

    CreateConVar("l4d2_webmap_version", PLUGIN_VERSION, "WebMap plugin version.", FCVAR_NOTIFY|FCVAR_DONTRECORD);

    g_cvBackendURL   = CreateConVar("webmap_backend_url",   "http://127.0.0.1:11223", "后端地址");
    g_cvServerKey    = CreateConVar("webmap_server_key",    "default",               "服务器唯一标识");
    g_cvServerName   = CreateConVar("webmap_server_name",   "",                      "服务器显示名(留空自动用游戏服务器名称)");
    g_cvPushSecret   = CreateConVar("webmap_push_secret",   "",                      "推送鉴权密钥(须与后端一致)");
    g_cvCodeTTL      = CreateConVar("webmap_code_ttl",      "300",                   "验证码有效期(秒)");
    g_cvCodeLength   = CreateConVar("webmap_code_length",   "4",                     "验证码位数");
    g_cvHost         = CreateConVar("webmap_host",          "",                      "对外连接IP(留空自动探测)");
    g_cvPort         = CreateConVar("webmap_port",          "0",                     "对外连接端口(0=用引擎端口)");

    g_cvVotePassMode = CreateConVar("webmap_vote_pass_mode", "0", "投票通过判定模式. 0=仅需赞成多于反对(yes>no, 未投票忽略, 宽松, 默认). 1=赞成必须严格超过反对+未投票(yes>no+弃权, 即赞成过半才通过, 未投视作反对, 严格).", FCVAR_NOTIFY);

    // 终局评分（API 契约对齐 zhrradiant-srvmap → l4d2_server_status REST；站点固定为 RATING_API_BASE）
    g_cvRatingEnabled    = CreateConVar("webmap_rating_enabled",     "1",  "终局章节是否弹出地图评分菜单. 1=开 0=关");
    g_cvRatingTrigger    = CreateConVar("webmap_rating_trigger",     "0",  "自动触发评分的时机. 0=终局章节开始(round_start) 1=触发终局救援(FinaleStart) 2=确认救援成功(finale_vehicle_leaving)", _, true, 0.0, true, 2.0);
    g_cvRatingDelay      = CreateConVar("webmap_rating_delay",       "3.0","触发后延迟多少秒进行判定/弹出评分菜单（路径 0/1/2 触发到判定的等待）");
    g_cvRatingFinaleRecheck = CreateConVar("webmap_rating_finale_recheck_count", "5", "路径0: delay 到点 finale 仍未就绪时的最多重排次数", _, true, 0.0);
    g_cvRatingFirstDelay = CreateConVar("webmap_rating_first_delay", "2.0","路径0: 玩家就绪后首次显示等待秒数（独立于 delay）");
    g_cvRatingFirstMax   = CreateConVar("webmap_rating_first_max",   "10", "路径0: 首次检查（不计数重试）的最大次数，防无限重排", _, true, 1.0);
    g_cvRatingMenuRetryInterval = CreateConVar("webmap_rating_menu_retry_interval", "3.0", "菜单占用时的计数重试间隔秒数");
    g_cvRatingMenuRetryCount = CreateConVar("webmap_rating_menu_retry_count", "20", "最大计数重试次数；路径 2 不重试", _, true, 0.0);
    g_cvRatingMenuTime   = CreateConVar("webmap_rating_menu_time",   "30", "评分菜单超时秒数");
    g_cvRatingOfficial   = CreateConVar("webmap_rating_official",    "0",  "官方图是否允许评分. 1=允许 0=仅三方");
    g_cvRatingShowAvg    = CreateConVar("webmap_rating_show_avg",    "1",  "菜单标题是否显示当前均分");
    g_cvRatingSpectators = CreateConVar("webmap_rating_spectators",  "0",  "旁观者是否弹出评分. 1=是 0=否");

    AutoExecConfig(true, "webmap");

    g_cvMPGameMode = FindConVar("mp_gamemode");
    g_cvMPGameMode.AddChangeHook(CvarChanged_Mode);

    RegConsoleCmd("sm_webmap", cmdWebMap);
    RegConsoleCmd("sm_rate", cmdRate);

    RegServerCmd("sm_web_vote", cmdWebVote);

    RegAdminCmd("sm_webmap_reload", cmdReload, ADMFLAG_ROOT);

    // 三个触发源的挂钩始终注册（不按 trigger 值动态增删）；各回调进入时自判路径值早退。
    HookEvent("round_start", Event_RoundStart_Rating, EventHookMode_PostNoCopy);
    HookEvent("finale_vehicle_leaving", Event_FinaleVehicleLeaving_Rating, EventHookMode_PostNoCopy);
    HookEntityOutput("trigger_finale", "FinaleStart", OnFinaleStart_Rating);
    // 路径 0 事件驱动逐人投递
    HookEvent("player_team",  Event_PlayerTeam_Rating);
    HookEvent("player_spawn", Event_PlayerSpawn_Rating);
}

public void OnConfigsExecuted() {
    GetCvars_Mode();

    static bool bFirstLoad = false;
    if (bFirstLoad)
        return;
    bFirstLoad = true;

    // 自动补写缺失的翻译条目（参照 L4D2 Map vote 的 OnPhrasesReady），再加载到 StringMap
    AutoFillPhrases();
    LoadPhraseMaps();

    // LoadTranslations 供游戏内 fmt_Translate 使用（按玩家客户端语言显示投票标题）
    LoadTranslations(TRANSLATION_MISSIONS);
    LoadTranslations(TRANSLATION_CHAPTERS);
    g_bTranslationsReady = true;

    SetFirstMapString();

    // 首次推送到后端
    PushMapData();
}

public void OnMapStart() {
    // 用地图名判定是否真正换图：同图内 OnMapStart 可能重复触发，只有地图名变化才复位评分状态，
    // 避免误清已在进行的路径0 会话（round_start 可能早于/晚于 OnMapStart）。
    char currentMap[64];
    GetCurrentMap(currentMap, sizeof currentMap);
    if (strcmp(currentMap, g_sRatingMapName) != 0) {
        strcopy(g_sRatingMapName, sizeof g_sRatingMapName, currentMap);
        g_sRatingMapId[0] = '\0';
        g_sRatingMapDisplay[0] = '\0';
        g_fRatingAvg = 0.0;
        g_iRatingCount = 0;
        // 路径0 会话状态复位
        g_bRatingSessionReady = false;
        g_bRatingSessionBuilding = false;
        g_bRatingScanScheduled = false;
        g_smRatingDelivered.Clear();
        g_smRatingHandled.Clear();
        // 冷却用 GetGameTime：跨图后可能回落；清零避免旧 last 算出异常 remain
        for (int i = 1; i <= MaxClients; i++) {
            g_fLastWebmapTime[i] = 0.0;
            ClearFirstCheckState(i);
            g_iMenuRetryCount[i] = 0;
        }
    }
    if (g_bTranslationsReady)
        PushMapData();
}

public void OnClientDisconnect(int client) {
    if (client < 1 || client > MaxClients)
        return;
    // 槽位复用：离开后清零，避免下一位玩家继承冷却；0 也表示“从未使用”
    g_fLastWebmapTime[client] = 0.0;
    // 清瞬时调度态，防止槽位复用把上一位玩家的 pending/计数带给新玩家
    ClearFirstCheckState(client);
    g_iMenuRetryCount[client] = 0;
}

#if defined _webmap_globals_included
    #endinput
#endif
#define _webmap_globals_included

#define PLUGIN_VERSION  "0.1.5"
#define PLUGIN_NAME     "L4D2 WebMap"
// 翻译文件基础名（不带 .txt：LoadTranslations 由 SM 自动补扩展名，文件路径拼装处另行补全）
#define TRANSLATION_MISSIONS "webmap.missions.phrases"
#define TRANSLATION_CHAPTERS "webmap.chapters.phrases"
// 评分 REST 站点（与 zhrradiant-srvmap defaultSiteURL 一致，无 ConVar）
#define RATING_API_BASE "https://zhrradiant.com"

// ========== gamedata / SDKCall ==========
Handle      g_hSDK_GetAllMissions;
Handle      g_hSDK_OnChangeMissionVote;
Address     g_pDirector;
Address     g_pMatchExtL4D;

// ========== ConVar ==========
ConVar g_cvBackendURL;
ConVar g_cvServerKey;
ConVar g_cvServerName;
ConVar g_cvPushSecret;
ConVar g_cvCodeTTL;
ConVar g_cvCodeLength;
ConVar g_cvHost;
ConVar g_cvPort;
ConVar g_cvMPGameMode;
ConVar g_cvVotePassMode;

// 终局评分
ConVar g_cvRatingEnabled;
ConVar g_cvRatingTrigger;            // 0=章节开始 1=触发救援 2=确认救援成功
ConVar g_cvRatingDelay;
ConVar g_cvRatingFinaleRecheck;      // 路径0: finale 未就绪时重排次数
ConVar g_cvRatingFirstDelay;         // 路径0: 玩家就绪后首次显示等待
ConVar g_cvRatingFirstMax;           // 路径0: 首次检查最大次数
ConVar g_cvRatingMenuRetryInterval;  // 菜单占用计数重试间隔
ConVar g_cvRatingMenuRetryCount;     // 最大计数重试次数
ConVar g_cvRatingMenuTime;
ConVar g_cvRatingOfficial;
ConVar g_cvRatingShowAvg;
ConVar g_cvRatingSpectators;

// ========== 全局 ==========
char    g_sMode[128];       // 当前游戏模式
bool    g_bTranslationsReady;
bool    g_bGamedataReady;
StringMap g_smExclude;     // 排除的 mission
StringMap g_smFirstMap;    // 第一张图: map -> mission
StringMap g_smMissionEn;   // mission 名: en
StringMap g_smMissionChi;  // mission 名: chi
StringMap g_smChapterEn;   // chapter map 名: en
StringMap g_smChapterChi;  // chapter map 名: chi
StringMap g_smValidMaps;   // 有效地图缓存: mapName -> 1
float  g_fLastWebmapTime[MAXPLAYERS+1]; // !webmap 每客户端冷却：上次成功时间；0=从未使用

// 终局评分状态
char   g_sRatingMapId[191];                        // 当前评分 map_id（站点 identifier / mission）
char   g_sRatingMapDisplay[128];                   // 菜单展示名
float  g_fRatingAvg;                               // 当前会话均分（路径0 逐人显示复用）
int    g_iRatingCount;                             // 当前会话评分人数
char   g_sClientRateMapId[MAXPLAYERS+1][191];      // 玩家菜单绑定的 map_id
float  g_fLastRateTime[MAXPLAYERS+1];              // 提交冷却

// 路径 0（round_start 会话 + 事件驱动逐人投递）
char      g_sRatingMapName[64];                    // 上次复位时的地图名，供 OnMapStart 判定是否真正换图（定时器/回调各自现取当前图名防串图）
bool      g_bRatingSessionReady;                   // 路径0: 本图评分会话是否已就绪
bool      g_bRatingSessionBuilding;                // 路径0: 会话是否正在建立（去重 round_start）
bool      g_bRatingScanScheduled;                  // 路径1/2: 全员扫描是否已安排（去重多次触发）
StringMap g_smRatingDelivered;                     // 已投递集合（键: SteamID64/退化）
StringMap g_smRatingHandled;                       // 本图已处理集合（用尽 first_max 放弃）
// 防抖调度态（按 client 槽位，断线即清）
float     g_fFirstCheckDeadline[MAXPLAYERS+1];     // 首次检查目标终点
bool      g_bFirstCheckPending[MAXPLAYERS+1];      // 是否已有 pending 首次检查 timer
int       g_iFirstCheckCount[MAXPLAYERS+1];        // 首次检查次数（不计入 retry）
int       g_iMenuRetryCount[MAXPLAYERS+1];         // 计数重试次数

// 原生投票状态
char g_sVoteMission[64];
char g_sVoteMap[128];
int  g_sVoteInitiator;

// 验证码字符集: 大写字母+数字，去除 0/O/1/I/L（共 29 个）
// 用普通全局数组(非 static)以便 commands.inc 的 GenerateCode() 跨文件下标索引;
// static 全局仅本文件可见, #define 字面量不支持下标, 故用可写全局数组。
char CODE_CHARS[] = "23456789ABCDEFGHJKMNPQRSTUVWXYZ";

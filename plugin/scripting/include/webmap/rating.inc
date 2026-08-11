#if defined _webmap_rating_included
    #endinput
#endif
#define _webmap_rating_included

// ===========================================================================
// 终局评分 — 触发 / 菜单 / REST（对齐 zhrradiant-srvmap 的 maps/ratings）
// ===========================================================================

// 清空路径0 的每客户端瞬时调度态（防抖终点 / pending / 首次计数）
void ClearFirstCheckState(int client) {
    g_fFirstCheckDeadline[client] = 0.0;
    g_bFirstCheckPending[client] = false;
    g_iFirstCheckCount[client] = 0;
}

// 异步回调/定时器防串图：比对发起时保存的地图名与当前地图名是否一致
bool IsSameRatingMap(const char[] mapName) {
    char currentMap[64];
    GetCurrentMap(currentMap, sizeof currentMap);
    return strcmp(currentMap, mapName) == 0;
}

// 路径 0：终局章节开始（round_start）。挂钩始终注册，进入即自判 trigger==0。
void Event_RoundStart_Rating(Event event, const char[] name, bool dontBroadcast) {
    if (!g_cvRatingEnabled.BoolValue)
        return;
    if (g_cvRatingTrigger.IntValue != 0)
        return;
    // 去重：本图会话已建立或正在建立则忽略后续 round_start
    if (g_bRatingSessionReady || g_bRatingSessionBuilding)
        return;

    // 现取真实地图名（不依赖 g_sRatingMapName，避免 round_start 早于 OnMapStart 时取到旧图名）
    char currentMap[64];
    GetCurrentMap(currentMap, sizeof currentMap);

    g_bRatingSessionBuilding = true;
    ScheduleFinaleDecision(currentMap, 0);
}

// 路径 1：触发终局救援（trigger_finale.FinaleStart）。
void OnFinaleStart_Rating(const char[] output, int caller, int activator, float delay) {
    if (!g_cvRatingEnabled.BoolValue)
        return;
    if (g_cvRatingTrigger.IntValue != 1)
        return;
    ScheduleFullScan(true); // allowMenuRetry = true
}

// 路径 2：确认救援成功（finale_vehicle_leaving）。
void Event_FinaleVehicleLeaving_Rating(Event event, const char[] name, bool dontBroadcast) {
    if (!g_cvRatingEnabled.BoolValue)
        return;
    if (g_cvRatingTrigger.IntValue != 2)
        return;
    ScheduleFullScan(false); // allowMenuRetry = false（不重试）
}

// 路径 1/2 共用：延迟后全员扫描一次。用 g_bRatingScanScheduled 去重多次触发。
void ScheduleFullScan(bool allowMenuRetry) {
    if (g_bRatingScanScheduled)
        return;
    g_bRatingScanScheduled = true;

    float wait = g_cvRatingDelay.FloatValue;
    if (wait < 0.0)
        wait = 0.0;

    char currentMap[64];
    GetCurrentMap(currentMap, sizeof currentMap);

    DataPack pk;
    CreateDataTimer(wait, Timer_FullScan, pk, TIMER_FLAG_NO_MAPCHANGE);
    pk.WriteString(currentMap);
    pk.WriteCell(allowMenuRetry ? 1 : 0);
}

Action Timer_FullScan(Handle timer, DataPack pk) {
    pk.Reset();
    char mapName[64];
    pk.ReadString(mapName, sizeof mapName);
    bool allowMenuRetry = pk.ReadCell() != 0;
    if (!IsSameRatingMap(mapName))
        return Plugin_Stop; // 已切图
    // FinaleStart / finale_vehicle_leaving 触发时本就处于终局中，此处仅作保险校验
    if (!L4D_IsMissionFinalMap())
        return Plugin_Stop;
    BeginRatingPrompt(0, allowMenuRetry); // 0 = 全员
    return Plugin_Stop;
}

// ===========================================================================
// 路径 0 —— round_start 会话 + 事件驱动逐人投递
// ===========================================================================

// 安排 finale 判定 timer；timer 保存地图名与已重排次数
void ScheduleFinaleDecision(const char[] mapName, int recheckCount) {
    float wait = g_cvRatingDelay.FloatValue;
    if (wait < 0.0)
        wait = 0.0;

    DataPack pk;
    CreateDataTimer(wait, Timer_FinaleDecision, pk, TIMER_FLAG_NO_MAPCHANGE);
    pk.WriteString(mapName);
    pk.WriteCell(recheckCount);
}

Action Timer_FinaleDecision(Handle timer, DataPack pk) {
    pk.Reset();
    char mapName[64];
    pk.ReadString(mapName, sizeof mapName);
    int recheckCount = pk.ReadCell();

    // 地图名校验：跨图 / 会话已建立则作废
    if (!IsSameRatingMap(mapName))
        return Plugin_Stop;
    if (g_bRatingSessionReady)
        return Plugin_Stop;

    if (!L4D_IsMissionFinalMap()) {
        // 区分「确实非终局」与「mission 尚未就绪」：未用尽重排次数则再等一次
        if (recheckCount < g_cvRatingFinaleRecheck.IntValue) {
            ScheduleFinaleDecision(mapName, recheckCount + 1);
        } else {
            // 用尽仍非终局：放弃本图会话建立
            g_bRatingSessionBuilding = false;
        }
        return Plugin_Stop;
    }

    // 确认终局：建立会话（解析 map_id + 拉取均分），只建立一次
    BuildRatingSession(mapName);
    return Plugin_Stop;
}

// 建立评分会话：解析 map_id / 均分。均分请求带地图名，回调校验防串图。
void BuildRatingSession(const char[] mapName) {
    char mapId[191], display[128];
    bool official;
    if (!ResolveCurrentMapId(mapId, sizeof mapId, display, sizeof display, official)) {
        LogMessage("[WebMap] 路径0 会话建立跳过：无法解析当前 map_id");
        g_bRatingSessionBuilding = false;
        return;
    }
    if (official && !g_cvRatingOfficial.BoolValue) {
        g_bRatingSessionBuilding = false;
        return;
    }

    strcopy(g_sRatingMapId, sizeof g_sRatingMapId, mapId);
    strcopy(g_sRatingMapDisplay, sizeof g_sRatingMapDisplay, display);

    if (g_cvRatingShowAvg.BoolValue) {
        // 异步拉取均分，回调里再标记会话就绪
        char url[512];
        FormatEx(url, sizeof url, "%s/wp-json/l4d2/v2/maps/ratings", RATING_API_BASE);
        HTTPRequest request = new HTTPRequest(url);
        request.AppendQueryParam("map_ids", "%s", mapId);
        request.Timeout = 12;

        DataPack pack = new DataPack();
        pack.WriteString(mapName);
        pack.WriteString(mapId);
        request.Get(OnSessionRatingsFetched, pack);
    } else {
        ActivateRatingSession(mapName, 0.0, 0);
    }
}

void OnSessionRatingsFetched(HTTPResponse response, any value, const char[] error) {
    DataPack pack = view_as<DataPack>(value);
    pack.Reset();
    char mapName[64];
    pack.ReadString(mapName, sizeof mapName);
    char mapId[191];
    pack.ReadString(mapId, sizeof mapId);
    delete pack;

    // 异步回调保护：地图名 + map_id 双校验，防上一图延迟响应激活当前会话
    if (!IsSameRatingMap(mapName))
        return;
    if (g_sRatingMapId[0] != '\0' && strcmp(g_sRatingMapId, mapId) != 0)
        return;

    float avg = 0.0;
    int count = 0;
    ParseRatingsResponse(response, error, mapId, avg, count);
    ActivateRatingSession(mapName, avg, count);
}

// 会话就绪：记录均分，逐人扫描已在服玩家安排首次检查
void ActivateRatingSession(const char[] mapName, float avg, int count) {
    if (!IsSameRatingMap(mapName))
        return;
    if (g_bRatingSessionReady)
        return;

    g_fRatingAvg = avg;
    g_iRatingCount = count;
    g_bRatingSessionReady = true;
    g_bRatingSessionBuilding = false;

    for (int i = 1; i <= MaxClients; i++) {
        if (IsClientInGame(i) && !IsFakeClient(i))
            ScheduleFirstCheck(i);
    }
}

// --- 玩家投递事件（仅会话就绪后安排 timer） ---

public void OnClientPostAdminCheck(int client) {
    if (g_cvRatingTrigger.IntValue != 0)
        return;
    if (!g_bRatingSessionReady)
        return;
    ScheduleFirstCheck(client);
}

void Event_PlayerTeam_Rating(Event event, const char[] name, bool dontBroadcast) {
    if (g_cvRatingTrigger.IntValue != 0 || !g_bRatingSessionReady)
        return;
    int client = GetClientOfUserId(event.GetInt("userid"));
    if (client)
        ScheduleFirstCheck(client);
}

void Event_PlayerSpawn_Rating(Event event, const char[] name, bool dontBroadcast) {
    if (g_cvRatingTrigger.IntValue != 0 || !g_bRatingSessionReady)
        return;
    int client = GetClientOfUserId(event.GetInt("userid"));
    if (client)
        ScheduleFirstCheck(client);
}

// 首次检查调度（防抖）：刷新目标终点；已有 pending 则不重复建 timer
void ScheduleFirstCheck(int client) {
    if (client < 1 || client > MaxClients)
        return;
    if (!IsClientInGame(client) || IsFakeClient(client))
        return;
    // 已投递 / 本图已放弃则不再安排
    if (IsDelivered(client) || IsHandled(client))
        return;

    float delay = g_cvRatingFirstDelay.FloatValue;
    if (delay < 0.0)
        delay = 0.0;
    g_fFirstCheckDeadline[client] = GetGameTime() + delay;

    if (g_bFirstCheckPending[client])
        return; // 已有 pending timer，只刷新终点

    char currentMap[64];
    GetCurrentMap(currentMap, sizeof currentMap);

    g_bFirstCheckPending[client] = true;
    DataPack pk;
    CreateDataTimer(delay, Timer_FirstCheck, pk, TIMER_FLAG_NO_MAPCHANGE);
    pk.WriteString(currentMap);
    pk.WriteCell(GetClientUserId(client));
}

// 首次检查到点（不消耗计数重试额度，但受 first_max 约束）
Action Timer_FirstCheck(Handle timer, DataPack pk) {
    pk.Reset();
    char mapName[64];
    pk.ReadString(mapName, sizeof mapName);
    int client = GetClientOfUserId(pk.ReadCell());

    if (!IsSameRatingMap(mapName))
        return Plugin_Stop;
    if (!client || !IsClientInGame(client)) {
        if (client >= 1 && client <= MaxClients)
            g_bFirstCheckPending[client] = false;
        return Plugin_Stop;
    }

    // 防抖：终点未到则等待剩余时间
    float now = GetGameTime();
    if (now < g_fFirstCheckDeadline[client] - 0.05) {
        float remain = g_fFirstCheckDeadline[client] - now;
        if (remain < 0.1)
            remain = 0.1;
        DataPack pk2;
        CreateDataTimer(remain, Timer_FirstCheck, pk2, TIMER_FLAG_NO_MAPCHANGE);
        pk2.WriteString(mapName);
        pk2.WriteCell(GetClientUserId(client));
        return Plugin_Stop; // pending 仍为 true
    }

    g_bFirstCheckPending[client] = false;

    // 已投递 / 本图已处理则结束
    if (IsDelivered(client) || IsHandled(client))
        return Plugin_Stop;

    // 首次检查次数上限：达到则本图放弃该玩家
    if (g_iFirstCheckCount[client] >= g_cvRatingFirstMax.IntValue) {
        MarkHandled(client);
        return Plugin_Stop;
    }

    if (!IsValidRater(client)) {
        // 非有效评分者：再安排一次首次检查（不计数重试、受 first_max 约束）
        g_iFirstCheckCount[client]++;
        ScheduleFirstCheck(client);
        return Plugin_Stop;
    }

    if (IsClientMenuBusy(client)) {
        // 有菜单占用：进入计数重试
        ScheduleMenuRetry(client, true);
        return Plugin_Stop;
    }

    // 显示并标记已投递
    ShowRatingMenu(client, g_sRatingMapId, g_sRatingMapDisplay, g_fRatingAvg, g_iRatingCount);
    MarkDelivered(client);
    return Plugin_Stop;
}

// --- 持久标记（按 SteamID64，不可用时退化 client 槽位） ---

// 生成本图持久键：优先 SteamID64，退化为 "c<slot>"
void BuildPersistKey(int client, char[] out, int maxlen) {
    char steam64[32];
    if (GetClientAuthId(client, AuthId_SteamID64, steam64, sizeof steam64, true) && steam64[0] != '\0')
        FormatEx(out, maxlen, "s%s", steam64);
    else
        FormatEx(out, maxlen, "c%d", client);
}

void MarkDelivered(int client) {
    char key[40];
    BuildPersistKey(client, key, sizeof key);
    g_smRatingDelivered.SetValue(key, 1);
}

bool IsDelivered(int client) {
    char key[40];
    BuildPersistKey(client, key, sizeof key);
    return g_smRatingDelivered.ContainsKey(key);
}

void MarkHandled(int client) {
    char key[40];
    BuildPersistKey(client, key, sizeof key);
    g_smRatingHandled.SetValue(key, 1);
}

bool IsHandled(int client) {
    char key[40];
    BuildPersistKey(client, key, sizeof key);
    return g_smRatingHandled.ContainsKey(key);
}

// !rate / sm_rate：终局内手动打开评分（不依赖 FinaleStart）
public Action cmdRate(int client, int args) {
    if (!client || !IsClientInGame(client) || IsFakeClient(client))
        return Plugin_Handled;

    if (!g_cvRatingEnabled.BoolValue) {
        CPrintToChat(client, "{default}评分功能已关闭");
        return Plugin_Handled;
    }
    if (!L4D_IsMissionFinalMap()) {
        CPrintToChat(client, "{default}当前不是终局地图，无法评分");
        return Plugin_Handled;
    }
    if (!IsValidRater(client)) {
        CPrintToChat(client, "{default}旁观者无法评分");
        return Plugin_Handled;
    }

    // 手动命令不受「已投递 / 本图已处理」标记约束，直接弹（不进计数重试）
    BeginRatingPrompt(client, false);
    return Plugin_Handled;
}

// target=0 全员；否则仅该 client。allowMenuRetry：菜单被 SourceMod Normal 菜单占用时是否计数重试。
void BeginRatingPrompt(int target, bool allowMenuRetry) {
    char mapId[191], display[128];
    bool official;
    if (!ResolveCurrentMapId(mapId, sizeof mapId, display, sizeof display, official)) {
        if (target > 0)
            CPrintToChat(target, "{default}无法识别当前地图，无法评分");
        else
            LogMessage("[WebMap] 评分跳过：无法解析当前 map_id");
        return;
    }
    if (official && !g_cvRatingOfficial.BoolValue) {
        if (target > 0)
            CPrintToChat(target, "{default}官方图不开放评分");
        return;
    }

    strcopy(g_sRatingMapId, sizeof g_sRatingMapId, mapId);
    strcopy(g_sRatingMapDisplay, sizeof g_sRatingMapDisplay, display);

    if (g_cvRatingShowAvg.BoolValue)
        FetchRatingsThenShowMenus(mapId, target, allowMenuRetry);
    else
        ShowRatingMenus(target, 0.0, 0, allowMenuRetry);
}

bool IsValidRater(int client) {
    if (!client || !IsClientInGame(client) || IsFakeClient(client))
        return false;
    int team = GetClientTeam(client);
    if (team == 2 || team == 3)
        return true;
    if (team == 1 && g_cvRatingSpectators.BoolValue)
        return true;
    return false;
}

// 当前章节 → mission 名作为 map_id（与站点 CSV identifier 对齐）
bool ResolveCurrentMapId(char[] mapId, int maxlen, char[] display, int displayLen, bool &official) {
    mapId[0] = '\0';
    display[0] = '\0';
    official = false;

    if (!g_bGamedataReady)
        return false;

    char currentMap[128];
    GetCurrentMap(currentMap, sizeof currentMap);

    char mission[64], mapName[128];
    SourceKeyValues kvMissions = SDKCall(g_hSDK_GetAllMissions, g_pMatchExtL4D);
    for (SourceKeyValues kvSub = kvMissions.GetFirstTrueSubKey(); !kvSub.IsNull(); kvSub = kvSub.GetNextTrueSubKey()) {
        kvSub.GetName(mission, sizeof mission);
        if (g_smExclude.ContainsKey(mission))
            continue;

        SourceKeyValues kvModes = kvSub.FindKey("modes");
        if (kvModes.IsNull())
            continue;

        SourceKeyValues kvModeChapters = kvModes.FindKey(g_sMode);
        if (kvModeChapters.IsNull())
            continue;

        for (SourceKeyValues kvChapter = kvModeChapters.GetFirstTrueSubKey(); !kvChapter.IsNull(); kvChapter = kvChapter.GetNextTrueSubKey()) {
            kvChapter.GetString("Map", mapName, sizeof mapName, "N/A");
            if (!strcmp(mapName, "N/A"))
                continue;
            if (strcmp(mapName, currentMap, false) != 0)
                continue;

            strcopy(mapId, maxlen, mission);
            official = (kvSub.GetInt("builtin") != 0);
            GetTranslation(mission, display, displayLen, "chi", mission);
            return mapId[0] != '\0';
        }
    }

    return false;
}

void BuildRaterKey(int client, char[] out, int maxlen) {
    char steam64[32];
    if (GetClientAuthId(client, AuthId_SteamID64, steam64, sizeof steam64, true) && steam64[0] != '\0') {
        FormatEx(out, maxlen, "s%s", steam64);
        SanitizeRaterKey(out, maxlen);
        return;
    }
    // 无 Steam：退化键（尽量避免；同图同人可能不稳）
    FormatEx(out, maxlen, "g%d_%d", GetClientUserId(client), GetTime());
    SanitizeRaterKey(out, maxlen);
}

// 对齐站点 sanitize：小写 + 仅 a-z0-9._- + 最长 191
void SanitizeRaterKey(char[] key, int maxlen) {
    int write = 0;
    for (int i = 0; key[i] != '\0' && write < maxlen - 1 && write < 191; i++) {
        int c = key[i];
        if (c >= 'A' && c <= 'Z')
            c += 32;
        if ((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-')
            key[write++] = c;
    }
    key[write] = '\0';
}

// PHP json_encode 会把 5.0 编成整数 5；ripext GetFloat 只认 JSON real，整型会读成 0。
// 因此：先 GetFloat，为 0 时再回退 GetInt / 字符串。
float JsonObject_GetNumber(JSONObject obj, const char[] key) {
    if (obj == null || !obj.HasKey(key) || obj.IsNull(key))
        return 0.0;

    float f = obj.GetFloat(key);
    if (f != 0.0)
        return f;

    int i = obj.GetInt(key);
    if (i != 0)
        return float(i);

    char buf[32];
    if (obj.GetString(key, buf, sizeof buf) && buf[0] != '\0')
        return StringToFloat(buf);

    return 0.0;
}

// ── HTTP：GET 均分（v2 公开） / POST 评分（v1） ──

void FetchRatingsThenShowMenus(const char[] mapId, int target, bool allowMenuRetry) {
    char url[512];
    FormatEx(url, sizeof url, "%s/wp-json/l4d2/v2/maps/ratings", RATING_API_BASE);

    HTTPRequest request = new HTTPRequest(url);
    request.AppendQueryParam("map_ids", "%s", mapId);
    request.Timeout = 12;

    DataPack pack = new DataPack();
    pack.WriteCell(target);
    pack.WriteCell(allowMenuRetry ? 1 : 0);
    pack.WriteString(mapId);
    request.Get(OnRatingsFetched, pack);
}

void OnRatingsFetched(HTTPResponse response, any value, const char[] error) {
    DataPack pack = view_as<DataPack>(value);
    pack.Reset();
    int target = pack.ReadCell();
    bool allowMenuRetry = pack.ReadCell() != 0;
    char mapId[191];
    pack.ReadString(mapId, sizeof mapId);
    delete pack;

    float avg = 0.0;
    int count = 0;
    ParseRatingsResponse(response, error, mapId, avg, count);

    // map 可能已切换；仅当仍是本次 map_id 时弹窗
    if (g_sRatingMapId[0] != '\0' && strcmp(g_sRatingMapId, mapId) != 0)
        return;

    g_fRatingAvg = avg;
    g_iRatingCount = count;
    ShowRatingMenus(target, avg, count, allowMenuRetry);
}

// 解析 v2 均分响应到 avg/count（供路径 1/2 与路径 0 会话建立共用）
void ParseRatingsResponse(HTTPResponse response, const char[] error, const char[] mapId, float &avg, int &count) {
    avg = 0.0;
    count = 0;
    if (response.Status == HTTPStatus_Invalid) {
        LogError("[WebMap] 拉取评分失败（网络: %s）", error);
        return;
    }
    if (response.Status != HTTPStatus_OK || response.Data == null)
        return;

    JSONObject root = view_as<JSONObject>(response.Data);
    if (root.HasKey("success") && root.GetBool("success") && root.HasKey("data") && !root.IsNull("data")) {
        JSONObject data = view_as<JSONObject>(root.Get("data"));
        if (data != null && data.HasKey("ratings") && !data.IsNull("ratings")) {
            JSONObject ratings = view_as<JSONObject>(data.Get("ratings"));
            if (ratings != null && ratings.HasKey(mapId) && !ratings.IsNull(mapId)) {
                JSONObject info = view_as<JSONObject>(ratings.Get(mapId));
                if (info != null) {
                    avg = JsonObject_GetNumber(info, "avg");
                    if (info.HasKey("count"))
                        count = info.GetInt("count");
                    delete info;
                }
            }
            delete ratings;
        }
        delete data;
    }
}

// 路径 1/2 全员扫描 / cmdRate 单人。markDelivered=false（不写路径0 会话集合）。
void ShowRatingMenus(int target, float avg, int count, bool allowMenuRetry) {
    if (target > 0) {
        if (IsValidRater(target))
            TryDeliverRatingMenu(target, avg, count, allowMenuRetry, false);
        return;
    }
    for (int i = 1; i <= MaxClients; i++) {
        if (IsValidRater(i))
            TryDeliverRatingMenu(i, avg, count, allowMenuRetry, false);
    }
}

// 尝试给 client 投递评分菜单：菜单被 SourceMod Normal 面板占用时，按 allowMenuRetry 决定重试或丢弃。
// markDelivered=true 时（路径0），成功显示后写入已投递集合。
void TryDeliverRatingMenu(int client, float avg, int count, bool allowMenuRetry, bool markDelivered) {
    if (!IsValidRater(client))
        return;

    if (IsClientMenuBusy(client)) {
        if (allowMenuRetry)
            ScheduleMenuRetry(client, markDelivered);
        // 不重试则直接丢弃（路径 2 / 已达上限）
        return;
    }

    ShowRatingMenu(client, g_sRatingMapId, g_sRatingMapDisplay, avg, count);
    if (markDelivered)
        MarkDelivered(client);
}

// 仅能检出 SourceMod Normal 菜单，检不出 External（原生投票 / 其他扩展面板）
bool IsClientMenuBusy(int client) {
    return GetClientMenu(client) == MenuSource_Normal;
}

// 计数重试：定时器保存 UserId，受 menu_retry_count 上限约束
void ScheduleMenuRetry(int client, bool markDelivered) {
    if (g_iMenuRetryCount[client] >= g_cvRatingMenuRetryCount.IntValue)
        return; // 达上限，放弃

    float interval = g_cvRatingMenuRetryInterval.FloatValue;
    if (interval < 0.5)
        interval = 0.5;

    char currentMap[64];
    GetCurrentMap(currentMap, sizeof currentMap);

    DataPack pk;
    CreateDataTimer(interval, Timer_MenuRetry, pk, TIMER_FLAG_NO_MAPCHANGE);
    pk.WriteString(currentMap);
    pk.WriteCell(GetClientUserId(client));
    pk.WriteCell(markDelivered ? 1 : 0);
}

Action Timer_MenuRetry(Handle timer, DataPack pk) {
    pk.Reset();
    char mapName[64];
    pk.ReadString(mapName, sizeof mapName);
    int client = GetClientOfUserId(pk.ReadCell());
    bool markDelivered = pk.ReadCell() != 0;
    if (!IsSameRatingMap(mapName))
        return Plugin_Stop;
    if (!client || !IsValidRater(client))
        return Plugin_Stop;
    // 路径0：已投递则结束
    if (markDelivered && IsDelivered(client))
        return Plugin_Stop;

    if (IsClientMenuBusy(client)) {
        g_iMenuRetryCount[client]++;
        if (g_iMenuRetryCount[client] < g_cvRatingMenuRetryCount.IntValue)
            ScheduleMenuRetry(client, markDelivered);
        return Plugin_Stop;
    }

    ShowRatingMenu(client, g_sRatingMapId, g_sRatingMapDisplay, g_fRatingAvg, g_iRatingCount);
    if (markDelivered)
        MarkDelivered(client);
    return Plugin_Stop;
}

void ShowRatingMenu(int client, const char[] mapId, const char[] display, float avg, int count) {
    if (!mapId[0])
        return;

    strcopy(g_sClientRateMapId[client], sizeof g_sClientRateMapId[], mapId);

    Menu menu = new Menu(RatingMenuHandler);
    if (g_cvRatingShowAvg.BoolValue && count > 0)
        menu.SetTitle("地图评分\n%s\n均分 %.1f 共 %d 人", display, avg, count);
    else
        menu.SetTitle("地图评分\n%s", display);

    menu.AddItem("1", "★☆☆☆☆  1分");
    menu.AddItem("2", "★★☆☆☆  2分");
    menu.AddItem("3", "★★★☆☆  3分");
    menu.AddItem("4", "★★★★☆  4分");
    menu.AddItem("5", "★★★★★  5分");
    menu.ExitButton = true;

    int timeout = g_cvRatingMenuTime.IntValue;
    if (timeout < 5)
        timeout = 5;
    if (timeout > 120)
        timeout = 120;
    menu.Display(client, timeout);
}

int RatingMenuHandler(Menu menu, MenuAction action, int client, int param2) {
    switch (action) {
        case MenuAction_Select: {
            if (!client || !IsClientInGame(client))
                return 0;

            char info[8];
            menu.GetItem(param2, info, sizeof info);
            int rating = StringToInt(info);
            if (rating < 1 || rating > 5)
                return 0;

            char mapId[191];
            strcopy(mapId, sizeof mapId, g_sClientRateMapId[client]);
            if (!mapId[0])
                strcopy(mapId, sizeof mapId, g_sRatingMapId);
            if (!mapId[0]) {
                CPrintToChat(client, "{default}评分失败：地图未知");
                return 0;
            }

            float now = GetGameTime();
            if (now - g_fLastRateTime[client] < 2.0) {
                CPrintToChat(client, "{default}操作太快，请稍后再试");
                return 0;
            }
            g_fLastRateTime[client] = now;

            PostMapRating(client, mapId, rating);
        }
        case MenuAction_End: {
            delete menu;
        }
    }
    return 0;
}

void PostMapRating(int client, const char[] mapId, int rating) {
    char raterKey[64];
    BuildRaterKey(client, raterKey, sizeof raterKey);
    if (!raterKey[0]) {
        CPrintToChat(client, "{default}无法获取玩家标识，评分失败");
        return;
    }

    char url[512];
    FormatEx(url, sizeof url, "%s/wp-json/l4d2/v1/maps/ratings", RATING_API_BASE);

    JSONObject body = new JSONObject();
    body.SetString("map_id", mapId);
    body.SetInt("rating", rating);
    body.SetString("rater_key", raterKey);

    HTTPRequest request = new HTTPRequest(url);
    request.Timeout = 12;

    DataPack pack = new DataPack();
    pack.WriteCell(GetClientUserId(client));
    pack.WriteCell(rating);
    request.Post(body, OnRatingPosted, pack);
    delete body;

    CPrintToChat(client, "{default}正在提交评分 {olive}%d {default}星…", rating);
}

void OnRatingPosted(HTTPResponse response, any value, const char[] error) {
    DataPack pack = view_as<DataPack>(value);
    pack.Reset();
    int userid = pack.ReadCell();
    int submitted = pack.ReadCell();
    delete pack;

    int client = GetClientOfUserId(userid);
    if (!client || !IsClientInGame(client))
        return;

    if (response.Status == HTTPStatus_Invalid) {
        CPrintToChat(client, "{default}评分提交失败（网络错误）");
        LogError("[WebMap] 评分 POST 失败: %s", error);
        return;
    }

    if (response.Status != HTTPStatus_OK || response.Data == null) {
        CPrintToChat(client, "{default}评分提交失败（HTTP %d）", view_as<int>(response.Status));
        return;
    }

    JSONObject root = view_as<JSONObject>(response.Data);
    bool ok = root.HasKey("success") && root.GetBool("success");
    if (!ok) {
        char msg[128];
        msg[0] = '\0';
        if (root.HasKey("data") && !root.IsNull("data"))
            root.GetString("data", msg, sizeof msg);
        if (msg[0])
            CPrintToChat(client, "{default}评分失败：{olive}%s", msg);
        else
            CPrintToChat(client, "{default}评分失败");
        return;
    }

    float avg = 0.0;
    int count = 0;
    int my = submitted;
    if (root.HasKey("data") && !root.IsNull("data")) {
        JSONObject data = view_as<JSONObject>(root.Get("data"));
        if (data != null) {
            avg = JsonObject_GetNumber(data, "avg");
            if (data.HasKey("count"))
                count = data.GetInt("count");
            if (data.HasKey("my_rating")) {
                int respMy = data.GetInt("my_rating");
                if (respMy > 0)
                    my = respMy;
            }
            delete data;
        }
    }

    if (count > 0)
        CPrintToChat(client, "{default}已评 {green}%d {default}星，均分 {olive}%.1f {default}(%d 人)", my, avg, count);
    else
        CPrintToChat(client, "{default}已评 {green}%d {default}星", my);
}

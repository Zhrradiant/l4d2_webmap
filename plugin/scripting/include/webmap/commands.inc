#if defined _webmap_commands_included
    #endinput
#endif
#define _webmap_commands_included

// ===========================================================================
// !webmap 命令 - 生成验证码并推送
// ===========================================================================

public Action cmdWebMap(int client, int args) {
    if (!client || !IsClientInGame(client) || IsFakeClient(client))
        return Plugin_Handled;

    // 每客户端冷却（默认 30 秒）。
    // g_fLastWebmapTime==0 表示从未成功触发：不可用「当前时间-0」判断，否则开局 GetGameTime()<30 会误拦首次使用。
    // curTime < last：GetGameTime 回落（或未清的旧 last），视为无效冷却起点，直接放行。
    float curTime = GetGameTime();
    float last = g_fLastWebmapTime[client];
    if (last > 0.0 && curTime >= last) {
        float remain = 30.0 - (curTime - last);
        if (remain > 0.0) {
            if (remain > 30.0)
                remain = 30.0;
            // 冷却提示也必须延迟：同步 CPrintToChat 会排在玩家 "!webmap" 原消息之前
            DataPack coolPack = new DataPack();
            coolPack.WriteCell(GetClientUserId(client));
            coolPack.WriteFloat(remain);
            CreateTimer(0.1, Timer_PrintWebmapCooldown, coolPack, TIMER_FLAG_NO_MAPCHANGE);
            return Plugin_Handled;
        }
    }
    g_fLastWebmapTime[client] = curTime;

    int codeLen = g_cvCodeLength.IntValue;
    if (codeLen < 1) codeLen = 4;
    if (codeLen > 8) codeLen = 8;

    char code[16];
    GenerateCode(code, codeLen);

    int ttl = g_cvCodeTTL.IntValue;
    if (ttl < 60) ttl = 60;

    int expire = GetTime() + ttl;

    // 获取玩家信息
    char playerName[MAX_NAME_LENGTH], steamID[64];
    GetClientName(client, playerName, sizeof playerName);
    if (!GetClientAuthId(client, AuthId_Steam2, steamID, sizeof steamID))
        steamID = "";

    // 推送到后端（附管理员标记，供后端权限层 preset_access_mode=1 判定）
    PushCodeToBackend(code, playerName, steamID, expire, IsGameAdmin(client));

    // 延迟回复：chat trigger 会在命令回调之后才把玩家原消息刷进聊天框，
    // 若这里同步 CPrintToChat，插件回复会排在 "!webmap" 之前。
    // 0.1s 通常够让玩家原消息先显示，同时体感几乎无感。
    char url[256];
    g_cvBackendURL.GetString(url, sizeof url);

    DataPack pack = new DataPack();
    pack.WriteCell(GetClientUserId(client));
    pack.WriteString(url);
    pack.WriteString(code);
    pack.WriteCell(ttl);
    CreateTimer(0.1, Timer_PrintWebmapReply, pack, TIMER_FLAG_NO_MAPCHANGE);

    return Plugin_Handled;
}

public Action Timer_PrintWebmapCooldown(Handle timer, DataPack pack) {
    pack.Reset();
    int client = GetClientOfUserId(pack.ReadCell());
    float remain = pack.ReadFloat();
    delete pack;

    if (!client || !IsClientInGame(client))
        return Plugin_Stop;

    if (remain < 1.0)
        remain = 1.0;

    CPrintToChat(client, "{default}请等待 {olive}%.0f {default}秒后再试", remain);
    return Plugin_Stop;
}

public Action Timer_PrintWebmapReply(Handle timer, DataPack pack) {
    pack.Reset();
    int client = GetClientOfUserId(pack.ReadCell());

    char url[256], code[16];
    pack.ReadString(url, sizeof url);
    pack.ReadString(code, sizeof code);
    int ttl = pack.ReadCell();
    delete pack;

    if (!client || !IsClientInGame(client))
        return Plugin_Stop;

    CPrintToChat(client, "{default}打开 {blue}%s?code=%s {default}({olive}%d秒{default}内有效)", url, code, ttl);
    CPrintToChat(client, "{default}点击链接打开网页，即可{green}发起游戏内投票换图{default}。");
    return Plugin_Stop;
}

void GenerateCode(char[] buffer, int len) {
    int charCount = strlen(CODE_CHARS);
    for (int i = 0; i < len; i++) {
        buffer[i] = CODE_CHARS[GetRandomInt(0, charCount - 1)];
    }
    buffer[len] = '\0';
}

// 判断玩家是否为游戏服务器管理员（SourceMod admin 体系；后端权限层 preset_access_mode=1 使用）。
// root / generic 任一 flag 命中即视为管理员；仅查 flags，不依赖具体命令 override。
bool IsGameAdmin(int client) {
    int flags = GetUserFlagBits(client);
    return (flags & (ADMFLAG_ROOT | ADMFLAG_GENERIC)) != 0;
}

void PushCodeToBackend(const char[] code, const char[] player, const char[] steamID, int expire, bool admin) {
    char serverKey[64];
    ResolveServerKey(serverKey, sizeof serverKey);

    JSONObject data = new JSONObject();
    data.SetString("server_key", serverKey);
    data.SetString("code", code);
    data.SetString("player", player);
    data.SetString("steamid", steamID);
    data.SetInt("expire", expire);
    data.SetBool("admin", admin);

    PostToBackend("/api/code", data);
    delete data;
}

// ===========================================================================
// sm_web_vote 命令 - 后端 RCON 调用，发起原生投票
// ===========================================================================

public Action cmdWebVote(int args) {
    if (args != 3) {
        PrintToServer("Usage: sm_web_vote <player_name> <mission> <map>");
        return Plugin_Handled;
    }

    char player[MAX_NAME_LENGTH], mission[64], mapName[128];
    GetCmdArg(1, player, sizeof player);
    GetCmdArg(2, mission, sizeof mission);
    GetCmdArg(3, mapName, sizeof mapName);

    // 按 player_name 找在线 client 作为 Initiator
    int initiator = FindClientByName(player);
    if (initiator == -1 || !IsClientInGame(initiator) || IsFakeClient(initiator)) {
        PrintToServer("player_not_found");
        return Plugin_Handled;
    }

    // 校验地图
    if (!IsMapValidEx(mapName)) {
        PrintToServer("map_invalid");
        return Plugin_Handled;
    }

    // 校验是否允许新投票
    if (!L4D2NativeVote_IsAllowNewVote()) {
        PrintToServer("vote_in_progress");
        return Plugin_Handled;
    }

    // 发起原生投票（参考 L4D2 Map vote VoteChangeMap）
    StartNativeVote(initiator, mission, mapName);
    PrintToServer("vote_started");
    return Plugin_Handled;
}

int FindClientByName(const char[] name) {
    char clientName[MAX_NAME_LENGTH];
    for (int i = 1; i <= MaxClients; i++) {
        if (!IsClientInGame(i) || IsFakeClient(i))
            continue;
        GetClientName(i, clientName, sizeof clientName);
        if (strcmp(clientName, name, false) == 0)
            return i;
    }
    return -1;
}

// ===========================================================================
// 管理员重载命令
// ===========================================================================

public Action cmdReload(int client, int args) {
    AutoFillPhrases();
    LoadPhraseMaps();
    LoadTranslations(TRANSLATION_MISSIONS);
    LoadTranslations(TRANSLATION_CHAPTERS);
    g_bTranslationsReady = true;
    SetFirstMapString();
    PushMapData();
    ReplyToCommand(client, "[WebMap] 地图数据已重新推送到后端");
    return Plugin_Handled;
}

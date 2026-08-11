#if defined _webmap_maps_included
    #endinput
#endif
#define _webmap_maps_included

// ===========================================================================
// 地图遍历 / 翻译加载与自动补写（复用 L4D2 Map vote 的 GetAllMissions 方案）
// ===========================================================================

// 自动补写用的短语条目（key=mission/地图名, val=游戏内显示名）
enum struct esPhrase {
    char key[64];
    char val[64];
}

void Init() {
    GameData hGameData = new GameData("l4d2_webmap");
    if (!hGameData)
        SetFailState("Failed to load \"l4d2_webmap.txt\" gamedata file");

    g_pDirector = hGameData.GetAddress("CDirector");
    if (!g_pDirector)
        SetFailState("Failed to find address: \"CDirector\"");

    g_pMatchExtL4D = hGameData.GetAddress("g_pMatchExtL4D");
    if (!g_pMatchExtL4D)
        SetFailState("Failed to find address: \"g_pMatchExtL4D\"");

    StartPrepSDKCall(SDKCall_Raw);
    PrepSDKCall_SetVirtual(0);
    PrepSDKCall_SetReturnInfo(SDKType_PlainOldData, SDKPass_Plain);
    if (!(g_hSDK_GetAllMissions = EndPrepSDKCall()))
        SetFailState("Failed to create SDKCall: \"MatchExtL4D::GetAllMissions\"");

    StartPrepSDKCall(SDKCall_Raw);
    PrepSDKCall_SetFromConf(hGameData, SDKConf_Signature, "CDirector::OnChangeMissionVote");
    PrepSDKCall_AddParameter(SDKType_String, SDKPass_Pointer);
    if (!(g_hSDK_OnChangeMissionVote = EndPrepSDKCall()))
        SetFailState("Failed to create SDKCall: \"CDirector::OnChangeMissionVote\"");

    delete hGameData;
    g_bGamedataReady = true;
}

void CvarChanged_Mode(ConVar convar, const char[] oldValue, const char[] newValue) {
    GetCvars_Mode();
}

void GetCvars_Mode() {
    g_cvMPGameMode.GetString(g_sMode, sizeof g_sMode);
}

// --- 第一张图映射（复用 L4D2 Map vote）---

void SetFirstMapString() {
    g_smFirstMap.Clear();
    g_smValidMaps.Clear();
    char key[64], mission[64], map[64];
    SourceKeyValues kvMissions = SDKCall(g_hSDK_GetAllMissions, g_pMatchExtL4D);
    for (SourceKeyValues kvSub = kvMissions.GetFirstTrueSubKey(); !kvSub.IsNull(); kvSub = kvSub.GetNextTrueSubKey()) {
        FormatEx(key, sizeof key, "modes/%s/1/Map", g_sMode);
        SourceKeyValues kvFirstMap = kvSub.FindKey(key);
        if (kvFirstMap.IsNull())
            continue;

        kvSub.GetName(mission, sizeof mission);
        kvFirstMap.GetString(NULL_STRING, map, sizeof map);
        g_smFirstMap.SetString(map, mission);
        g_smValidMaps.SetValue(map, 1);
    }
    // 缓存所有有效地图，供 BuildMapDataJSON 绕过 FindMap
    CacheValidMaps(kvMissions);
}

void CacheValidMaps(SourceKeyValues kvMissions) {
    char mission[64], mapName[128];
    for (SourceKeyValues kvSub = kvMissions.GetFirstTrueSubKey(); !kvSub.IsNull(); kvSub = kvSub.GetNextTrueSubKey()) {
        kvSub.GetName(mission, sizeof mission);
        if (g_smExclude.ContainsKey(mission))
            continue;
        SourceKeyValues kvModes = kvSub.FindKey("modes");
        if (kvModes.IsNull())
            continue;
        char modeKey[128];
        FormatEx(modeKey, sizeof modeKey, "%s", g_sMode);
        SourceKeyValues kvModeChapters = kvModes.FindKey(modeKey);
        if (kvModeChapters.IsNull())
            continue;
        for (SourceKeyValues kvChapter = kvModeChapters.GetFirstTrueSubKey(); !kvChapter.IsNull(); kvChapter = kvChapter.GetNextTrueSubKey()) {
            kvChapter.GetString("Map", mapName, sizeof mapName, "N/A");
            if (!strcmp(mapName, "N/A"))
                continue;
            if (IsMapValidEx(mapName))
                g_smValidMaps.SetValue(mapName, 1);
        }
    }
}

// --- 构建地图 JSON 并推送 ---

void PushMapData() {
    if (!g_bGamedataReady)
        return;

    GetCvars_Mode();

    // 用 REST in Pawn 构建 JSON 对象树，转义与序列化交给扩展，不再有缓冲长度上限之忧。
    JSONObject data = BuildMapDataJSON();
    PostToBackend("/api/push", data);
    delete data;
}

// ResolveHostPort 解析对外 host/port：ConVar 留空/为 0 时自动探测 hostip/hostport。
void ResolveHostPort(char[] host, int maxlen, int &port) {
    g_cvHost.GetString(host, maxlen);
    port = g_cvPort.IntValue;
    if (host[0] == '\0') {
        // 用服务器 IP（可能不精确，服主应配置）
        int ip = GetConVarInt(FindConVar("hostip"));
        FormatEx(host, maxlen, "%d.%d.%d.%d",
            (ip >> 24) & 0xFF, (ip >> 16) & 0xFF, (ip >> 8) & 0xFF, ip & 0xFF);
    }
    if (port <= 0) {
        port = GetConVarInt(FindConVar("hostport"));
    }
}

// ResolveServerKey 解析服务器唯一标识：ConVar 有值（且非默认 "default"）时用之，
// 否则回退为自动探测的 "IP_端口"（如 1.2.3.4_27015），保证多服天然唯一。
void ResolveServerKey(char[] out, int maxlen) {
    g_cvServerKey.GetString(out, maxlen);
    if (out[0] != '\0' && strcmp(out, "default") != 0)
        return;
    char host[64];
    int port;
    ResolveHostPort(host, sizeof host, port);
    FormatEx(out, maxlen, "%s_%d", host, port);
}

// ResolveServerName 解析服务器显示名：ConVar 有值时用之，
// 留空时回退游戏自带 hostname（仍为空则由前端回退 server_key）。
void ResolveServerName(char[] out, int maxlen) {
    g_cvServerName.GetString(out, maxlen);
    TrimString(out);
    if (out[0] != '\0')
        return;
    ConVar cvHostname = FindConVar("hostname");
    if (cvHostname != null)
        cvHostname.GetString(out, maxlen);
}

JSONObject BuildMapDataJSON() {
    char serverKey[64], serverName[128], currentMap[128], host[64];
    int port;
    ResolveServerKey(serverKey, sizeof serverKey);
    ResolveServerName(serverName, sizeof serverName);
    GetCurrentMap(currentMap, sizeof currentMap);
    ResolveHostPort(host, sizeof host, port);

    // 头部字段：字符串转义由 REST in Pawn 负责，serverName 含中文/引号/反斜杠均安全。
    JSONObject root = new JSONObject();
    root.SetString("server_key", serverKey);
    root.SetString("host", host);
    root.SetInt("port", port);
    root.SetString("server_name", serverName);
    root.SetString("gamemode", g_sMode);
    root.SetString("current_map", currentMap);
    // 可选增强：探测同服 Mixmap，供前端决定是否显示图池编辑器
    root.SetBool("mixmap_available", ProbeMixmapAvailable());

    JSONArray maps = new JSONArray();

    char mission[64], mapName[128];
    char dispEn[128], dispChi[128], chapEn[128], chapChi[128];

    SourceKeyValues kvMissions = SDKCall(g_hSDK_GetAllMissions, g_pMatchExtL4D);
    for (SourceKeyValues kvSub = kvMissions.GetFirstTrueSubKey(); !kvSub.IsNull(); kvSub = kvSub.GetNextTrueSubKey()) {
        kvSub.GetName(mission, sizeof mission);
        if (g_smExclude.ContainsKey(mission))
            continue;

        int isOfficial = kvSub.GetInt("builtin");

        SourceKeyValues kvModes = kvSub.FindKey("modes");
        if (kvModes.IsNull())
            continue;

        // 取当前模式下的章节
        char modeKey[128];
        FormatEx(modeKey, sizeof modeKey, "%s", g_sMode);
        SourceKeyValues kvModeChapters = kvModes.FindKey(modeKey);
        if (kvModeChapters.IsNull())
            continue;

        // 翻译 mission 名
        GetTranslation(mission, dispEn, sizeof dispEn, "en", mission);
        GetTranslation(mission, dispChi, sizeof dispChi, "chi", mission);

        for (SourceKeyValues kvChapter = kvModeChapters.GetFirstTrueSubKey(); !kvChapter.IsNull(); kvChapter = kvChapter.GetNextTrueSubKey()) {
            kvChapter.GetString("Map", mapName, sizeof mapName, "N/A");
            if (!strcmp(mapName, "N/A"))
                continue;
            if (!g_smValidMaps.ContainsKey(mapName))
                continue;

            // 翻译章节名
            GetTranslation(mapName, chapEn, sizeof chapEn, "en", mapName);
            GetTranslation(mapName, chapChi, sizeof chapChi, "chi", mapName);

            char firstMission[64];
            bool isFirst = g_smFirstMap.GetString(mapName, firstMission, sizeof firstMission);

            JSONObject mapObj = new JSONObject();
            mapObj.SetString("mission", mission);
            mapObj.SetString("mission_display_en", dispEn);
            mapObj.SetString("mission_display_chi", dispChi);
            mapObj.SetString("chapter_map", mapName);
            mapObj.SetString("chapter_en", chapEn);
            mapObj.SetString("chapter_chi", chapChi);
            mapObj.SetBool("official", isOfficial != 0);
            mapObj.SetBool("is_first", isFirst);

            // 复制语义：Push 会复制值，随后立即释放本地句柄。
            maps.Push(mapObj);
            delete mapObj;
        }
    }

    root.Set("maps", maps);
    delete maps;

    return root;
}

// ===========================================================================
// 自动补写翻译文件（参照 L4D2 Map vote 的 OnPhrasesReady）
// ===========================================================================

// AutoFillPhrases 扫描服务器所有 mission/chapter，把 translations 文件中
// 缺失的条目自动补进去（en 补进根目录文件、chi 补进语言子目录文件），
// 免去新地图后手动维护翻译文件。
// 官方地图因无 Localizer 不做自动翻译，两个语言文件均先用游戏内显示名，可手动改。
void AutoFillPhrases() {
    ArrayList alMissions = new ArrayList(sizeof(esPhrase));
    ArrayList alChapters = new ArrayList(sizeof(esPhrase));

    esPhrase esp;
    char phrase[64];
    char translation[64];
    SourceKeyValues kvModes;
    SourceKeyValues kvChapters;

    SourceKeyValues kvMissions = SDKCall(g_hSDK_GetAllMissions, g_pMatchExtL4D);
    for (kvMissions = kvMissions.GetFirstTrueSubKey(); !kvMissions.IsNull(); kvMissions = kvMissions.GetNextTrueSubKey()) {
        kvMissions.GetName(phrase, sizeof phrase);
        if (g_smExclude.ContainsKey(phrase))
            continue;

        kvModes = kvMissions.FindKey("modes");
        if (kvModes.IsNull())
            continue;

        if (alMissions.FindString(phrase) == -1) {
            kvMissions.GetString("DisplayTitle", translation, sizeof translation, "N/A");
            strcopy(esp.key, sizeof esp.key, phrase);
            strcopy(esp.val, sizeof esp.val, !strcmp(translation, "N/A") ? phrase : translation);
            alMissions.PushArray(esp);
        }

        // 遍历所有模式的所有章节（不限当前模式），保证新图全部补上
        for (kvModes = kvModes.GetFirstTrueSubKey(); !kvModes.IsNull(); kvModes = kvModes.GetNextTrueSubKey()) {
            for (kvChapters = kvModes.GetFirstTrueSubKey(); !kvChapters.IsNull(); kvChapters = kvChapters.GetNextTrueSubKey()) {
                kvChapters.GetString("Map", phrase, sizeof phrase, "N/A");
                if (!strcmp(phrase, "N/A") || FindCharInString(phrase, '/') != -1)
                    continue;

                if (alChapters.FindString(phrase) == -1) {
                    kvChapters.GetString("DisplayName", translation, sizeof translation, "N/A");
                    strcopy(esp.key, sizeof esp.key, phrase);
                    strcopy(esp.val, sizeof esp.val, !strcmp(translation, "N/A") ? phrase : translation);
                    alChapters.PushArray(esp);
                }
            }
        }
    }

    // 与 L4D2 Map vote 一致：按语言分文件补写（en 在根目录，chi 在语言子目录），读取时合并
    AutoFillOnePhraseFile(TRANSLATION_MISSIONS, "en", false, alMissions);
    AutoFillOnePhraseFile(TRANSLATION_MISSIONS, "chi", true, alMissions);
    AutoFillOnePhraseFile(TRANSLATION_CHAPTERS, "en", false, alChapters);
    AutoFillOnePhraseFile(TRANSLATION_CHAPTERS, "chi", true, alChapters);

    delete alMissions;
    delete alChapters;
}

// AutoFillOnePhraseFile 对某语言的翻译文件补写缺失条目：
// 文件不存在则创建并写入全部条目；存在则仅补缺失 key，避免覆盖已有手工翻译。
// langDir=true 时文件位于 translations/chi/（语言子目录），条目只写 langKey 语言键。
void AutoFillOnePhraseFile(const char[] filename, const char[] langKey, bool langDir, ArrayList alPhrases) {
    char path[PLATFORM_MAX_PATH];
    if (langDir)
        BuildPath(Path_SM, path, sizeof path, "translations/chi/%s.txt", filename);
    else
        BuildPath(Path_SM, path, sizeof path, "translations/%s.txt", filename);

    KeyValues kv = new KeyValues("Phrases");
    esPhrase esp;
    bool bChanged = false;

    if (!FileExists(path)) {
        // 文件不存在：先创建空文件，再导出全部条目
        File file = OpenFile(path, "w");
        if (!file && langDir) {
            // 语言子目录可能未随插件发布：创建目录后重试一次
            char dir[PLATFORM_MAX_PATH];
            BuildPath(Path_SM, dir, sizeof dir, "translations/chi");
            if (CreateDirectory(dir, -1))
                file = OpenFile(path, "w");
        }
        if (!file) {
            LogError("[WebMap] 无法创建翻译文件: %s", path);
            delete kv;
            return;
        }
        file.WriteLine("");
        delete file;

        for (int x = 0; x < alPhrases.Length; x++) {
            alPhrases.GetArray(x, esp);
            if (kv.JumpToKey(esp.key, true)) {
                kv.SetString(langKey, esp.val);
                kv.Rewind();
            }
        }
        kv.ExportToFile(path);
    } else if (kv.ImportFromFile(path)) {
        // 文件已存在：仅补缺失条目，已有翻译不动
        for (int x = 0; x < alPhrases.Length; x++) {
            alPhrases.GetArray(x, esp);
            if (!kv.JumpToKey(esp.key)) {
                kv.Rewind();
                kv.JumpToKey(esp.key, true);
                kv.SetString(langKey, esp.val);
                bChanged = true;
            }
            kv.Rewind();
        }
        if (bChanged)
            kv.ExportToFile(path);
    } else {
        LogError("[WebMap] 翻译文件解析失败，跳过自动补写: %s", path);
    }

    delete kv;
}

// LoadPhraseMaps 把翻译文件按语言合并进 StringMap：
// 布局与 L4D2 Map vote 一致（en 在根目录、chi 在语言子目录），
// 合并后 GetTranslation/推送 JSON 天然得到 en/chi 双键。
void LoadPhraseMaps() {
    LoadOnePhraseFile(TRANSLATION_MISSIONS, g_smMissionEn, g_smMissionChi);
    LoadOnePhraseFile(TRANSLATION_CHAPTERS, g_smChapterEn, g_smChapterChi);
}

// LoadOnePhraseFile 读取某翻译文件的两份语言数据：
// 先读根目录文件（en + chi，chi 缺失回落 en），再读 chi/ 语言子目录文件（人工中文，覆盖 chi）。
void LoadOnePhraseFile(const char[] filename, StringMap smEn, StringMap smChi) {
    char path[PLATFORM_MAX_PATH];
    BuildPath(Path_SM, path, sizeof path, "translations/%s.txt", filename);
    if (!FileExists(path)) {
        LogError("[WebMap] 翻译文件加载失败: %s", path);
        return;
    }
    LoadOnePhraseFileInto(path, smEn, smChi, true);

    // 语言子目录文件可能不存在（只有 en 时），静默跳过，chi 回落 en
    BuildPath(Path_SM, path, sizeof path, "translations/chi/%s.txt", filename);
    if (FileExists(path))
        LoadOnePhraseFileInto(path, smEn, smChi, false);
}

// LoadOnePhraseFileInto 读单个翻译文件进 StringMap；
// storeEn=true 时写 en（根目录文件），false 时仅覆盖 chi（语言子目录文件优先）。
void LoadOnePhraseFileInto(const char[] path, StringMap smEn, StringMap smChi, bool storeEn) {
    KeyValues kv = new KeyValues("Phrases");
    if (!kv.ImportFromFile(path)) {
        delete kv;
        return;
    }

    if (!kv.GotoFirstSubKey()) {
        delete kv;
        return;
    }

    char key[64], valEn[128], valChi[128];
    do {
        kv.GetSectionName(key, sizeof key);
        kv.GetString("en", valEn, sizeof valEn, key);
        kv.GetString("chi", valChi, sizeof valChi, valEn);  // chi 缺失时回落 en
        if (storeEn)
            smEn.SetString(key, valEn);
        smChi.SetString(key, valChi);
    } while (kv.GotoNextKey());

    kv.Rewind();
    delete kv;
}

// GetTranslation 按 lang ("en"/"chi") 从对应 StringMap 取翻译文本。
// smEn/smChi 在 LoadPhraseMaps 时填充。
void GetTranslation(const char[] phrase, char[] buffer, int maxlength, const char[] lang, const char[] defvalue) {
    StringMap sm = null;
    bool isMission = g_smMissionEn.GetString(phrase, buffer, maxlength);
    if (isMission) {
        sm = (strcmp(lang, "chi") == 0) ? g_smMissionChi : g_smMissionEn;
    } else {
        sm = (strcmp(lang, "chi") == 0) ? g_smChapterChi : g_smChapterEn;
    }
    if (!sm.GetString(phrase, buffer, maxlength))
        strcopy(buffer, maxlength, defvalue);
}

bool IsMapValidEx(const char[] map) {
    if (!map[0])
        return false;
    char foundmap[1];
    return FindMap(map, foundmap, sizeof foundmap) == FindMap_Found;
}

void fmt_Translate(const char[] phrase, char[] buffer, int maxlength, const char[] defvalue) {
    if (!TranslationPhraseExists(phrase))
        strcopy(buffer, maxlength, defvalue);
    else
        Format(buffer, maxlength, "%T", phrase, LANG_SERVER);
}

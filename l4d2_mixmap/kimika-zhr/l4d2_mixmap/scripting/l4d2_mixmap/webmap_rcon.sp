#if defined _l4d2_mixmap_webmap_rcon_included
	#endinput
#endif
#define _l4d2_mixmap_webmap_rcon_included

// ===========================================================================
// WebMap RCON 对接模块（自包含、可跨版移植）
// ---------------------------------------------------------------------------
// 设计约束（务必保持，以便在 blueblur0730 / kimika 等版本间自由转换）：
//   1. 全程只用 PrintToServer 输出单行状态码，绝不调用 g_hLogger —— 因此与各版本
//      的 log4sp 条件编译差异完全无关，无需适配。
//   2. 不修改任何现有函数：自带私有建池 (MixmapRcon_BuildPool) 与真人计数
//      (MixmapRcon_CountVoters)，只读写主文件已有的全局变量，只调用官方 void 版
//      CreateMixmapVote / InitiateMixmap。
//   3. 移植方式：复制本文件 + 在主 .sp 加一行 #include + 在 SetupCommands 注册两条
//      命令即可，三处胶水在各版本字节一致。
//
// 成功/失败一律只 PrintToServer 一行状态码，禁止其它 console 输出污染 RCON reply。
// ===========================================================================

// 按玩家名（不区分大小写）查找在游戏中的真人客户端；未找到返回 -1。
static int MixmapRcon_FindClientByName(const char[] name)
{
	char clientName[MAX_NAME_LENGTH];
	for (int i = 1; i <= MaxClients; i++)
	{
		if (!IsClientInGame(i) || IsFakeClient(i))
			continue;
		GetClientName(i, clientName, sizeof(clientName));
		if (strcmp(clientName, name, false) == 0)
			return i;
	}
	return -1;
}

// 统计可参与投票的真人数（在游戏中、非 bot、非旁观队伍 1）。
// 官方 CreateMixmapVote 在无真人时会调用 DisplayVote(_, 0, 20)，本函数用于提前拦截。
static int MixmapRcon_CountVoters()
{
	int count = 0;
	for (int i = 1; i <= MaxClients; i++)
	{
		if (IsClientInGame(i) && !IsFakeClient(i) && GetClientTeam(i) != 1)
			count++;
	}
	return count;
}

// 公共前置检查；命中则已 PrintToServer 状态码并返回 true（调用方应直接 return）。
static bool MixmapRcon_RejectIfBusy()
{
	if (!g_hCvar_Enable.BoolValue)
	{
		PrintToServer("mixmap_disabled");
		return true;
	}
	if (g_bMapsetInitialized)
	{
		PrintToServer("mixmap_already_started");
		return true;
	}
	if (!L4D2NativeVote_IsAllowNewVote())
	{
		PrintToServer("vote_in_progress");
		return true;
	}
	if (g_bManullyChoosingMap)
	{
		PrintToServer("manual_select_in_progress");
		return true;
	}
	return false;
}

// 私有建池：从地图名列表原子构建 g_hArrayPools / g_hArraySurvivorSets（等长同序）。
// 仅做逐图黑名单 + GetMapInfoByBspName 校验，不做 preset 文件级 gamemode 段校验。
// 无效地图静默跳过（不写日志、不 PrintToServer），由调用方统一回单行状态码。
// 返回有效地图数量；为 0 时释放两数组置空。
static int MixmapRcon_BuildPool(ArrayList hMaps)
{
	delete g_hArrayPools;
	g_hArrayPools = new ArrayList(ByteCountToCells(128));

	delete g_hArraySurvivorSets;
	g_hArraySurvivorSets = new ArrayList();

	if (!hMaps || !hMaps.Length)
	{
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		return 0;
	}

	char sMode[32];
	FindConVar("mp_gamemode").GetString(sMode, sizeof(sMode));

	for (int i = 0; i < hMaps.Length; i++)
	{
		char sBuffer[128];
		hMaps.GetString(i, sBuffer, sizeof(sBuffer));
		if (sBuffer[0] == '\0')
			continue;

		// check blacklist.
		if (g_hCvar_EnableBlackList.BoolValue && g_hArrayBlackList && g_hArrayBlackList.Length)
		{
			if (CheckBlackList(sBuffer))
				continue;
		}

		// erase the invalid map name.
		SourceKeyValues kvMissionInfo;
		SourceKeyValues kvMapInfo = TheMatchExt.GetMapInfoByBspName(sBuffer, sMode, view_as<Address>(kvMissionInfo));
		if (!kvMapInfo || kvMapInfo.IsNull())
			continue;

		if (!kvMissionInfo || kvMissionInfo.IsNull())
			continue;

		// 成对 push，保证两数组等长同序
		g_hArraySurvivorSets.Push(kvMissionInfo.GetInt("survivor_set", 2));
		g_hArrayPools.PushString(sBuffer);
	}

	if (!g_hArrayPools.Length)
	{
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		return 0;
	}

	return g_hArrayPools.Length;
}

// sm_mixmap_load_pool "<initiator_name>" "<map1 map2 ... mapN>" ["<preset_name>"]
Action cmdMixmapLoadPool(int args)
{
	if (args < 2)
	{
		// 用法提示只给人工调试；后端不会发缺参命令
		PrintToServer("pool_empty");
		return Plugin_Handled;
	}

	if (MixmapRcon_RejectIfBusy())
		return Plugin_Handled;

	char sPlayer[MAX_NAME_LENGTH];
	char sMapsArg[2048];
	char sPresetName[256];
	GetCmdArg(1, sPlayer, sizeof(sPlayer));
	GetCmdArg(2, sMapsArg, sizeof(sMapsArg));
	if (args >= 3)
		GetCmdArg(3, sPresetName, sizeof(sPresetName));
	else
		strcopy(sPresetName, sizeof(sPresetName), "WebMap");

	if (sPresetName[0] == '\0')
		strcopy(sPresetName, sizeof(sPresetName), "WebMap");

	int initiator = MixmapRcon_FindClientByName(sPlayer);
	if (initiator <= 0)
	{
		PrintToServer("player_not_found");
		return Plugin_Handled;
	}

	// 解析空格分隔地图序列
	ArrayList hMaps = new ArrayList(ByteCountToCells(128));
	char sMap[128];
	int idx = 0;
	int len = strlen(sMapsArg);
	while (idx < len)
	{
		// skip spaces
		while (idx < len && sMapsArg[idx] == ' ')
			idx++;
		if (idx >= len)
			break;

		int start = idx;
		while (idx < len && sMapsArg[idx] != ' ')
			idx++;

		int n = idx - start;
		if (n <= 0)
			continue;
		if (n >= sizeof(sMap))
			n = sizeof(sMap) - 1;

		for (int i = 0; i < n; i++)
			sMap[i] = sMapsArg[start + i];
		sMap[n] = '\0';

		if (sMap[0] != '\0')
			hMaps.PushString(sMap);
	}

	if (!MixmapRcon_BuildPool(hMaps))
	{
		delete hMaps;
		PrintToServer("pool_empty");
		return Plugin_Handled;
	}
	delete hMaps;

	// 无可投票真人时不发起，避免官方 CreateMixmapVote 走 DisplayVote(_, 0, 20)。
	// 同时清掉刚建的池，避免残留半成品状态。
	if (MixmapRcon_CountVoters() == 0)
	{
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		PrintToServer("no_voters");
		return Plugin_Handled;
	}

	// MapSet_Preset 分支假设投票发起前 g_hArrayPools 已填好
	g_iMapsetType = MapSet_Preset;
	strcopy(g_sPresetName, sizeof(g_sPresetName), sPresetName);

	CreateMixmapVote(initiator, MapSet_Preset);
	PrintToServer("pool_vote_started");
	return Plugin_Handled;
}

// sm_mixmap_start_auto "<initiator_name>" "<type>"
// type: official / custom / mixtape
Action cmdMixmapStartAuto(int args)
{
	if (args < 2)
	{
		PrintToServer("bad_type");
		return Plugin_Handled;
	}

	if (MixmapRcon_RejectIfBusy())
		return Plugin_Handled;

	char sPlayer[MAX_NAME_LENGTH];
	char sType[32];
	GetCmdArg(1, sPlayer, sizeof(sPlayer));
	GetCmdArg(2, sType, sizeof(sType));

	MapSetType type;
	if (StrEqual(sType, "official", false))
		type = MapSet_Official;
	else if (StrEqual(sType, "custom", false))
		type = MapSet_Custom;
	else if (StrEqual(sType, "mixtape", false))
		type = MapSet_Mixtape;
	else
	{
		PrintToServer("bad_type");
		return Plugin_Handled;
	}

	int initiator = MixmapRcon_FindClientByName(sPlayer);
	if (initiator <= 0)
	{
		PrintToServer("player_not_found");
		return Plugin_Handled;
	}

	// 无可投票真人时不发起，避免官方 CreateMixmapVote 走 DisplayVote(_, 0, 20)。
	if (MixmapRcon_CountVoters() == 0)
	{
		PrintToServer("no_voters");
		return Plugin_Handled;
	}

	// 自动模式：投票通过后由 InitiateMixmap 走 CollectAllMaps + SelectRandomMap
	CreateMixmapVote(initiator, type);
	PrintToServer("auto_vote_started");
	return Plugin_Handled;
}

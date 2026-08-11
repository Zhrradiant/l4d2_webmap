#if defined _l4d2_mixmap_actions_included
	#endinput
#endif
#define _l4d2_mixmap_actions_included

void InitiateMixmap(MapSetType type)
{
	switch (type)
	{
		case MapSet_Official, MapSet_Custom, MapSet_Mixtape:
		{
			CollectAllMaps(type);
			if (!SelectRandomMap())
			{
				CPrintToChatAll("%t", "FailedToGet");
				g_bMapsetInitialized = false;
				return;
			}

			g_iMapsetType = type;
			CPrintToChatAll("%t", "StartingIn", g_hCvar_SecondsToRead.IntValue);
			CreateTimer(g_hCvar_SecondsToRead.FloatValue, Timer_StartFirstMissionMixmap);
		}

		case MapSet_Manual, MapSet_Preset, MapSet_PresetRandom:
		{
			g_iMapsetType = type;
			CPrintToChatAll("%t", "StartingIn", g_hCvar_SecondsToRead.IntValue);
			CreateTimer(g_hCvar_SecondsToRead.FloatValue, Timer_StartFirstMapMixmap);
		}
	}
}

// OnChangeMissionVote needs mission name.
void Timer_StartFirstMissionMixmap(Handle timer)
{
	char sMap[128], sMissionName[128];
	g_hArrayPools.GetString(0, sMap, sizeof(sMap));
	g_hMapChapterNames.GetString(sMap, sMissionName, sizeof(sMissionName));

	g_hLogger.info("### Starting Mixmap with %s", sMissionName);
	

	g_bMapsetInitialized = true;
	Call_StartForward(g_hForwardStart);
	Call_PushCell(g_hArrayPools.Length);
	Call_PushCell(g_iMapsetType);
	Call_PushString("");
	Call_Finish();

	TheDirector.OnChangeMissionVote(sMissionName);
}

void Timer_StartFirstMapMixmap(Handle timer)
{
	char sMap[128];
	g_hArrayPools.GetString(0, sMap, sizeof(sMap));

	g_hLogger.info("### Starting Mixmap with %s", sMap);
	
	g_bMapsetInitialized = true;
	Call_StartForward(g_hForwardStart);
	Call_PushCell(g_hArrayPools.Length);
	Call_PushCell(g_iMapsetType);
	Call_PushString(g_sPresetName);
	Call_Finish();

	TheDirector.OnChangeChapterVote(sMap);
}

void Timer_Notify(Handle timer, int userid)
{
	int client = GetClientOfUserId(userid);

	if (client <= 0 || client > MaxClients)
		return;

	if (!IsClientInGame(client))
		return;

	NotifyMixmap(client);
}

void Timer_ShowMaplist(Handle timer, int userid)
{
	int client = GetClientOfUserId(userid);

	if (client <= 0 || client > MaxClients)
		return;

	if (!IsClientInGame(client))
		return;

	NotifyMapList(client);
}

void NotifyMixmap(int client)
{
	char sCurrentMap[64], sNextMap[64];
	GetCurrentMap(sCurrentMap, sizeof(sCurrentMap));

	if (g_iMapsPlayed >= g_hArrayPools.Length)
		Format(sNextMap, sizeof(sNextMap), "%T", "None", client);
	else
		g_hArrayPools.GetString(g_iMapsPlayed, sNextMap, sizeof(sNextMap));

	CPrintToChat(client, "%t", "MapProgress", sCurrentMap, sNextMap);

	if (g_iMapsPlayed == g_hArrayPools.Length)
		CPrintToChat(client, "%t", "HaveReachedTheEnd");
}

void NotifyMapList(int client)
{
	if (g_hArrayPools.Length > 6)
		CPrintToChat(client, "%t", "SeeConsole");

	g_hArrayPools.Length > 6 ?	  // we have a small chat right?
	PrintToConsole(client, "%t", "MapList_NoColor") : 
	CPrintToChat(client, "%t", "MapList");

	char sBuffer[64], sCurrentMap[64], sCurrent[32];
	GetCurrentMap(sCurrentMap, sizeof(sCurrentMap));
	Format(sCurrent, sizeof(sCurrent), "%T", "Current", client);
	for (int i = 0; i < g_hArrayPools.Length; i++)
	{
		g_hArrayPools.GetString(i, sBuffer, sizeof(sBuffer));
		g_hArrayPools.Length > 6 ? PrintToConsole(client, "-> %s %s", sBuffer, (!strcmp(sCurrentMap, sBuffer) && g_iMapsPlayed == i + 1) ? sCurrent : "") : CPrintToChat(client, "{green}-> {olive}%s{default} {orange}%s{default}", sBuffer, (!strcmp(sCurrentMap, sBuffer) && g_iMapsPlayed == i + 1) ? sCurrent : "");
	}
}

// bye bye sourcemod keyvalues.
// @blueblur: not going to check whether the map name is valid or not, since this array is not the one to be used to change map.
void BuildBlackList(int client)
{
	char sPath[128];
	BuildPath(Path_SM, sPath, sizeof(sPath), CONFIG_BLACKLIST);

	KeyValues kv = new KeyValues("BlackList");
	if (!kv.ImportFromFile(sPath))
	{
		delete kv;
		g_hLogger.error("Failed to load black list file from \""... CONFIG_BLACKLIST..."\".");
		if (client != -1 && client > 0 && client <= MaxClients)
			CPrintToChat(client, "%t", "FailedToLoadBlackList");
		return;
	}

	delete g_hArrayBlackList;
	g_hArrayBlackList = new ArrayList(ByteCountToCells(64));

	char sMap[64], sMode[32];
	int count = 0;
	FindConVar("mp_gamemode").GetString(sMode, sizeof(sMode));
	GetBasedMode(sMode, sizeof(sMode));

	char sections[2][32] = { "global_filter", "" };
	strcopy(sections[1], sizeof(sections[]), sMode);
	for (int section = 0; section < sizeof(sections); section++)
	{
		kv.Rewind();
		if (!kv.JumpToKey(sections[section], false) || !kv.GotoFirstSubKey(false))
			continue;

		do
		{
			kv.GetString(NULL_STRING, sMap, sizeof(sMap));
			if (sMap[0] == '\0')
				continue;

			g_hArrayBlackList.PushString(sMap);
			count++;
			if (count >= g_hCvar_BlackListLimit.IntValue)
			{
				delete kv;
				g_hLogger.warning("Reached limit of %d blacklisted maps. Abort the rest.", g_hCvar_BlackListLimit.IntValue);
				if (client != -1 && client > 0 && client <= MaxClients)
					CPrintToChat(client, "%t", "BlackListLoaded");
				return;
			}
		} while (kv.GotoNextKey(false));
	}

	delete kv;
	if (!g_hArrayBlackList.Length)
	{
		g_hLogger.error("No keys found in \""... CONFIG_BLACKLIST..."\" on node %s and global filter.", sMode);
		if (client != -1 && client > 0 && client <= MaxClients)
			CPrintToChat(client, "%t", "NoKeysFoundInBlackList");
		return;
	}

	if (client != -1 && client > 0 && client <= MaxClients)
		CPrintToChat(client, "%t", "BlackListLoaded");
}

void LoadFolderFiles(int client)
{
	delete g_hArrayPresetList;
	g_hArrayPresetList = new ArrayList(ByteCountToCells(512));

	delete g_hArrayPresetNames;
	g_hArrayPresetNames = new ArrayList(ByteCountToCells(512));

	char sPath[128];
	BuildPath(Path_SM, sPath, sizeof(sPath), CONFIG_PRESET_FOLDER);

	DirectoryListing hDir = OpenDirectory(sPath);

	// no directory found.
	if (!hDir)
	{
		if (client != -1 && client > 0 && client <= MaxClients)
			CPrintToChat(client, "%t", "NoPresetFolderFound");

		delete g_hArrayPresetNames;
		delete g_hArrayPresetList;
		g_hLogger.error("Failed to open directory \"%s\".", sPath);
		return;
	}

	FileType type;
	char	 sFile[128];
	while (hDir.GetNext(sFile, sizeof(sFile), type))
	{
		if (StrEqual(sFile, ".") || StrEqual(sFile, ".."))
			continue;

		if (type != FileType_File)
			continue;

		char sFilePath[128];
		Format(sFilePath, sizeof(sFilePath), "%s/%s", sPath, sFile);

		KeyValues kv = new KeyValues("Presets");
		if (!kv.ImportFromFile(sFilePath))
		{
			delete kv;
			g_hLogger.error("Failed to load preset file: \"%s\"", sFilePath);
			continue;
		}

		g_hArrayPresetList.PushString(sFilePath);

		char sPresetName[512];
		kv.GetString("presetName", sPresetName, sizeof(sPresetName), "untitled_preset");
		g_hArrayPresetNames.PushString(sPresetName);
		delete kv;
	}

	if (client != -1 && client > 0 && client <= MaxClients)
		CPrintToChat(client, "%t", "PresetFileReLoaded");

	delete hDir;
}

void LoadPreset(const char[] sFile, int client, bool bRandom)
{
	delete g_hArrayPools;
	g_hArrayPools = new ArrayList(ByteCountToCells(128));

	delete g_hArraySurvivorSets;
	g_hArraySurvivorSets = new ArrayList();

	KeyValues kv = new KeyValues("Presets");
	if (!kv.ImportFromFile(sFile))
	{
		delete kv;
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		g_hLogger.error("Failed to load preset file: \"%s\"", sFile);
		CPrintToChat(client, "%t", "PresetFileLoadFailed");
		return;
	}

	kv.GetString("presetName", g_sPresetName, sizeof(g_sPresetName), "untitled_preset");

	int iUseBased = kv.GetNum("useBased", 1);
	if (!kv.JumpToKey("gamemode", false))
	{
		delete kv;
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		g_hLogger.error("Failed to find subkey \"gamemode\" in preset file: \"%s\"", sFile);
		CPrintToChat(client, "%t", "PresetFileLoadFailed");
		return;
	}

	char sMode[32];
	FindConVar("mp_gamemode").GetString(sMode, sizeof(sMode));

	bool bFound = false;
	if (iUseBased)
		GetBasedMode(sMode, sizeof(sMode));

	if (kv.GotoFirstSubKey(false))
	{
		do
		{
			char sBuffer[32];
			kv.GetString(NULL_STRING, sBuffer, sizeof(sBuffer));
			if (!strcmp(sMode, sBuffer))
			{
				bFound = true;
				break;
			}
		} while (kv.GotoNextKey(false));
	}
	kv.Rewind();

	if (!bFound)
	{
		delete kv;
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		g_hLogger.error("Failed to find gamemode \"%s\" in preset file: \"%s\", useBased: \"%d\"", sMode, sFile, iUseBased);
		CPrintToChat(client, "%t", "PresetFileLoadFailed_GameModeNotMatched");
		return;
	}

	if (!kv.JumpToKey("MapPool", false))
	{
		delete kv;
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		g_hLogger.error("Failed to find subkey \"MapPool\" in preset file: \"%s\"", sFile);
		CPrintToChat(client, "%t", "PresetFileLoadFailed");
		return;
	}

	ArrayList hPresetMaps = new ArrayList(ByteCountToCells(64));
	ArrayList hPresetRoles = new ArrayList();
	ArrayList hPresetSets = new ArrayList();

	if (kv.GotoFirstSubKey(false))
	{
		do
		{
			char sBuffer[64];
			kv.GetString(NULL_STRING, sBuffer, sizeof(sBuffer));

			// check blacklist.
			if (g_hCvar_EnableBlackList.BoolValue && g_hArrayBlackList && g_hArrayBlackList.Length)
			{
				if (CheckBlackList(sBuffer))
				{
					g_hLogger.info("Found map \"%s\" in blacklist.", sBuffer);
					continue;
				}
			}

			// Game-owned KeyValues pointers still use SourceKeyValues; no object is constructed here.
			SourceKeyValues kvMissionInfo;
			SourceKeyValues kvMapInfo = TheMatchExt.GetMapInfoByBspName(sBuffer, sMode, view_as<Address>(kvMissionInfo));
			if (!kvMapInfo || kvMapInfo.IsNull())
			{
				g_hLogger.warning("Failed to find map \"%s\" in gamemode \"%s\".", sBuffer, sMode);
				continue;
			}

			if (!kvMissionInfo || kvMissionInfo.IsNull())
			{
				g_hLogger.warning("Failed to find mission info for map \"%s\" in gamemode \"%s\". kvMissionInfo: %d", sBuffer, sMode, kvMissionInfo);
				continue;
			}

			MapRole role;
			int survivorSet;
			char sMission[128];
			if (!GetMapRoleForMode(sBuffer, sMode, role, survivorSet, sMission, sizeof(sMission)))
			{
				g_hLogger.warning("Failed to classify map \"%s\" in gamemode \"%s\".", sBuffer, sMode);
				continue;
			}

			hPresetMaps.PushString(sBuffer);
			hPresetRoles.Push(role);
			hPresetSets.Push(survivorSet);
		} while (kv.GotoNextKey(false));
	}

	bool bBuilt = false;
	if (bRandom)
		bBuilt = BuildRandomPresetPool(hPresetMaps, hPresetRoles, hPresetSets);
	else
		bBuilt = BuildOrderedPresetPool(hPresetMaps, hPresetRoles, hPresetSets);

	delete hPresetMaps;
	delete hPresetRoles;
	delete hPresetSets;

	if (!bBuilt)
	{
		delete kv;
		delete g_hArrayPools;
		delete g_hArraySurvivorSets;
		g_hLogger.error("Preset file \"%s\" has no valid map pool for the requested mode.", sFile);
		CPrintToChat(client, "%t", "PresetFileLoadFailed");
		return;
	}

	CPrintToChat(client, "%t", "PresetFileLoaded", g_sPresetName, g_hCvar_PresetLoadDelay.IntValue);
	DataPack dp = new DataPack();
	dp.WriteCell(GetClientUserId(client));
	dp.WriteCell(view_as<int>(bRandom ? MapSet_PresetRandom : MapSet_Preset));
	CreateTimer(g_hCvar_PresetLoadDelay.FloatValue, Timer_LoadPresetFile, dp);
	delete kv;
}

void ResetPresetOutput()
{
	delete g_hArrayPools;
	g_hArrayPools = new ArrayList(ByteCountToCells(128));
	delete g_hArraySurvivorSets;
	g_hArraySurvivorSets = new ArrayList();
}

void AppendPresetMap(const char[] sMap, int survivorSet)
{
	g_hArrayPools.PushString(sMap);
	g_hArraySurvivorSets.Push(survivorSet);
}

bool PickPresetMap(ArrayList hMaps, ArrayList hRoles, ArrayList hSets, MapRole desired, bool allowSingle, StringMap hUsed, char[] sMap, int mapSize, int &survivorSet)
{
	ArrayList hCandidates = new ArrayList();
	for (int i = 0; i < hMaps.Length; i++)
	{
		MapRole role = view_as<MapRole>(hRoles.Get(i));
		if (role != desired && !(allowSingle && role == MapRole_Single))
			continue;

		hMaps.GetString(i, sMap, mapSize);
		bool bUsed;
		if (hUsed.GetValue(sMap, bUsed))
			continue;

		hCandidates.Push(i);
	}

	if (!hCandidates.Length)
	{
		delete hCandidates;
		return false;
	}

	int selected = hCandidates.Get(GetRandomInt(0, hCandidates.Length - 1));
	hMaps.GetString(selected, sMap, mapSize);
	survivorSet = hSets.Get(selected);
	hUsed.SetValue(sMap, true);
	delete hCandidates;
	return true;
}

bool BuildRandomPresetPool(ArrayList hMaps, ArrayList hRoles, ArrayList hSets)
{
	ResetPresetOutput();
	int capacity = g_hCvar_MapPoolCapacity.IntValue;
	if (capacity < 1 || !hMaps.Length)
		return false;

	StringMap hUsed = new StringMap();
	char sMap[64], sFirst[64], sFinale[64];
	int firstSet, finaleSet;

	if (capacity == 1)
	{
		bool ok = PickPresetMap(hMaps, hRoles, hSets, MapRole_First, true, hUsed, sMap, sizeof(sMap), firstSet);
		if (ok)
			AppendPresetMap(sMap, firstSet);
		delete hUsed;
		return ok;
	}

	// In a multi-map pool a one-map campaign is simultaneously first and finale,
	// so it is not safe in either endpoint slot.
	if (!PickPresetMap(hMaps, hRoles, hSets, MapRole_Finale, false, hUsed, sFinale, sizeof(sFinale), finaleSet))
	{
		delete hUsed;
		return false;
	}

	if (!PickPresetMap(hMaps, hRoles, hSets, MapRole_First, false, hUsed, sFirst, sizeof(sFirst), firstSet))
	{
		delete hUsed;
		ResetPresetOutput();
		return false;
	}

	AppendPresetMap(sFirst, firstSet);
	for (int i = 0; i < capacity - 2; i++)
	{
		if (!PickPresetMap(hMaps, hRoles, hSets, MapRole_Middle, false, hUsed, sMap, sizeof(sMap), firstSet))
		{
			delete hUsed;
			ResetPresetOutput();
			return false;
		}
		AppendPresetMap(sMap, firstSet);
	}

	AppendPresetMap(sFinale, finaleSet);
	delete hUsed;
	return true;
}

bool BuildOrderedPresetPool(ArrayList hMaps, ArrayList hRoles, ArrayList hSets)
{
	ResetPresetOutput();
	if (!hMaps.Length)
		return false;

	int firstIndex = -1;
	int finaleIndex = -1;
	int singleIndex = -1;
	for (int i = 0; i < hMaps.Length; i++)
	{
		MapRole role = view_as<MapRole>(hRoles.Get(i));
		if (firstIndex == -1 && role == MapRole_First)
			firstIndex = i;
		if (role == MapRole_Finale)
			finaleIndex = i;
		if (singleIndex == -1 && role == MapRole_Single)
			singleIndex = i;
	}

	// A one-map campaign is safe only as a complete, one-map preset.
	if (firstIndex == -1)
	{
		if (singleIndex == -1 || hMaps.Length != 1)
			return false;

		char sSingle[64];
		hMaps.GetString(singleIndex, sSingle, sizeof(sSingle));
		AppendPresetMap(sSingle, hSets.Get(singleIndex));
		return true;
	}

	if (finaleIndex == -1)
		finaleIndex = singleIndex;
	if (finaleIndex == -1)
		return false;

	char sMap[64];
	hMaps.GetString(firstIndex, sMap, sizeof(sMap));
	AppendPresetMap(sMap, hSets.Get(firstIndex));

	// Never preserve a bad file order: Finale must remain the final pool entry.
	for (int i = 0; i < hMaps.Length; i++)
	{
		if (view_as<MapRole>(hRoles.Get(i)) != MapRole_Middle)
			continue;

		hMaps.GetString(i, sMap, sizeof(sMap));
		if (g_hArrayPools.FindString(sMap) == -1)
			AppendPresetMap(sMap, hSets.Get(i));
	}

	hMaps.GetString(finaleIndex, sMap, sizeof(sMap));
	if (g_hArrayPools.FindString(sMap) != -1)
		return false;

	AppendPresetMap(sMap, hSets.Get(finaleIndex));
	return true;
}

void Timer_LoadPresetFile(Handle hTimer, DataPack dp)
{
	dp.Reset();
	int client = GetClientOfUserId(dp.ReadCell());
	MapSetType type = view_as<MapSetType>(dp.ReadCell());
	delete dp;

	if (client > 0 && IsClientInGame(client))
		CreateMixmapVote(client, type);
	}

void PluginStartInit()
{
	g_bMapsetInitialized = false;
	g_iMapsPlayed		 = 0;
	g_iMapsetType		 = MapSet_None;
	delete g_hArrayPools;
	delete g_hMapChapterNames;
	delete g_hArraySurvivorSets;

	StoreToAddress(g_bNeedRestore, 1, NumberType_Int8);
}
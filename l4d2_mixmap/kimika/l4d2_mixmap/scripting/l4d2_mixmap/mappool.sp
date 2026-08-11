#if defined _l4d2_mixmap_mappool_included
	#endinput
#endif
#define _l4d2_mixmap_mappool_included

/**
 * -----------------------------
 * Randomly select map section.
 * -----------------------------
 */

// Get all missions and their map names.
void CollectAllMaps(MapSetType type)
{
	delete g_hArrayMissionsAndMaps;
	g_hArrayMissionsAndMaps = new ArrayList();

	delete g_hArraySurvivorSets;
	g_hArraySurvivorSets = new ArrayList();

	char sMode[64], sKey[256];
	FindConVar("mp_gamemode").GetString(sMode, sizeof(sMode));
	GetBasedMode(sMode, sizeof(sMode));

	SourceKeyValues kvMissions = TheMatchExt.GetAllMissions();
	for (SourceKeyValues kvSub = kvMissions.GetFirstTrueSubKey(); !kvSub.IsNull(); kvSub = kvSub.GetNextTrueSubKey())
	{
		char sMissionName[128];
		kvSub.GetName(sMissionName, sizeof(sMissionName));

		// no fake compaign. these are not playable.
		if (IsFakeMission(sMissionName))
			continue;

		switch (type)
		{
			case MapSet_Custom:
			{
				if (IsOfficialMap(sMissionName))
					continue;
			}

			case MapSet_Official:
			{
				if (!IsOfficialMap(sMissionName))
					continue;
			}
		}

		// we find the key modes/<mode> , continue the subkey iteration.
		FormatEx(sKey, sizeof(sKey), "modes/%s", sMode);
		SourceKeyValues kvMode = kvSub.FindKey(sKey);

		if (!kvMode || kvMode.IsNull())
			continue;

		// should be free.
		ArrayList hArray = new ArrayList(ByteCountToCells(64));

		// on this case we are iterating "1", "2"... subkeys.
		for (SourceKeyValues kvMapNumber = kvMode.GetFirstTrueSubKey(); !kvMapNumber.IsNull(); kvMapNumber = kvMapNumber.GetNextTrueSubKey())
		{
			char sValue[64];
			kvMapNumber.GetString("Map", sValue, sizeof(sValue));
			hArray.PushString(sValue);
		}

		if (!hArray.Length)
		{
			delete hArray;
			continue;
		}

		// pack mission and map names up. into an arraylist so we can sort them.
		DataPack dp = new DataPack();
		dp.WriteCell(hArray);
		dp.WriteCell(kvSub.GetInt("survivor_set", 2));
		dp.WriteString(sMissionName);
		g_hArrayMissionsAndMaps.Push(dp);
	}
}

bool SelectRandomMap()
{
	SetRandomSeed(view_as<int>(GetEngineTime()));

	if (!g_hArrayMissionsAndMaps || !g_hArrayMissionsAndMaps.Length)
		return false;

	if (g_hArrayMissionsAndMaps.Length < g_hCvar_MapPoolCapacity.IntValue)
	{
		CPrintToChatAll("%t", "NotEnoughMaps");
		CleanMemory();
		return false;
	}

	delete g_hArrayPools;
	g_hArrayPools = new ArrayList(ByteCountToCells(64));

	delete g_hMapChapterNames;
	g_hMapChapterNames = new StringMap();

	delete g_hArraySurvivorSets;
	g_hArraySurvivorSets = new ArrayList();

	for (int i = 0; i < g_hCvar_MapPoolCapacity.IntValue; i++)
	{
		if (!g_hArrayMissionsAndMaps.Length)
		{
			CPrintToChatAll("%t", "NotEnoughMaps");
			CleanMemory();
			return false;
		}

		// first random sort the main arraylist, meaning choosing a mission here randomly.
		// everytime we loop through the arraylist we sort again.
		char sMissionName[64], sMap[64];
		g_hArrayMissionsAndMaps.Sort(Sort_Random, Sort_Integer);
		int		 index = GetRandomInt(0, g_hArrayMissionsAndMaps.Length - 1);
		DataPack dp	   = g_hArrayMissionsAndMaps.Get(index);

		dp.Reset();
		ArrayList hArray	  = dp.ReadCell();
		int		  survivorSet = dp.ReadCell();
		dp.ReadString(sMissionName, sizeof(sMissionName));

		// hArray is only one time used.
		if (hArray && hArray.Length)
		{
			// set the mission's first map name, as we need the mission name to transfer to the first map.
			char sFirstMap[128];
			hArray.GetString(0, sFirstMap, sizeof(sFirstMap));
			g_hMapChapterNames.SetString(sFirstMap, sMissionName);

			// the first map should be always the first one.
			if (i == 0)
			{
				// ignore scavenge mode.
				if (L4D2_IsScavengeMode())
					continue;

				hArray.GetString(0, sMap, sizeof(sMap));
				if ((g_hCvar_EnableBlackList.BoolValue && g_hArrayBlackList) && CheckBlackList(sMap))
				{
					i--;	// do not take this into count.
					continue;
				}

				g_hArrayPools.PushString(sMap);
				g_hArraySurvivorSets.Push(survivorSet);
			}
			// the last selection must be the finale.
			else if (i == g_hCvar_MapPoolCapacity.IntValue - 1)
			{
				// ignore scavenge mode.
				if (L4D2_IsScavengeMode())
					continue;

				hArray.GetString(hArray.Length - 1, sMap, sizeof(sMap));
				if ((g_hCvar_EnableBlackList.BoolValue && g_hArrayBlackList) && CheckBlackList(sMap))
				{
					// dont need this, as it only have one map and it's on the blacklist.
					if (hArray.Length == 1)
					{
						DiscardMissionCandidate(index, hArray, dp);
						i--;
						continue;
					}

					i--;
					continue;
				}

				g_hArrayPools.PushString(sMap);
				g_hArraySurvivorSets.Push(survivorSet);
			}
			else
			{
				// we need at least 2 maps to make a selection. we are in the middle of the pool.
				if (hArray.Length > 2)
				{
					// erase the head and tail.
					// ignore scavenge mode.
					if (!L4D2_IsScavengeMode())
					{
						hArray.Erase(hArray.Length - 1);
						hArray.Erase(0);
					}

					// Determine how many maps to take from this campaign.
					// Slots from i to (capacity-2) are middle slots;
					// the last slot (capacity-1) is always reserved for the finale.
					int iSlotsLeft   = g_hCvar_MapPoolCapacity.IntValue - 1 - i;
					int iMaxFromCvar = g_hCvar_MaxMapsPerCampaign.IntValue;
					int iCountToTake;
					if (iMaxFromCvar <= 0)
						iCountToTake = (iSlotsLeft < hArray.Length) ? iSlotsLeft : hArray.Length;
					else
					{
						iCountToTake = iMaxFromCvar;
						if (iSlotsLeft    < iCountToTake) iCountToTake = iSlotsLeft;
						if (hArray.Length < iCountToTake) iCountToTake = hArray.Length;
					}
					if (iCountToTake < 1) iCountToTake = 1;

					// Shuffle once, then collect up to iCountToTake valid (non-blacklisted) maps.
					hArray.Sort(Sort_Random, Sort_String);

					ArrayList hPicked = new ArrayList(ByteCountToCells(64));
					if (g_hCvar_EnableBlackList.BoolValue && g_hArrayBlackList)
					{
						for (int j = 0; j < hArray.Length && hPicked.Length < iCountToTake; j++)
						{
							hArray.GetString(j, sMap, sizeof(sMap));
							if (!CheckBlackList(sMap))
								hPicked.PushString(sMap);
						}
					}
					else
					{
						for (int j = 0; j < iCountToTake && j < hArray.Length; j++)
						{
							hArray.GetString(j, sMap, sizeof(sMap));
							hPicked.PushString(sMap);
						}
					}

					// All candidates were blacklisted – skip this campaign and retry.
					if (!hPicked.Length)
					{
						i--;
						delete hPicked;
						DiscardMissionCandidate(index, hArray, dp);
						continue;
					}

					for (int j = 0; j < hPicked.Length; j++)
					{
						hPicked.GetString(j, sMap, sizeof(sMap));
						g_hArrayPools.PushString(sMap);
						g_hArraySurvivorSets.Push(survivorSet);
					}

					// Extra maps already consumed their own slots; advance i accordingly
					// (the outer for-loop will add 1 more on its own).
					i += hPicked.Length - 1;

					delete hPicked;
				}
				// 2关战役（m1+finale）在头尾Erase后Length==0，没有中间关，跳过
				else if (hArray.Length == 2)
				{
					i--;
					DiscardMissionCandidate(index, hArray, dp);
					continue; // 没有中间关，不能往中间槽塞m1
				}
				// do not take any action, as this can be a finale map.
				else if (hArray.Length == 1)
				{
					// ignore scavenge mode.
					if (!L4D2_IsScavengeMode())
					{
						// we need to decrease the index, as we do not push any map into the arraylist.
						i--;
						DiscardMissionCandidate(index, hArray, dp);
						continue;	 // skip this, this is a finale map in the middle of pool.
					}
					else
					{
						g_hArrayPools.PushString(sMap);
						g_hArraySurvivorSets.Push(survivorSet);
					}
				}
			}

			delete hArray;
		}

		// earse this one you've made this far for next selection. make sure no same compaign map.
		g_hArrayMissionsAndMaps.Erase(index);

		delete dp;
	}

	CleanMemory();

	for (int i = 1; i < MaxClients; i++)
	{
		if (!IsClientInGame(i) || IsFakeClient(i))
			continue;

		NotifyMapList(i);
	}

	return true;
}

void DiscardMissionCandidate(int index, ArrayList hArray, DataPack dp)
{
	g_hArrayMissionsAndMaps.Erase(index);
	delete hArray;
	delete dp;
}

void CleanMemory()
{
	if (g_hArrayMissionsAndMaps && g_hArrayMissionsAndMaps.Length)
	{
		for (int i = 0; i < g_hArrayMissionsAndMaps.Length; i++)
		{
			DataPack dp = g_hArrayMissionsAndMaps.Get(i);
			dp.Reset();
			ArrayList hArray = dp.ReadCell();
			delete hArray;
			delete dp;
		}
	}

	delete g_hArrayMissionsAndMaps;
}

/**
 * -----------------------------
 * Manully select map section.
 * -----------------------------
 */

// menu is BAAAAAAAAAAAAAAAAD.
static int g_iManualMissionIndex = -1;

void CollectAllMapsEx(int client, MapSetType type)
{
	g_bManullyChoosingMap	= true;
	g_iManualMissionIndex = -1;

	if (!CollectMissionsToMenu(type, client))
	{
		g_bManullyChoosingMap	= false;
		return;
	}

	// Both manual modes start with a campaign list. The selected chapter is
	// either drawn safely or shown in a second, campaign-specific menu.
	CreateManullySelectCampaignMenu(client);
}

void CreateManullySelectCampaignMenu(int client)
{
	char sBuffer[128];
	Menu menu = new Menu(MenuHandler_ChooseCampaign);
	FormatEx(sBuffer, sizeof(sBuffer), "%T", "MenuTitle_ChooseCampaign", client);
	menu.SetTitle(sBuffer);

	int iValidCampaigns = 0;
	for (int i = 0; i < g_hArrayMissionsAndMaps.Length; i++)
	{
		DataPack dp = g_hArrayMissionsAndMaps.Get(i);
		dp.Reset();
		ArrayList hMaps = dp.ReadCell();
		dp.ReadCell();
		dp.ReadCell();
		dp.ReadString(sBuffer, sizeof(sBuffer));
		if (!CampaignHasValidChapter(hMaps))
			continue;

		char sIndex[12];
		IntToString(i, sIndex, sizeof(sIndex));
		menu.AddItem(sIndex, sBuffer);
		iValidCampaigns++;
	}

	if (!iValidCampaigns)
	{
		delete menu;
		CPrintToChat(client, "%t", "NoValidChapterForCampaign");
		AbortManualSelection(client, false);
		return;
	}

	menu.ExitBackButton = true;
	menu.Display(client, MENU_TIME_FOREVER);
}

void MenuHandler_ChooseCampaign(Menu menu, MenuAction action, int client, int selection)
{
	switch (action)
	{
		case MenuAction_Select:
		{
			char sIndex[12];
			menu.GetItem(selection, sIndex, sizeof(sIndex));
			int missionIndex = StringToInt(sIndex);
			if (g_bManualCampaignOnly)
				SelectRandomChapterFromCampaign(client, missionIndex);
			else
				CreateManualChapterMenu(client, missionIndex);
		}

		case MenuAction_Cancel:
		{
			if (selection == MenuCancel_ExitBack)
			{
				AbortManualSelection(client, false);
				ManullySelectMap_ChooseMode(client);
			}
			else
			{
				AbortManualSelection(client, false);
			}
		}

		case MenuAction_End:
			delete menu;
	}
}

bool IsRoleAllowedAtCurrentSlot(MapRole role)
{
	int capacity = g_hCvar_MapPoolCapacity.IntValue;
	int current = g_hArrayPools.Length;

	if (capacity <= 1)
		return role == MapRole_First || role == MapRole_Single;
	if (current == 0)
		return role == MapRole_First;
	if (current == capacity - 1)
		return role == MapRole_Finale;
	return role == MapRole_Middle;
}

bool IsManualMapCandidate(const char[] sMap)
{
	if (g_hArrayPools.FindString(sMap) != -1)
		return false;
	if (g_hCvar_EnableBlackList.BoolValue && g_hArrayBlackList && CheckBlackList(sMap))
		return false;

	MapRole role;
	int survivorSet;
	char sMission[128];
	return GetCurrentMapRole(sMap, role, survivorSet, sMission, sizeof(sMission)) && IsRoleAllowedAtCurrentSlot(role);
}

bool CampaignHasValidChapter(ArrayList hMaps)
{
	char sMap[64];
	for (int i = 0; i < hMaps.Length; i++)
	{
		hMaps.GetString(i, sMap, sizeof(sMap));
		if (IsManualMapCandidate(sMap))
			return true;
	}
	return false;
}

void CreateManualChapterMenu(int client, int missionIndex)
{
	if (missionIndex < 0 || missionIndex >= g_hArrayMissionsAndMaps.Length)
	{
		CreateManullySelectCampaignMenu(client);
		return;
	}

	g_iManualMissionIndex = missionIndex;
	DataPack dp = g_hArrayMissionsAndMaps.Get(missionIndex);
	dp.Reset();
	ArrayList hMaps = dp.ReadCell();
	StringMap hNames = dp.ReadCell();
	dp.ReadCell();

	char sTitle[128], sMap[64], sDisplay[128], sChapter[64];
	dp.ReadString(sTitle, sizeof(sTitle));
	Menu menu = new Menu(MenuHandler_ChooseManualChapter);
	FormatEx(sDisplay, sizeof(sDisplay), "%T", "MenuTitle_ChooseFrom", client, sTitle);
	menu.SetTitle(sDisplay);

	int iCandidates = 0;
	for (int i = 0; i < hMaps.Length; i++)
	{
		hMaps.GetString(i, sMap, sizeof(sMap));
		if (!IsManualMapCandidate(sMap))
			continue;

		if (!hNames.GetString(sMap, sChapter, sizeof(sChapter)))
			strcopy(sChapter, sizeof(sChapter), sMap);
		FormatEx(sDisplay, sizeof(sDisplay), "%s - %s", sMap, sChapter);
		menu.AddItem(sMap, sDisplay);
		iCandidates++;
	}

	if (!iCandidates)
	{
		delete menu;
		CPrintToChat(client, "%t", "NoValidChapterForCampaign");
		CreateManullySelectCampaignMenu(client);
		return;
	}

	menu.ExitBackButton = true;
	menu.Display(client, MENU_TIME_FOREVER);
}

void MenuHandler_ChooseManualChapter(Menu menu, MenuAction action, int client, int selection)
{
	switch (action)
	{
		case MenuAction_Select:
		{
			char sMap[64];
			menu.GetItem(selection, sMap, sizeof(sMap));
			if (!IsManualMapCandidate(sMap))
			{
				CPrintToChat(client, "%t", "InvalidChapterRole");
				CreateManualChapterMenu(client, g_iManualMissionIndex);
				return;
			}

			MapRole role;
			int survivorSet;
			char sMission[128];
			if (!GetCurrentMapRole(sMap, role, survivorSet, sMission, sizeof(sMission)))
			{
				CPrintToChat(client, "%t", "NoValidChapterForCampaign");
				CreateManualChapterMenu(client, g_iManualMissionIndex);
				return;
			}

			g_hArrayPools.PushString(sMap);
			g_hArraySurvivorSets.Push(survivorSet);
			CPrintToChat(client, "%t", "AddedInto", sMap);
			ContinueOrFinishManualSelection(client);
		}

		case MenuAction_Cancel:
		{
			if (selection == MenuCancel_ExitBack)
				CreateManullySelectCampaignMenu(client);
			else
				AbortManualSelection(client, false);
		}

		case MenuAction_End:
			delete menu;
	}
}

void ContinueOrFinishManualSelection(int client)
{
	if (g_hArrayPools.Length >= g_hCvar_MapPoolCapacity.IntValue)
	{
		CleanMemoryEx();
		g_bManullyChoosingMap = false;
		g_iManualMissionIndex = -1;
		CPrintToChat(client, "%t", "FullSelected", g_hCvar_ManualSelectDelay.IntValue);
		CreateTimer(g_hCvar_ManualSelectDelay.FloatValue, Timer_PreparedToVote, GetClientUserId(client));
	}
	else
	{
		CreateManullySelectCampaignMenu(client);
	}
}

void AbortManualSelection(int client, bool reopenModeMenu)
{
	CleanMemoryEx();
	g_bManullyChoosingMap = false;
	g_iManualMissionIndex = -1;
	if (reopenModeMenu && client > 0 && client <= MaxClients && IsClientInGame(client))
		ManullySelectMap_ChooseMode(client);
}

void SelectRandomChapterFromCampaign(int client, int missionIndex)
{
	if (missionIndex < 0 || missionIndex >= g_hArrayMissionsAndMaps.Length)
	{
		CreateManullySelectCampaignMenu(client);
		return;
	}

	DataPack dp = g_hArrayMissionsAndMaps.Get(missionIndex);
	dp.Reset();
	ArrayList hMaps = dp.ReadCell();
	dp.ReadCell();
	int survivorSet = dp.ReadCell();

	int capacity = g_hCvar_MapPoolCapacity.IntValue;

	// A single-map campaign cannot be used in a multi-map pool: it is both
	// first and finale, and either placement would violate the other slot.
	if (capacity > 1 && hMaps.Length == 1)
	{
		CPrintToChat(client, "%t", "NoValidChapterForCampaign");
		CreateManullySelectCampaignMenu(client);
		return;
	}

	ArrayList hCandidates = new ArrayList(ByteCountToCells(64));
	char sMap[64];
	for (int i = 0; i < hMaps.Length; i++)
	{
		hMaps.GetString(i, sMap, sizeof(sMap));
		if (IsManualMapCandidate(sMap))
			hCandidates.PushString(sMap);
	}

	if (!hCandidates.Length)
	{
		delete hCandidates;
		CPrintToChat(client, "%t", "NoValidChapterForCampaign");
		CreateManullySelectCampaignMenu(client);
		return;
	}

	hCandidates.Sort(Sort_Random, Sort_String);
	hCandidates.GetString(0, sMap, sizeof(sMap));
	delete hCandidates;

	g_hArrayPools.PushString(sMap);
	g_hArraySurvivorSets.Push(survivorSet);
	CPrintToChat(client, "%t", "AddedInto", sMap);

	if (g_hArrayPools.Length == capacity)
	{
		ContinueOrFinishManualSelection(client);
	}
	else
	{
		CreateManullySelectCampaignMenu(client);
	}
}

void Timer_PreparedToVote(Handle timer, int userid)
{
	int client = GetClientOfUserId(userid);
	if (client <= 0 || !IsClientInGame(client))
	{
		PluginStartInit();
		return;
	}

	g_bManullyChoosingMap = false;
	CreateMixmapVote(client, MapSet_Manual);
}

void CleanMemoryEx()
{
	if (g_hArrayMissionsAndMaps && g_hArrayMissionsAndMaps.Length)
	{
		for (int i = 0; i < g_hArrayMissionsAndMaps.Length; i++)
		{
			DataPack dp = g_hArrayMissionsAndMaps.Get(i);
			dp.Reset();

			ArrayList hArray = dp.ReadCell();
			StringMap hMap	 = dp.ReadCell();

			delete hArray;
			delete hMap;
			delete dp;
		}
	}

	delete g_hArrayMissionsAndMaps;
}

bool CollectMissionsToMenu(MapSetType type, int client)
{
	delete g_hArrayPools;
	g_hArrayPools = new ArrayList(ByteCountToCells(64));

	delete g_hArrayMissionsAndMaps;
	g_hArrayMissionsAndMaps = new ArrayList();

	delete g_hArraySurvivorSets;
	g_hArraySurvivorSets = new ArrayList();

	char sMode[64];
	FindConVar("mp_gamemode").GetString(sMode, sizeof(sMode));
	GetBasedMode(sMode, sizeof(sMode));	   // note that this plugin won't consider survival/versus survival.

	SourceKeyValues kvAllMissions = TheMatchExt.GetAllMissions();
	for (SourceKeyValues kvSub = kvAllMissions.GetFirstTrueSubKey(); !kvSub.IsNull(); kvSub = kvSub.GetNextTrueSubKey())
	{
		if (kvSub.IsNull())
			continue;

		char sMissionName[128];
		kvSub.GetName(sMissionName, sizeof(sMissionName));

		// no fake compaign. these are not playable.
		if (IsFakeMission(sMissionName))
			continue;

		switch (type)
		{
			case MapSet_Custom:
			{
				if (IsOfficialMap(sMissionName))
					continue;
			}

			case MapSet_Official:
			{
				if (!IsOfficialMap(sMissionName))
					continue;
			}
		}

		int	 survivorSet = kvSub.GetInt("survivor_set", 2);	   // L4D2 = 2, L4D1 = 1

		char sDisplayTitle[128];
		kvSub.GetString("DisplayTitle", sDisplayTitle, sizeof(sDisplayTitle));

		// official maps use tags to translate map names and other stuff.
		// gotta turn this into your language.
		ConvertTagAndTranslate(sDisplayTitle, sizeof(sDisplayTitle), client, IsOfficialMap(sMissionName));

		char sKey[64];
		FormatEx(sKey, sizeof(sKey), "modes/%s", sMode);
		SourceKeyValues kvMode = kvSub.FindKey(sKey);

		if (kvMode.IsNull())
			continue;

		ArrayList hArray = new ArrayList(ByteCountToCells(64));
		StringMap hMap	 = new StringMap();
		for (SourceKeyValues kvMapNumber = kvMode.GetFirstTrueSubKey(); !kvMapNumber.IsNull(); kvMapNumber = kvMapNumber.GetNextTrueSubKey())
		{
			char sValue[64], sDisplayName[64];
			kvMapNumber.GetString("Map", sValue, sizeof(sValue));
			kvMapNumber.GetString("DisplayName", sDisplayName, sizeof(sDisplayName));
			ConvertTagAndTranslate(sDisplayName, sizeof(sDisplayName), client, IsOfficialMap(sMissionName));

			hMap.SetString(sValue, sDisplayName);
			hArray.PushString(sValue);
		}

		DataPack dp = new DataPack();
		dp.WriteCell(hArray);
		dp.WriteCell(hMap);
		dp.WriteCell(survivorSet);
		dp.WriteString(sDisplayTitle);
		g_hArrayMissionsAndMaps.Push(dp);
	}

	if (!g_hArrayMissionsAndMaps || !g_hArrayMissionsAndMaps.Length)
		return false;

	return true;
}
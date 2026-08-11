#if defined _webmap_vote_included
    #endinput
#endif
#define _webmap_vote_included

// ===========================================================================
// 原生投票逻辑（参考 L4D2 Map vote VoteChangeMap / ChangeMap_Handler）
// ===========================================================================

void StartNativeVote(int initiator, const char[] mission, const char[] mapName) {
    strcopy(g_sVoteMission, sizeof g_sVoteMission, mission);
    strcopy(g_sVoteMap, sizeof g_sVoteMap, mapName);
    g_sVoteInitiator = initiator;

    L4D2NativeVote vote = L4D2NativeVote(WebVote_Handler);
    vote.Initiator = initiator;

    char info[256];
    FormatEx(info, sizeof info, "%s//%s", mission, mapName);
    vote.SetInfo(info);

    char titleMission[128], titleChapter[128];
    fmt_Translate(mission, titleMission, sizeof titleMission, mission);
    fmt_Translate(mapName, titleChapter, sizeof titleChapter, mapName);
    vote.SetTitle("更换地图: %s (%s)", titleMission, titleChapter);

    int team;
    int playerCount = 0;
    int[] clients = new int[MaxClients];
    for (int i = 1; i <= MaxClients; i++) {
        if (!IsClientInGame(i) || IsFakeClient(i))
            continue;
        team = GetClientTeam(i);
        if (team < 2 || team > 3)
            continue;

        clients[playerCount++] = i;
    }

    vote.DisplayVote(clients, playerCount, 20);
}

void WebVote_Handler(L4D2NativeVote vote, VoteAction action, int param1, int param2) {
    switch (action) {
        case VoteAction_Start: {
            char titleMission[128], titleChapter[128];
            fmt_Translate(g_sVoteMission, titleMission, sizeof titleMission, g_sVoteMission);
            fmt_Translate(g_sVoteMap, titleChapter, sizeof titleChapter, g_sVoteMap);

            for (int i = 1; i <= MaxClients; i++) {
                if (IsClientInGame(i) && !IsFakeClient(i)) {
                    CPrintToChat(i, "{blue}%N {default}发起投票 {olive}更换地图: %s (%s)", g_sVoteInitiator, titleMission, titleChapter);
                }
            }
        }

        case VoteAction_PlayerVoted: {
            CPrintToChatAll("{blue}%N {default}已投票", param1);
        }

        case VoteAction_End: {
            int yes = vote.YesCount;
            int no = vote.NoCount;

            // 通过判定 (受 webmap_vote_pass_mode 控制):
            //   模式 0 (宽松, 默认): yes > no, 未投票忽略 —— 发起人那票即可通过.
            //   模式 1 (严格): yes > (反对 + 未投票), 即赞成票严格过半
            //                  (total = vote.PlayerCount), 未投票视作反对,
            //                  等价于 yes*2 > total.
            int mode = g_cvVotePassMode.IntValue;
            bool bPass = (mode == 1) ? (yes * 2 > vote.PlayerCount) : (yes > no);

            if (bPass) {
                vote.SetPass("加载中...");

                // 切图逻辑（复用 L4D2 Map vote ChangeMap_Handler）
                char buffer[128];
                if (g_smFirstMap.GetString(g_sVoteMap, buffer, sizeof buffer))
                    SDKCall(g_hSDK_OnChangeMissionVote, g_pDirector, buffer);
                else
                    ServerCommand("changelevel %s", g_sVoteMap);

                // 推送投票结果
                PushVoteResult("pass", yes, no);
            } else {
                vote.SetFail();
                PushVoteResult("fail", yes, no);
            }
        }
    }
}

void PushVoteResult(const char[] result, int yes, int no) {
    char serverKey[64];
    ResolveServerKey(serverKey, sizeof serverKey);

    char initName[MAX_NAME_LENGTH];
    if (g_sVoteInitiator > 0 && IsClientInGame(g_sVoteInitiator))
        GetClientName(g_sVoteInitiator, initName, sizeof initName);
    else
        strcopy(initName, sizeof initName, "unknown");

    JSONObject data = new JSONObject();
    data.SetString("server_key", serverKey);
    data.SetString("result", result);
    data.SetString("mission", g_sVoteMission);
    data.SetString("map", g_sVoteMap);
    data.SetInt("yes", yes);
    data.SetInt("no", no);
    data.SetString("initiator", initName);

    PostToBackend("/api/vote_result", data);
    delete data;
}

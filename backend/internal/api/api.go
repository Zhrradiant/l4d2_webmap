// Package api 实现 HTTP 接口。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"webmap/internal/config"
	"webmap/internal/onlinemap"
	"webmap/internal/perm"
	"webmap/internal/rcon"
	"webmap/internal/rconcfg"
	"webmap/internal/session"
	"webmap/internal/store"
)

// Server 聚合所有依赖的 API 服务。
type Server struct {
	Cfg        *config.Config
	Store      *store.Store
	Session    *session.Manager
	RconCfg    *rconcfg.Manager
	OnlineMap  *onlinemap.Store         // 在线地图元数据（nil 表示未启用）
	MixPresets *store.MixmapPresetStore // mixmap 网页预设（nil 表示未启用）
	Perm       *perm.Store              // 站点权限层（permissions.json）
}

// Register 在根 mux 上注册所有 API 路由。
func (s *Server) Register(mux *http.ServeMux) {
	// --- 游戏服务器推送（需 X-Api-Key） ---
	mux.HandleFunc("POST /api/push", s.handlePush)
	mux.HandleFunc("POST /api/code", s.handleCode)
	mux.HandleFunc("POST /api/vote_result", s.handleVoteResult)

	// --- 公开接口 ---
	mux.HandleFunc("GET /api/servers", s.handleServers)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/config", s.handleConfig)

	// --- 评论 / 评分 / 标签代理（同源转发到 zhrradiant.com，绕开浏览器 CORS） ---
	mux.HandleFunc("GET /api/comments", s.handleGetComments)
	mux.HandleFunc("POST /api/comments", s.handleSubmitComment)
	mux.HandleFunc("POST /api/comments/like", s.handleToggleCommentLike)
	mux.HandleFunc("POST /api/comments/delete", s.handleDeleteComment)
	mux.HandleFunc("GET /api/ratings", s.handleGetRatings)
	mux.HandleFunc("GET /api/tag-defs", s.handleGetTagDefs)
	mux.HandleFunc("GET /api/tags", s.handleGetTags)

	// --- 受保护接口（需 Bearer token；server state 为公开只读，供游客预览） ---
	mux.HandleFunc("GET /api/me", s.withAuth(s.handleMe))
	mux.HandleFunc("GET /api/server/{key}/state", s.handleServerStatePublic)
	mux.HandleFunc("POST /api/action", s.withAuth(s.handleAction))
	mux.HandleFunc("GET /api/events", s.withAuth(s.handleEvents))

	// --- Mixmap 图池对接 ---
	s.registerMixmapRoutes(mux)
}

// --- 中间件 ---

// checkAPIKey 校验推送密钥。
func (s *Server) checkAPIKey(r *http.Request) bool {
	key := r.Header.Get("X-Api-Key")
	return key != "" && key == s.Cfg.PushSecret
}

// authBearer 从 Authorization 提取 token。
func authBearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if strings.HasPrefix(v, "Bearer ") {
		return strings.TrimPrefix(v, "Bearer ")
	}
	return ""
}

// withAuth 认证中间件。
// 优先从 Authorization header 取 token；也支持 ?token= 查询参数（供浏览器 EventSource 使用）。
func (s *Server) withAuth(h func(w http.ResponseWriter, r *http.Request, sess *session.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := authBearer(r)
		// 浏览器 EventSource 不支持自定义 header，允许 query ?token= 回退
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "未提供 token")
			return
		}
		sess, ok := s.Session.Validate(token)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token 无效或已过期")
			return
		}
		h(w, r, sess)
	}
}

// --- 工具函数 ---

// escapeRCON 转义参数中的反斜杠与双引号，防止命令注入。
// 顺序：先转义反斜杠再转义双引号，避免二次转义。
func escapeRCON(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		log.Printf("[api] %s %s 请求体解析失败 from %s: %v", r.Method, r.URL.Path, r.RemoteAddr, err)
		writeError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return false
	}
	return true
}

// ====================================================================
// 推送接口
// ====================================================================

type pushReq struct {
	ServerKey       string             `json:"server_key"`
	Host            string             `json:"host"`
	Port            int                `json:"port"`
	ServerName      string             `json:"server_name"`
	Gamemode        string             `json:"gamemode"`
	CurrentMap      string             `json:"current_map"`
	Maps            []store.ChapterMap `json:"maps"`
	MixmapAvailable *bool              `json:"mixmap_available,omitempty"`
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if !s.checkAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "无效的 API Key")
		return
	}
	var req pushReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.ServerKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 server_key")
		return
	}

	sd := &store.ServerData{
		ServerKey:  req.ServerKey,
		Host:       req.Host,
		Port:       req.Port,
		Name:       req.ServerName,
		Gamemode:   req.Gamemode,
		CurrentMap: req.CurrentMap,
		Maps:       req.Maps,
	}
	// 可选字段：插件探测 mixmap 是否在线
	if req.MixmapAvailable != nil {
		sd.MixmapAvailable = *req.MixmapAvailable
	}
	if err := s.Store.UpsertServer(sd); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type codeReq struct {
	ServerKey string `json:"server_key"`
	Code      string `json:"code"`
	Player    string `json:"player"`
	SteamID   string `json:"steamid"`
	Expire    int64  `json:"expire"`
	Admin     bool   `json:"admin"` // 可选：是否游戏服务器管理员（权限层模式 1 使用）
}

func (s *Server) handleCode(w http.ResponseWriter, r *http.Request) {
	if !s.checkAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "无效的 API Key")
		return
	}
	var req codeReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.ServerKey == "" || req.Code == "" {
		writeError(w, http.StatusBadRequest, "缺少 server_key 或 code")
		return
	}

	if err := s.Store.AppendCode(req.ServerKey, store.Code{
		Code:    req.Code,
		Player:  req.Player,
		SteamID: req.SteamID,
		Expire:  req.Expire,
		Admin:   req.Admin,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type voteResultReq struct {
	ServerKey string `json:"server_key"`
	Result    string `json:"result"`
	Mission   string `json:"mission"`
	Map       string `json:"map"`
	Yes       int    `json:"yes"`
	No        int    `json:"no"`
	Initiator string `json:"initiator"`
}

func (s *Server) handleVoteResult(w http.ResponseWriter, r *http.Request) {
	if !s.checkAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "无效的 API Key")
		return
	}
	var req voteResultReq
	if !readJSON(w, r, &req) {
		return
	}
	s.Store.BroadcastVoteResult(req.ServerKey, req.Result, req.Mission, req.Map)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ====================================================================
// 公开接口
// ====================================================================

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	summaries := s.Store.List()
	// 预览列表需要把当前地图显示为战役名：逐台补全
	for i := range summaries {
		summaries[i].CurrentMapName = s.currentMapCampaignName(summaries[i])
	}
	writeJSON(w, http.StatusOK, summaries)
}

// currentMapCampaignName 计算服务器当前地图所属战役的展示名（预览列表用）。
// 优先在线元数据中文名，回退本地 mission 翻译，再回退 mission 内部名；找不到返回空串。
func (s *Server) currentMapCampaignName(sum store.ServerSummary) string {
	if sum.CurrentMap == "" {
		return ""
	}
	sd, err := s.Store.Get(sum.ServerKey)
	if err != nil {
		return ""
	}
	for _, m := range sd.Maps {
		if m.ChapterMap != sum.CurrentMap {
			continue
		}
		if s.OnlineMap != nil {
			if e := s.OnlineMap.FindByIdentifier(m.Mission); e != nil && e.ChineseName != "" {
				return e.ChineseName
			}
		}
		if m.MissionDisplayChi != "" {
			return m.MissionDisplayChi
		}
		if m.MissionDisplayEn != "" {
			return m.MissionDisplayEn
		}
		return m.Mission
	}
	return ""
}

// searchServerResp 统合搜索：单台服务器的命中结果（EnrichedMap 已做在线匹配）。
type searchServerResp struct {
	ServerKey      string              `json:"server_key"`
	Name           string              `json:"name"`
	Gamemode       string              `json:"gamemode"`
	Online         bool                `json:"online"`
	CurrentMap     string              `json:"current_map"`
	CurrentMapName string              `json:"current_map_name"`
	Matches        []store.EnrichedMap `json:"matches"`
}

// handleSearch 统合搜索：跨全部服务器检索包含关键字的地图（预览系统首页用，公开只读）。
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"query":   q,
			"results": []searchServerResp{},
		})
		return
	}
	// 统合搜索：除本地字段外，也把 CSV 在线中文名 / 大厅展示名 / 识别名并入匹配，
	// 与具体服务器视图前端 getFilteredCampaigns 的搜索口径一致。
	// OnlineMap 未启用时 onlineRef 为 nil，退回仅本地字段匹配。
	var onlineRef func(mission string) *store.OnlineMapRef
	if s.OnlineMap != nil {
		onlineRef = func(mission string) *store.OnlineMapRef {
			if e := s.OnlineMap.FindByIdentifier(mission); e != nil {
				return onlineToRef(e)
			}
			return nil
		}
	}
	raw := s.Store.SearchMaps(q, onlineRef)
	results := make([]searchServerResp, 0, len(raw))
	for _, sr := range raw {
		res := searchServerResp{
			ServerKey:  sr.ServerKey,
			Name:       sr.Name,
			Gamemode:   sr.Gamemode,
			Online:     sr.Online,
			CurrentMap: sr.CurrentMap,
			Matches:    s.matchOnlineMaps(sr.Matches),
		}
		res.CurrentMapName = s.currentMapCampaignName(store.ServerSummary{
			ServerKey:  sr.ServerKey,
			CurrentMap: sr.CurrentMap,
		})
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   q,
		"results": results,
	})
}

type loginReq struct {
	Code string `json:"code"`
}

type loginResp struct {
	OK               bool   `json:"ok"`
	Token            string `json:"token,omitempty"`
	ServerKey        string `json:"server_key,omitempty"`
	Player           string `json:"player,omitempty"`
	ServerName       string `json:"server_name,omitempty"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
	CanManagePresets bool   `json:"can_manage_presets"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "缺少验证码")
		return
	}

	// O(1) 通过 code 索引查找所属服务器
	svKey, _, ok := s.Store.LookupCode(req.Code)
	if !ok {
		writeError(w, http.StatusUnauthorized, "验证码无效或已过期")
		return
	}

	sess, err := s.Session.Login(s.Store, svKey, req.Code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "验证码无效或已过期")
		return
	}
	sd, _ := s.Store.Get(sess.ServerKey)
	name := ""
	if sd != nil {
		name = sd.Name
	}
	writeJSON(w, http.StatusOK, loginResp{
		OK:               true,
		Token:            sess.Token,
		ServerKey:        sess.ServerKey,
		Player:           sess.Player,
		ServerName:       name,
		ExpiresAt:        sess.Expire.Unix(),
		CanManagePresets: s.canManagePresets(sess),
	})
}

// bgConfigResp 前端背景配置响应。
type bgConfigResp struct {
	BgMode string `json:"bg_mode"` // "default" / "custom" / "none"
	BgURL  string `json:"bg_url"`  // 背景图链接
}

// handleConfig 返回前端背景配置（无需登录）。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, bgConfigResp{
		BgMode: s.Cfg.BgMode,
		BgURL:  s.Cfg.BgURL,
	})
}

// ====================================================================
// 受保护接口
// ====================================================================

// canManagePresets 判断会话是否具备预设管理权限（权限层判定）。
func (s *Server) canManagePresets(sess *session.Session) bool {
	return s.Perm != nil && s.Perm.CanManagePresets(sess.SteamID, sess.GameAdmin)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	me := sess.Repr()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":                 true,
		"token":              me.Token,
		"server_key":         me.ServerKey,
		"player":             me.Player,
		"expires_at":         me.ExpiresAt,
		"can_manage_presets": s.canManagePresets(sess),
	})
}

// handleServerStatePublic 服务器状态接口（预览模式公开只读）。
// 无 token 时按游客（预览系统）放行；带 token 时校验会话，未授权访问他人服务器返回 403。
func (s *Server) handleServerStatePublic(w http.ResponseWriter, r *http.Request) {
	var sess *session.Session
	token := authBearer(r)
	if token != "" {
		var ok bool
		sess, ok = s.Session.Validate(token)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token 无效或已过期")
			return
		}
	}
	s.serveServerState(w, r, sess)
}

func (s *Server) serveServerState(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	key := r.PathValue("key")
	if sess != nil && key != sess.ServerKey {
		writeError(w, http.StatusForbidden, "无权访问此服务器")
		return
	}
	sd, err := s.Store.GetPublicState(key)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// 匹配在线地图数据
	enriched := s.matchOnlineMaps(sd.Maps)

	resp := serverStateResp{
		ServerKey:       sd.ServerKey,
		Name:            sd.Name,
		Gamemode:        sd.Gamemode,
		CurrentMap:      sd.CurrentMap,
		Maps:            enriched,
		UpdatedAt:       sd.UpdatedAt,
		Online:          sd.Online,
		MixmapAvailable: sd.MixmapAvailable,
	}
	writeJSON(w, http.StatusOK, resp)
}

// serverStateResp 服务器状态响应（含 EnrichedMap）。
type serverStateResp struct {
	ServerKey       string              `json:"server_key"`
	Name            string              `json:"name"`
	Gamemode        string              `json:"gamemode"`
	CurrentMap      string              `json:"current_map"`
	Maps            []store.EnrichedMap `json:"maps"`
	UpdatedAt       int64               `json:"updated_at"`
	Online          bool                `json:"online"`
	MixmapAvailable bool                `json:"mixmap_available"`
}

type actionReq struct {
	Action  string `json:"action"`
	Mission string `json:"mission"`
	Map     string `json:"map"`
}

// handleAction 处理换图/投票动作。仅支持 vote。
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	var req actionReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Action != "vote" {
		writeError(w, http.StatusBadRequest, "仅支持 vote 动作")
		return
	}
	if req.Mission == "" || req.Map == "" {
		writeError(w, http.StatusBadRequest, "缺少 mission 或 map")
		return
	}

	// 获取服务器连接信息
	sd, err := s.Store.Get(sess.ServerKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// 通过 RCON status 检查投票发起者是否仍在服务器上（按 SteamID 精确匹配）
	if sess.SteamID != "" {
		online, err := s.checkPlayerOnline(sd, sess.SteamID, r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "查询在线玩家失败: "+err.Error())
			return
		}
		if !online {
			writeError(w, http.StatusForbidden, "你已不在该服务器上，请重新获取验证码")
			return
		}
	}

	// 取密码
	password, err := s.RconCfg.GetPassword(sess.ServerKey, sd.Port)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "RCON 密码未配置: "+err.Error())
		return
	}

	// 取 host（可被 override）
	host := s.RconCfg.GetHost(sess.ServerKey, sd.Host)
	addr := fmt.Sprintf("%s:%d", host, sd.Port)

	// 构造 RCON 命令（转义双引号防注入）
	cmd := fmt.Sprintf(`sm_web_vote "%s" "%s" "%s"`,
		escapeRCON(sess.Player), escapeRCON(req.Mission), escapeRCON(req.Map))

	reply, err := rcon.ExecuteOnce(addr, password, cmd, s.Cfg.RconDuration(), r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "RCON 执行失败: "+err.Error())
		return
	}

	reply = strings.TrimSpace(reply)
	switch reply {
	case "vote_in_progress":
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": "当前有投票正在进行中，请等待结束后再试",
		})
	case "player_not_found":
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": "发起者已不在服务器上，请重新获取验证码",
		})
	case "map_invalid":
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": false, "error": "目标地图不可用",
		})
	case "vote_started":
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "rcon_reply": reply,
		})
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "rcon_reply": reply,
		})
	}
}

// handleEvents SSE 推送服务器状态变化。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "不支持 SSE")
		return
	}

	key := r.URL.Query().Get("server")
	if key != sess.ServerKey {
		writeError(w, http.StatusForbidden, "无权订阅此服务器")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// SSE 是长连接，禁用 http.Server.WriteTimeout（30s）对该连接的限制，
	// 改由 maxSSELifetime（30 分钟硬上限）与 15s 心跳共同控制生命周期。
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	// 订阅事件
	ch, cancel := s.Store.Subscribe(key)
	if ch == nil {
		writeError(w, http.StatusServiceUnavailable, "订阅者已满")
		return
	}
	defer cancel()

	// 心跳
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	fmt.Fprintf(w, "event: connected\ndata: {\"server\":\"%s\"}\n\n", key)
	flusher.Flush()

	// 最大 SSE 连接生命周期（防止死连接长期占用资源）
	const maxSSELifetime = 30 * time.Minute
	deadline := time.NewTimer(maxSSELifetime)
	defer deadline.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-pingTicker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, string(data))
			flusher.Flush()
		}
	}
}

// ====================================================================
// 在线地图接口
// ====================================================================

// matchOnlineMaps 将本地地图与在线地图元数据匹配，生成 EnrichedMap。
// 匹配键：插件上报的 Mission == 游戏 mission txt 的 Name 字段 == CSV「地图文件识别名」(identifier)，
// 三者同源，且 identifier 是战役级唯一，故直接用 Mission → identifier 精确匹配。
// 匹配不上则走降级（MatchLevel = "none"），不做 code 兜底。
func (s *Server) matchOnlineMaps(localMaps []store.ChapterMap) []store.EnrichedMap {
	result := make([]store.EnrichedMap, len(localMaps))
	for i, m := range localMaps {
		em := store.EnrichedMap{
			Mission:           m.Mission,
			MissionDisplayEn:  m.MissionDisplayEn,
			MissionDisplayChi: m.MissionDisplayChi,
			ChapterMap:        m.ChapterMap,
			ChapterEn:         m.ChapterEn,
			ChapterChi:        m.ChapterChi,
			Official:          m.Official,
			IsFirst:           m.IsFirst,
		}

		if s.OnlineMap != nil {
			if entry := s.OnlineMap.FindByIdentifier(m.Mission); entry != nil {
				em.Online = onlineToRef(entry)
				em.MatchLevel = "exact"
			} else {
				em.MatchLevel = "none"
			}
		} else {
			em.MatchLevel = "none"
		}

		result[i] = em
	}
	return result
}

// onlineToRef 将 onlinemap.OnlineMapEntry 转为 store.OnlineMapRef。
func onlineToRef(e *onlinemap.OnlineMapEntry) *store.OnlineMapRef {
	return &store.OnlineMapRef{
		ChineseName: e.ChineseName,
		DisplayName: e.DisplayName,
		Identifier:  e.Identifier,
		ImageURL:    e.ImageURL,
	}
}

// ====================================================================
// 在线玩家校验（RCON status）
// ====================================================================

// checkPlayerOnline 通过 RCON status 检查 targetSteamID 是否在服务器在线玩家中。
func (s *Server) checkPlayerOnline(sd *store.ServerData, targetSteamID string, ctx context.Context) (bool, error) {
	password, err := s.RconCfg.GetPassword(sd.ServerKey, sd.Port)
	if err != nil {
		return false, err
	}
	host := s.RconCfg.GetHost(sd.ServerKey, sd.Host)
	addr := fmt.Sprintf("%s:%d", host, sd.Port)

	reply, err := rcon.ExecuteOnce(addr, password, "status", s.Cfg.RconDuration(), ctx)
	if err != nil {
		return false, err
	}
	return parseStatusForSteamID(reply, targetSteamID), nil
}

// parseStatusForSteamID 从 Source 引擎 status 输出中查找指定 SteamID。
// 每行玩家信息格式: # <userid> "<name>" <STEAM_X:Y:Z> <connected> ...
func parseStatusForSteamID(statusOutput string, targetSteamID string) bool {
	for _, raw := range strings.Split(statusOutput, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		// 跳过表头和结尾
		if strings.HasPrefix(line, "# userid") || strings.HasPrefix(line, "#end") {
			continue
		}
		// 找引号内的玩家名，取引号后的第一个 token 即 SteamID
		q1 := strings.Index(line, "\"")
		if q1 < 0 {
			continue
		}
		q2 := strings.Index(line[q1+1:], "\"")
		if q2 < 0 {
			continue
		}
		q2 += q1 + 1
		rest := strings.TrimSpace(line[q2+1:])
		sid := strings.SplitN(rest, " ", 2)[0]
		if sid == targetSteamID {
			return true
		}
	}
	return false
}

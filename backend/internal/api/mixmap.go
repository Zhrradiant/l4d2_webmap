package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"webmap/internal/rcon"
	"webmap/internal/session"
	"webmap/internal/store"
)

// registerMixmapRoutes 注册 mixmap 相关受保护路由。
func (s *Server) registerMixmapRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/mixmap/start", s.withAuth(s.handleMixmapStart))
	mux.HandleFunc("POST /api/mixmap/auto", s.withAuth(s.handleMixmapAuto))
	mux.HandleFunc("GET /api/mixmap/presets", s.withAuth(s.handleMixmapListPresets))
	mux.HandleFunc("POST /api/mixmap/presets", s.withAuth(s.handleMixmapSavePreset))
	mux.HandleFunc("DELETE /api/mixmap/presets/{name}", s.withAuth(s.handleMixmapDeletePreset))
}

// ---------- 请求体 ----------

type mixmapStartReq struct {
	Maps       []string `json:"maps"`
	PresetName string   `json:"preset_name"`
}

type mixmapAutoReq struct {
	Type string `json:"type"` // official / custom / mixtape
}

type mixmapSavePresetReq struct {
	Name     string   `json:"name"`
	Maps     []string `json:"maps"`
	Gamemode string   `json:"gamemode"`
}

// ---------- helpers ----------

func cleanMapList(maps []string) ([]string, error) {
	out := make([]string, 0, len(maps))
	for _, m := range maps {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		// RCON 序列以空格分隔，地图名本身不能含空白/引号
		for _, r := range m {
			if unicode.IsSpace(r) || r == '"' || r == '\'' {
				return nil, fmt.Errorf("非法地图名: %s", m)
			}
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("maps 不能为空")
	}
	if len(out) > store.MaxPresetMaps {
		return nil, fmt.Errorf("图池地图不能超过 %d 张", store.MaxPresetMaps)
	}
	return out, nil
}

// prepareMixmapRCON 取服务器信息、可选在线校验、RCON 地址与密码。
func (s *Server) prepareMixmapRCON(w http.ResponseWriter, r *http.Request, sess *session.Session) (addr, password string, ok bool) {
	sd, err := s.Store.Get(sess.ServerKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return "", "", false
	}

	// 与 /api/action 一致：仅当 SteamID 非空时按 SteamID 校验在线
	if sess.SteamID != "" {
		online, err := s.checkPlayerOnline(sd, sess.SteamID, r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "查询在线玩家失败: "+err.Error())
			return "", "", false
		}
		if !online {
			writeError(w, http.StatusForbidden, "你已不在该服务器上，请重新获取验证码")
			return "", "", false
		}
	}

	password, err = s.RconCfg.GetPassword(sess.ServerKey, sd.Port)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "RCON 密码未配置: "+err.Error())
		return "", "", false
	}
	host := s.RconCfg.GetHost(sess.ServerKey, sd.Host)
	addr = fmt.Sprintf("%s:%d", host, sd.Port)
	return addr, password, true
}

func mapMixmapReply(reply string) (ok bool, msg string) {
	switch reply {
	case "pool_vote_started", "auto_vote_started":
		return true, ""
	case "mixmap_disabled":
		return false, "Mixmap 插件已关闭"
	case "mixmap_already_started":
		return false, "已有 Mixmap 图池在进行中"
	case "vote_in_progress":
		return false, "当前有投票正在进行中，请等待结束后再试"
	case "manual_select_in_progress":
		return false, "有玩家正在游戏内手动选图，请稍后再试"
	case "player_not_found":
		return false, "发起者已不在服务器上，请重新获取验证码"
	case "pool_empty":
		return false, "图池为空或所有地图未通过服务器校验"
	case "no_voters":
		return false, "服务器上无可参与投票的玩家"
	case "bad_type":
		return false, "自动组池类型无效（official/custom/mixtape）"
	default:
		// 未知回显：仍返回给前端，便于排查（可能是命令未装/插件未加载）
		if reply == "" {
			return false, "游戏服务器无响应（请确认已加载支持 WebMap 对接的 Mixmap 插件）"
		}
		return false, "未知响应: " + reply
	}
}

// ---------- handlers ----------

// handleMixmapStart POST /api/mixmap/start
func (s *Server) handleMixmapStart(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	if s.MixPresets == nil {
		writeError(w, http.StatusInternalServerError, "mixmap 预设存储未初始化")
		return
	}
	var req mixmapStartReq
	if !readJSON(w, r, &req) {
		return
	}
	maps, err := cleanMapList(req.Maps)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	addr, password, ok := s.prepareMixmapRCON(w, r, sess)
	if !ok {
		return
	}

	presetName := strings.TrimSpace(req.PresetName)
	if presetName == "" {
		presetName = "WebMap"
	}

	// sm_mixmap_load_pool "<player>" "<map1 map2 ...>" "<preset>"
	cmd := fmt.Sprintf(`sm_mixmap_load_pool "%s" "%s" "%s"`,
		escapeRCON(sess.Player),
		escapeRCON(strings.Join(maps, " ")),
		escapeRCON(presetName),
	)

	reply, err := rcon.ExecuteOnce(addr, password, cmd, s.Cfg.RconDuration(), r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "RCON 执行失败: "+err.Error())
		return
	}
	reply = strings.TrimSpace(reply)
	okReply, msg := mapMixmapReply(reply)
	if okReply {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "rcon_reply": reply,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": false, "error": msg, "rcon_reply": reply,
	})
}

// handleMixmapAuto POST /api/mixmap/auto
func (s *Server) handleMixmapAuto(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	var req mixmapAutoReq
	if !readJSON(w, r, &req) {
		return
	}
	t := strings.ToLower(strings.TrimSpace(req.Type))
	switch t {
	case "official", "custom", "mixtape":
	default:
		writeError(w, http.StatusBadRequest, "type 须为 official / custom / mixtape")
		return
	}

	addr, password, ok := s.prepareMixmapRCON(w, r, sess)
	if !ok {
		return
	}

	cmd := fmt.Sprintf(`sm_mixmap_start_auto "%s" "%s"`,
		escapeRCON(sess.Player), escapeRCON(t))

	reply, err := rcon.ExecuteOnce(addr, password, cmd, s.Cfg.RconDuration(), r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "RCON 执行失败: "+err.Error())
		return
	}
	reply = strings.TrimSpace(reply)
	okReply, msg := mapMixmapReply(reply)
	if okReply {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "rcon_reply": reply,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": false, "error": msg, "rcon_reply": reply,
	})
}

// mixmapPresetItem 预设列表条目：store 字段 + 当前请求者的可删标记。
type mixmapPresetItem struct {
	store.MixmapPreset
	CanDelete bool `json:"can_delete"` // 当前登录者是否有权删除该预设
}

// handleMixmapListPresets GET /api/mixmap/presets?q=&page=&page_size=
// 需预设管理权限；返回 {ok, presets, total, page, page_size}。
// 排序：自己创建的排最前；搜索：名称/SteamID 子串；分页默认 10 条/页。
func (s *Server) handleMixmapListPresets(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	if s.MixPresets == nil {
		writeError(w, http.StatusInternalServerError, "mixmap 预设存储未初始化")
		return
	}
	if !s.canManagePresets(sess) {
		writeError(w, http.StatusForbidden, "无权访问预设管理")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 10)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	items, total, err := s.MixPresets.ListFiltered(sess.ServerKey, q, sess.SteamID, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	isSuper := s.Perm != nil && s.Perm.IsSuperAdmin(sess.SteamID)
	out := make([]mixmapPresetItem, 0, len(items))
	for _, p := range items {
		out = append(out, mixmapPresetItem{
			MixmapPreset: p,
			CanDelete:    isSuper || (p.OwnerSteamID != "" && p.OwnerSteamID == sess.SteamID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"presets":   out,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// atoiDefault 解析整数参数，非法或空时返回默认值。
func atoiDefault(v string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

// handleMixmapSavePreset POST /api/mixmap/presets
// 需预设管理权限；按创建者统计配额（max_presets_per_user）。
func (s *Server) handleMixmapSavePreset(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	if s.MixPresets == nil {
		writeError(w, http.StatusInternalServerError, "mixmap 预设存储未初始化")
		return
	}
	if !s.canManagePresets(sess) {
		writeError(w, http.StatusForbidden, "无权管理预设")
		return
	}
	owner := strings.TrimSpace(sess.SteamID)
	if owner == "" {
		writeError(w, http.StatusBadRequest, "无法获取你的 SteamID，请重新获取验证码登录")
		return
	}

	var req mixmapSavePresetReq
	if !readJSON(w, r, &req) {
		return
	}

	maxPresets := 5
	if s.Perm != nil {
		maxPresets = s.Perm.Get().MaxPresetsPerUser
	}
	if s.MixPresets.CountByOwner(sess.ServerKey, owner) >= maxPresets {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("预设数量已达上限（%d 个），请先删除不用的预设", maxPresets))
		return
	}

	// gamemode 仅作提示：优先 body，否则用当前服务器状态
	gm := strings.TrimSpace(req.Gamemode)
	if gm == "" {
		if sd, err := s.Store.Get(sess.ServerKey); err == nil && sd != nil {
			gm = sd.Gamemode
		}
	}
	saved, err := s.MixPresets.Save(sess.ServerKey, store.MixmapPreset{
		Name:         req.Name,
		Maps:         req.Maps,
		Gamemode:     gm,
		OwnerSteamID: owner,
	})
	if err != nil {
		if err == store.ErrPresetInvalidName {
			writeError(w, http.StatusBadRequest, "预设名非法（1-64 字，允许中文/字母/数字/空格/_/-）")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "preset": saved,
	})
}

// handleMixmapDeletePreset DELETE /api/mixmap/presets/{name}
// 仅创建者可删自己的；最高权限者可删任意（含无主旧数据）。
func (s *Server) handleMixmapDeletePreset(w http.ResponseWriter, r *http.Request, sess *session.Session) {
	if s.MixPresets == nil {
		writeError(w, http.StatusInternalServerError, "mixmap 预设存储未初始化")
		return
	}
	if !s.canManagePresets(sess) {
		writeError(w, http.StatusForbidden, "无权管理预设")
		return
	}

	name := r.PathValue("name")
	p, err := s.MixPresets.Get(sess.ServerKey, name)
	if err != nil {
		if err == store.ErrPresetNotFound {
			writeError(w, http.StatusNotFound, "预设不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "预设名非法")
		return
	}

	isSuper := s.Perm != nil && s.Perm.IsSuperAdmin(sess.SteamID)
	if !isSuper && (p.OwnerSteamID == "" || p.OwnerSteamID != sess.SteamID) {
		writeError(w, http.StatusForbidden, "只能删除自己创建的预设")
		return
	}

	if err := s.MixPresets.Delete(sess.ServerKey, name); err != nil {
		if err == store.ErrPresetNotFound {
			writeError(w, http.StatusNotFound, "预设不存在")
			return
		}
		if err == store.ErrPresetInvalidName {
			writeError(w, http.StatusBadRequest, "预设名非法")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

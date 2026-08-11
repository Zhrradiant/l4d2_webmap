package api

import (
	"net/http"
	"net/url"
	"strings"
)

// 标签代理：webmap 前端为纯浏览器页面，若直接 fetch 跨域到 zhrradiant.com，
// 可能触发 CORS 限制。这里由 Go 后端做同源代理转发，绕开浏览器同源策略，
// 契约与桌面端 zhrradiant-srvmap 对齐：
//   GET v2 /maps/tag-defs
//   GET v2 /maps/tags?map_ids=...
//
// 上游数据本体在 l4d2_server_status（表 l4d2_map_tag_defs / l4d2_map_tags）。
const (
	tagDefsUpstream = "https://zhrradiant.com/wp-json/l4d2/v2/maps/tag-defs"
	tagsUpstream    = "https://zhrradiant.com/wp-json/l4d2/v2/maps/tags"
)

// handleGetTagDefs 代理读取标签定义列表（上游 v2）。
func (s *Server) handleGetTagDefs(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, tagDefsUpstream, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "构造请求失败: "+err.Error())
		return
	}
	resp, err := commentHTTPClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "上游请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	proxyResponse(w, resp)
}

// handleGetTags 代理批量读取地图标签（上游 v2）。
// 查询参数 map_ids：逗号分隔的地图 id 列表（建议单次 ≤100，前端自行分片）。
func (s *Server) handleGetTags(w http.ResponseWriter, r *http.Request) {
	mapIDs := strings.TrimSpace(r.URL.Query().Get("map_ids"))
	if mapIDs == "" {
		writeError(w, http.StatusBadRequest, "缺少 map_ids")
		return
	}

	q := url.Values{}
	q.Set("map_ids", mapIDs)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, tagsUpstream+"?"+q.Encode(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "构造请求失败: "+err.Error())
		return
	}
	resp, err := commentHTTPClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "上游请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	proxyResponse(w, resp)
}

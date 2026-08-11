package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 评论代理：webmap 前端为纯浏览器页面，若直接 fetch 跨域到 zhrradiant.com，
// POST + application/json 会触发 CORS 预检，服务端未放行该来源时浏览器会报
// "Failed to fetch"。这里由 Go 后端做同源代理转发，绕开浏览器同源策略，
// 行为与桌面端 zhrradiant-srvmap（Go net/http 直连）保持一致。
//
// 上游端点与桌面端参考实现对齐：读取走 v2，写入走 v1。
const (
	commentUpstreamRead  = "https://zhrradiant.com/wp-json/l4d2/v2/maps/comments"
	commentUpstreamWrite = "https://zhrradiant.com/wp-json/l4d2/v1/maps/comments"
)

// commentHTTPClient 独立于游戏服务器 RCON 逻辑，供评论 / 评分 / 标签上游转发共用。
var commentHTTPClient = &http.Client{Timeout: 15 * time.Second}

// flexInt 兼容 parent_id 为 JSON 数字（0）或字符串（"0"）两种写法，
// 统一归一化为 int，避免前端旧缓存发送字符串导致上游类型校验失败。
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*f = 0
			return nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*f = flexInt(n)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

// commentSubmitReq 前端提交评论的入参（字段与 zhrradiant-srvmap 的 CommentInput 对齐）。
type commentSubmitReq struct {
	ItemID    string  `json:"item_id"`
	Content   string  `json:"content"`
	ParentID  flexInt `json:"parent_id"`
	UserName  string  `json:"user_name"`
	GuestKey  string  `json:"guest_key"`
}

// commentUpstreamBody 转发给上游 v1 的请求体，parent_id 为数字，空昵称/游客标识省略。
type commentUpstreamBody struct {
	ItemID    string `json:"item_id"`
	Content   string `json:"content"`
	ParentID  int    `json:"parent_id"`
	UserName  string `json:"user_name,omitempty"`
	GuestKey  string `json:"guest_key,omitempty"`
}

// handleGetComments 代理读取地图评论列表（上游 v2）。
func (s *Server) handleGetComments(w http.ResponseWriter, r *http.Request) {
	mapID := strings.TrimSpace(r.URL.Query().Get("map_id"))
	if mapID == "" {
		writeError(w, http.StatusBadRequest, "缺少 map_id")
		return
	}
	limit := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limit == "" {
		limit = "50"
	}

	q := url.Values{}
	q.Set("map_id", mapID)
	q.Set("limit", limit)
	// 透传游客标识，供上游计算 my_liked / is_mine
	if gk := strings.TrimSpace(r.URL.Query().Get("guest_key")); gk != "" {
		q.Set("guest_key", gk)
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, commentUpstreamRead+"?"+q.Encode(), nil)
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

// handleSubmitComment 代理提交地图评论（上游 v1）。
func (s *Server) handleSubmitComment(w http.ResponseWriter, r *http.Request) {
	var in commentSubmitReq
	if !readJSON(w, r, &in) {
		return
	}
	in.ItemID = strings.TrimSpace(in.ItemID)
	in.Content = strings.TrimSpace(in.Content)
	if in.ItemID == "" || in.Content == "" {
		writeError(w, http.StatusBadRequest, "缺少 item_id 或 content")
		return
	}

	payload, err := json.Marshal(commentUpstreamBody{
		ItemID:    in.ItemID,
		Content:   in.Content,
		ParentID:  int(in.ParentID),
		UserName:  strings.TrimSpace(in.UserName),
		GuestKey:  strings.TrimSpace(in.GuestKey),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "序列化失败: "+err.Error())
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, commentUpstreamWrite, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "构造请求失败: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := commentHTTPClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "上游请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	proxyResponse(w, resp)
}

// handleToggleCommentLike 代理点赞/取消点赞（上游 v1 /maps/comments/like）。
func (s *Server) handleToggleCommentLike(w http.ResponseWriter, r *http.Request) {
	proxyCommentAction(w, r, "https://zhrradiant.com/wp-json/l4d2/v1/maps/comments/like")
}

// handleDeleteComment 代理删除评论（上游 v1 /maps/comments/delete，仅本人，有子回复时软删除）。
func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	proxyCommentAction(w, r, "https://zhrradiant.com/wp-json/l4d2/v1/maps/comments/delete")
}

// proxyCommentAction 通用评论写操作代理：透传 JSON body 与响应到上游。
func proxyCommentAction(w http.ResponseWriter, r *http.Request, upstream string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstream, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "构造请求失败: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := commentHTTPClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "上游请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	proxyResponse(w, resp)
}

// proxyResponse 将上游响应的状态码与响应体透传给前端，保留 Content-Type。
func proxyResponse(w http.ResponseWriter, resp *http.Response) {
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

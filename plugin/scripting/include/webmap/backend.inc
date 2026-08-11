#if defined _webmap_backend_included
    #endinput
#endif
#define _webmap_backend_included

// ===========================================================================
// REST in Pawn HTTP POST 推送
// ===========================================================================

// PostToBackend 用 REST in Pawn 发送一次 JSON POST：
//   - data 由调用方负责 delete（REST in Pawn 为复制语义，Post 内部会自行序列化）；
//   - HTTPRequest 句柄在 .Post() 完成后由扩展自动释放，无需手动 delete；
//   - .Post() 会自动设置 Content-Type: application/json。
void PostToBackend(const char[] endpoint, JSON data) {
    char url[256], fullUrl[384];
    g_cvBackendURL.GetString(url, sizeof url);

    // 拼接完整 URL
    if (strlen(url) > 0 && url[strlen(url)-1] == '/')
        url[strlen(url)-1] = '\0';
    FormatEx(fullUrl, sizeof fullUrl, "%s%s", url, endpoint);

    HTTPRequest request = new HTTPRequest(fullUrl);

    char secret[128];
    g_cvPushSecret.GetString(secret, sizeof secret);
    // SetHeader 第二参数是格式串：密钥用 "%s" 传入，避免其中的 % 被当成格式符。
    request.SetHeader("X-Api-Key", "%s", secret);

    request.Post(data, OnBackendResponse);
}

// HTTP 请求完成回调：记录结果。REST in Pawn 会在请求结束后自动释放句柄，无需手动 delete。
void OnBackendResponse(HTTPResponse response, any value, const char[] error) {
    // Status 为 Invalid(0) 表示底层网络/传输失败（curl 错误信息在 error 中）。
    if (response.Status == HTTPStatus_Invalid) {
        LogError("[WebMap] 后端推送失败（网络错误: %s，请检查 webmap_backend_url 及网络连通性）", error);
        return;
    }

    if (response.Status != HTTPStatus_OK) {
        LogError("[WebMap] 后端返回非 200 状态码: %d（请检查 webmap_push_secret 是否与后端一致）", view_as<int>(response.Status));
        return;
    }
}

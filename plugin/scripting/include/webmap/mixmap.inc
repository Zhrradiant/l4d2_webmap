#if defined _webmap_mixmap_included
    #endinput
#endif
#define _webmap_mixmap_included

// ===========================================================================
// Mixmap 在线探测（可选增强）
// 用库名 l4d2_mixmap 是否注册判断，不 #include / 不依赖 native。
// ===========================================================================

// ProbeMixmapAvailable 探测同服是否加载了 l4d2_mixmap 插件库。
// 返回 true 表示前端可显示「图池编辑器」入口。
bool ProbeMixmapAvailable() {
    // LibraryExists 在 AskPluginLoad2/OnAllPluginsLoaded 后才可靠；
    // 推送发生在 OnConfigsExecuted/OnMapStart，此时库表已就绪。
    return LibraryExists("l4d2_mixmap");
}

/* ============================================
   L4D2 WebMap 前端逻辑 v2
   地图卡片网格、筛选搜索排序、标签筛选、详情弹窗、章节选择、评论、评分
   ============================================ */

(function () {
  'use strict';

  // ====== 状态 ======
  const state = {
    token: sessionStorage.getItem('wm_token') || '',
    serverKey: sessionStorage.getItem('wm_server_key') || '',
    playerName: sessionStorage.getItem('wm_player') || '',
    serverName: sessionStorage.getItem('wm_server_name') || '',
    previewMode: false, // 预览系统（游客）模式：无 token 只读查看服务器
    previewSearchQuery: '', // 统合搜索关键字（预览首页）
    serverData: null,
    filter: 'all',
    search: '',
    sort: 'default', // default | rating
    pageSize: 50,
    currentPage: 1,
    currentMission: null,
    pendingVote: null,
    eventSource: null,
    pollIntervalId: null,
    sseRetryMs: 1000,
    controller: null,
    _retryTimer: null,
    // 评论（按 identifier 缓存）
    commentsCache: {},
    // 评分（map_id -> { avg, count }），契约对齐 zhrradiant-srvmap
    ratingsMap: {},
    ratingsLoading: false,
    // 标签（契约对齐 zhrradiant-srvmap / l4d2_server_status；展示统一黑白，不消费上游 color）
    tagDefs: [],                 // [{ tag_name, color? }] 仅用 tag_name 做筛选选项
    tagsMap: {},                 // { map_id: string[] }
    tagsLoading: false,
    tagFilterTags: new Set(),    // 选中的标签（多选）
    tagNoMarkFilter: 'show',     // show | exclude：未配置标签是否显示
    tagFilterOpen: false,
    // 背景配置
    bgMode: 'default',
    bgURL: '',
    bgLoaded: false,
    // Mixmap 图池编辑器
    mixmapAvailable: false,
    mixmapOpen: false,
    maxPoolMaps: 50,
    canManagePresets: false,
    mixPresetPage: 1,
    mixPresetQuery: '',
    mixPresetTotal: 0,
    mixPool: [], // [{ map, displayName, mission }]
    mixPresets: [],
    mixDragFrom: -1,
  };

  // ====== DOM ======
  const $ = (id) => document.getElementById(id);
  const dom = {
    loginView: $('loginView'),
    panelView: $('panelView'),
    previewView: $('previewView'),
    serverGrid: $('serverGrid'),
    previewServerCount: $('previewServerCount'),
    previewSearchInput: $('previewSearchInput'),
    vpkDropZone: $('vpkDropZone'),
    vpkFileInput: $('vpkFileInput'),
    vpkResult: $('vpkResult'),
    loginForm: $('loginForm'),
    codeInput: $('codeInput'),
    loginBtn: $('loginBtn'),
    loginTip: $('loginTip'),
    // panel
    serverName: $('serverName'),
    serverDot: $('serverDot'),
    gamemodeBadge: $('gamemodeBadge'),
    currentMap: $('currentMap'),
    currentMapDetailBtn: $('currentMapDetailBtn'),
    playerName: $('playerName'),
    logoutBtn: $('logoutBtn'),
    searchInput: $('searchInput'),
    tagFilterBtn: $('tagFilterBtn'),
    tagFilterDropdown: $('tagFilterDropdown'),
    sortSelect: $('sortSelect'),
    mapGrid: $('mapGrid'),
    // pagination
    prevPageBtn: $('prevPageBtn'),
    nextPageBtn: $('nextPageBtn'),
    pageInfo: $('pageInfo'),
    // detail modal
    detailModal: $('detailModal'),
    detailCloseBtn: $('detailCloseBtn'),
    detailImage: $('detailImage'),
    detailImageFallback: $('detailImageFallback'),
    detailTitle: $('detailTitle'),
    detailSubtitle: $('detailSubtitle'),
    detailTags: $('detailTags'),
    detailIdentifier: $('detailIdentifier'),
    detailCodes: $('detailCodes'),
    chapterSection: $('chapterSection'),
    chapterList: $('chapterList'),
    detailVoteBtn: $('detailVoteBtn'),
    commentList: $('commentList'),
    commentEmpty: $('commentEmpty'),
    commentNick: $('commentNick'),
    commentContent: $('commentContent'),
    commentSubmitBtn: $('commentSubmitBtn'),
    commentStatus: $('commentStatus'),
    // vote modal
    voteModal: $('voteModal'),
    voteTarget: $('voteTarget'),
    voteConfirmBtn: $('voteConfirmBtn'),
    voteCancelBtn: $('voteCancelBtn'),
    // chapter pick modal（卡片「加入图池」章节选择）
    chapterPickModal: $('chapterPickModal'),
    chapterPickCampaign: $('chapterPickCampaign'),
    chapterPickList: $('chapterPickList'),
    chapterPickCancelBtn: $('chapterPickCancelBtn'),
    chapterPickConfirmBtn: $('chapterPickConfirmBtn'),
    // mixmap
    mixmapOpenBtn: $('mixmapOpenBtn'),
    mixmapPanel: $('mixmapPanel'),
    mixmapCloseBtn: $('mixmapCloseBtn'),
    mixPoolCount: $('mixPoolCount'),
    mixPoolList: $('mixPoolList'),
    mixPoolClearBtn: $('mixPoolClearBtn'),
    mixPoolStartBtn: $('mixPoolStartBtn'),
    mixPoolSaveBtn: $('mixPoolSaveBtn'),
    mixSaveRow: $('mixSaveRow'),
    mixPresetNameInput: $('mixPresetNameInput'),
    mixPresetList: $('mixPresetList'),
    mixPresetRefreshBtn: $('mixPresetRefreshBtn'),
    mixPresetSearchInput: $('mixPresetSearchInput'),
    mixPresetSearchBtn: $('mixPresetSearchBtn'),
    mixPresetPagination: $('mixPresetPagination'),
    mixPresetPrevBtn: $('mixPresetPrevBtn'),
    mixPresetNextBtn: $('mixPresetNextBtn'),
    mixPresetPageInfo: $('mixPresetPageInfo'),
    presetPreviewModal: $('presetPreviewModal'),
    presetPreviewTitle: $('presetPreviewTitle'),
    presetPreviewMeta: $('presetPreviewMeta'),
    presetPreviewList: $('presetPreviewList'),
    presetPreviewCloseBtn: $('presetPreviewCloseBtn'),
    mixTabManual: $('mixTabManual'),
    mixTabAuto: $('mixTabAuto'),
    mixTabPreset: $('mixTabPreset'),
    detailAddPoolBtn: $('detailAddPoolBtn'),
    // toast
    toastContainer: $('toastContainer'),
  };

  // ====== API 封装 ======
  async function api(path, opts) {
    opts = opts || {};
    const headers = Object.assign({}, opts.headers || {});
    if (opts.json !== undefined) {
      headers['Content-Type'] = 'application/json';
    }
    if (state.token) {
      headers['Authorization'] = 'Bearer ' + state.token;
    }
    const resp = await fetch(path, {
      method: opts.method || 'GET',
      headers: headers,
      body: opts.json !== undefined ? JSON.stringify(opts.json) : undefined,
      signal: opts.signal || undefined,
    });
    const text = await resp.text();
    let data;
    try { data = text ? JSON.parse(text) : {}; } catch (e) { data = { raw: text }; }
    if (!resp.ok) {
      const err = new Error(data.error || ('HTTP ' + resp.status));
      err.status = resp.status;
      throw err;
    }
    return data;
  }

  // ====== Toast ======
  const MAX_TOASTS = 3;

  // ====== 背景图加载 ======
  const mainBgEl = document.getElementById('mainBg');
  const mainBgImg = document.getElementById('mainBgImg');

  // fetchBgConfig 从后端获取背景配置。
  async function fetchBgConfig() {
    try {
      const resp = await fetch('/api/config');
      if (!resp.ok) return null;
      return await resp.json();
    } catch (e) {
      return null;
    }
  }

  // applyBg 根据配置设置背景图并预加载。
  function applyBg(bgMode, bgURL) {
    state.bgMode = bgMode || 'none';
    state.bgURL = bgURL || '';

    if (state.bgMode === 'none' || !state.bgURL) {
      // 无背景
      mainBgEl.classList.add('main-bg--hidden');
      state.bgLoaded = true;
      return;
    }

    // 预加载图片，加载成功后再显示背景（避免闪烁）
    const img = new Image();
    img.onload = function () {
      mainBgImg.src = state.bgURL;
      mainBgEl.classList.remove('main-bg--hidden');
      state.bgLoaded = true;
    };
    img.onerror = function () {
      // 图片加载失败，隐藏背景层
      mainBgEl.classList.add('main-bg--hidden');
      state.bgLoaded = true;
    };
    // 超时保护（10 秒）
    setTimeout(function () {
      if (!state.bgLoaded) {
        mainBgEl.classList.add('main-bg--hidden');
        state.bgLoaded = true;
      }
    }, 10000);
    img.src = state.bgURL;
  }

  // initBg 初始化背景（页面加载时调用）。
  async function initBg() {
    const cfg = await fetchBgConfig();
    if (cfg) {
      applyBg(cfg.bg_mode, cfg.bg_url);
    } else {
      // 请求失败时默认无背景
      mainBgEl.classList.add('main-bg--hidden');
      state.bgLoaded = true;
    }
  }
  function toast(msg, type) {
    type = type || 'info';
    var toasts = dom.toastContainer.querySelectorAll('.toast');
    while (toasts.length >= MAX_TOASTS) { toasts[0].remove(); toasts = dom.toastContainer.querySelectorAll('.toast'); }
    const el = document.createElement('div');
    el.className = 'toast ' + type;
    el.innerHTML = '<span>' + escapeHtml(msg) + '</span>';
    dom.toastContainer.appendChild(el);
    setTimeout(function () {
      el.style.transition = 'opacity .3s, transform .3s';
      el.style.opacity = '0';
      el.style.transform = 'translateX(40px)';
      setTimeout(function () { el.remove(); }, 300);
    }, 3500);
  }

  function escapeHtml(s) {
    return String(s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  // ====== 视图切换 ======
  // showLogin 验证码页（雪藏）：仅在 ?code= 自动登录失败时出现，供手动输入重试。
  function showLogin() {
    dom.loginView.classList.remove('hidden');
    dom.panelView.classList.add('hidden');
    dom.previewView.classList.add('hidden');
    // 从 URL 参数 ?code=XXX 自动填入验证码
    var params = new URLSearchParams(window.location.search);
    var codeFromUrl = params.get('code') || '';
    dom.codeInput.value = codeFromUrl.toUpperCase();
    dom.codeInput.focus();
  }

  function showPanel() {
    state.previewMode = false;
    dom.loginView.classList.add('hidden');
    dom.previewView.classList.add('hidden');
    dom.panelView.classList.remove('hidden');
    dom.playerName.textContent = state.playerName || '—';
    updatePresetAccess();
    loadServerState(false);
    connectSSE();
  }

  // ====== 第三方地图预览（游客模式） ======
  // showPreview 显示服务器列表（无 token 直接访问首页 / 退出登录 / 退出预览服务器）。
  function showPreview() {
    state.previewMode = false;
    stopPolling();
    if (state.controller) { state.controller.abort(); state.controller = null; }
    // 回到列表时清空统合搜索框与结果，展示全量服务器
    state.previewSearchQuery = '';
    if (dom.previewSearchInput) dom.previewSearchInput.value = '';
    dom.loginView.classList.add('hidden');
    dom.panelView.classList.add('hidden');
    dom.previewView.classList.remove('hidden');
    loadServerList();
  }

  // loadServerList 拉取所有已上报的服务器摘要并渲染。
  async function loadServerList() {
    if (!dom.serverGrid) return;
    dom.serverGrid.innerHTML = '<div class="empty-state"><span class="loading-spinner"></span> 加载服务器列表…</div>';
    if (dom.previewServerCount) dom.previewServerCount.textContent = '';
    try {
      const resp = await fetch('/api/servers');
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
      const list = await resp.json();
      renderServerList(list);
    } catch (err) {
      dom.serverGrid.innerHTML = '<div class="empty-state">加载服务器列表失败：' + escapeHtml(err.message) + '</div>';
    }
  }

  // renderServerList 渲染服务器卡片：只展示在线服务器，离线（含僵尸记录）不再显示。
  function renderServerList(list) {
    if (!dom.serverGrid) return;
    // 只保留在线服务器
    list = (Array.isArray(list) ? list : []).filter(function (s) { return !!s.online; });
    if (dom.previewServerCount) {
      dom.previewServerCount.textContent = '共 ' + list.length + ' 台在线服务器';
    }
    if (!list.length) {
      dom.serverGrid.innerHTML = '<div class="empty-state">暂无在线服务器</div>';
      return;
    }
    dom.serverGrid.innerHTML = list.map(function (s) {
      var name = s.name || s.server_key || '(未命名)';
      var modeHtml = s.gamemode
        ? '<span class="badge">' + escapeHtml(s.gamemode) + '</span>'
        : '';
      // 当前地图优先显示战役名（后端 current_map_name），无则回退章节 map
      var curMap = s.current_map_name || s.current_map || '—';
      return '' +
        '<div class="server-card" data-server-key="' + escapeHtml(s.server_key) + '">' +
          '<div class="server-card-name-row">' +
            '<span class="server-status-dot"></span>' +
            '<span class="server-card-name">' + escapeHtml(name) + '</span>' +
          '</div>' +
          '<div class="server-card-row">' +
            '<span class="server-card-map">当前地图：' + escapeHtml(curMap) + '</span>' +
            modeHtml +
          '</div>' +
        '</div>';
    }).join('');
  }

  // ====== 统合搜索（跨全服检索哪些服务器有此地图） ======
  var previewSearchTimer = null;
  if (dom.previewSearchInput) {
    dom.previewSearchInput.addEventListener('input', function () {
      clearTimeout(previewSearchTimer);
      previewSearchTimer = setTimeout(function () {
        var q = dom.previewSearchInput.value.trim();
        if (!q) {
          state.previewSearchQuery = '';
          loadServerList();
          return;
        }
        loadSearchResults(q);
      }, 250);
    });
  }

  // loadSearchResults 调用统合搜索接口并渲染命中服务器。
  async function loadSearchResults(q) {
    if (!dom.serverGrid) return;
    state.previewSearchQuery = q;
    dom.serverGrid.innerHTML = '<div class="empty-state"><span class="loading-spinner"></span> 搜索中…</div>';
    if (dom.previewServerCount) dom.previewServerCount.textContent = '';
    try {
      const resp = await fetch('/api/search?q=' + encodeURIComponent(q));
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
      const data = await resp.json();
      renderSearchResults((data && data.results) || []);
    } catch (err) {
      dom.serverGrid.innerHTML = '<div class="empty-state">搜索失败：' + escapeHtml(err.message) + '</div>';
    }
  }

  // renderSearchResults 渲染命中服务器卡片：只展示在线服务器。
  function renderSearchResults(results) {
    if (!dom.serverGrid) return;
    // 只保留在线服务器
    results = (Array.isArray(results) ? results : []).filter(function (s) { return !!s.online; });
    if (dom.previewServerCount) {
      dom.previewServerCount.textContent = '搜索「' + state.previewSearchQuery + '」：' + results.length + ' 台在线服务器命中';
    }
    if (!results.length) {
      dom.serverGrid.innerHTML = '<div class="empty-state">没有在线服务器包含匹配的地图</div>';
      return;
    }
    dom.serverGrid.innerHTML = results.map(function (s) {
      var name = s.name || s.server_key || '(未命名)';
      var modeHtml = s.gamemode
        ? '<span class="badge">' + escapeHtml(s.gamemode) + '</span>'
        : '';
      // 命中地图按战役去重，取展示名（在线中文名优先）
      var seen = {};
      var matchNames = [];
      (s.matches || []).forEach(function (m) {
        if (seen[m.mission]) return;
        seen[m.mission] = true;
        var mn = (m.online && m.online.chinese_name) || m.mission_display_chi || m.mission_display_en || m.mission;
        if (mn) matchNames.push(mn);
      });
      var matchesHtml = matchNames.length
        ? '<div class="server-card-matches">' +
          matchNames.slice(0, 4).map(function (n) {
            return '<span class="map-tag-badge">' + escapeHtml(n) + '</span>';
          }).join('') +
          (matchNames.length > 4
            ? '<span class="server-card-match-more">+' + (matchNames.length - 4) + '</span>'
            : '') +
          '</div>'
        : '';
      var curMap = s.current_map_name || s.current_map || '—';
      return '' +
        '<div class="server-card" data-server-key="' + escapeHtml(s.server_key) + '">' +
          '<div class="server-card-name-row">' +
            '<span class="server-status-dot"></span>' +
            '<span class="server-card-name">' + escapeHtml(name) + '</span>' +
          '</div>' +
          matchesHtml +
          '<div class="server-card-row">' +
            '<span class="server-card-map">当前地图：' + escapeHtml(curMap) + '</span>' +
            modeHtml +
          '</div>' +
        '</div>';
    }).join('');
  }

  // ====== VPK 拖拽比对（纯前端解析，不上传） ======
  // 目录树解析参考根目录 vpk-unpacker.js 的 parseVPK；此处仅提取 missions/*.txt，
  // 不将全部文件解出为 Blob，减少内存占用。

  function readNullString(dataView, offset) {
    var result = '';
    var charCode;
    while ((charCode = dataView.getUint8(offset)) !== 0) {
      result += String.fromCharCode(charCode);
      offset++;
    }
    return result;
  }

  // parseVPKMissions 解析 VPK 目录，返回 missions/ 下 txt 的 { path, content }。
  function parseVPKMissions(buffer) {
    var dataView = new DataView(buffer);
    var offset = 0;
    var signature = dataView.getUint32(offset, true); offset += 4;
    var version = dataView.getUint32(offset, true); offset += 4;
    var directorySize = dataView.getUint32(offset, true); offset += 4;
    if (signature !== 0x55AA1234) throw new Error('无效的 VPK 文件签名');
    if (version !== 1 && version !== 2) throw new Error('不支持的 VPK 版本: ' + version);
    if (version === 2) offset += 20; // v2 附加头
    var dataSectionStart = offset + directorySize;
    var directoryEnd = offset + directorySize;
    var missions = [];

    while (offset < directoryEnd) {
      var ext = readNullString(dataView, offset); offset += ext.length + 1;
      if (ext === '') break;
      while (offset < directoryEnd) {
        var dir = readNullString(dataView, offset); offset += dir.length + 1;
        if (dir === '') break;
        while (offset < directoryEnd) {
          var name = readNullString(dataView, offset); offset += name.length + 1;
          if (name === '') break;
          offset += 4; // crc32
          var preloadBytes = dataView.getUint16(offset, true); offset += 2;
          var archiveIndex = dataView.getUint16(offset, true); offset += 2;
          var entryOffset = dataView.getUint32(offset, true); offset += 4;
          var entryLength = dataView.getUint32(offset, true); offset += 4;
          var terminator = dataView.getUint16(offset, true); offset += 2;
          if (terminator !== 0xFFFF) throw new Error('VPK 文件条目终止符错误');
          var preload = preloadBytes > 0 ? new Uint8Array(buffer, offset, preloadBytes) : new Uint8Array(0);
          if (preloadBytes > 0) offset += preloadBytes;

          var extNorm = ext === ' ' ? '' : ext;
          var dirNorm = dir === ' ' ? '' : dir;
          var fileName = name + (extNorm ? '.' + extNorm : '');
          var fullPath = dirNorm ? dirNorm + '/' + fileName : fileName;
          var lower = fullPath.toLowerCase();
          if (lower.startsWith('missions/') && lower.endsWith('.txt')) {
            var contentBytes = null;
            if (archiveIndex === 0x7FFF) {
              // 数据内嵌于本 vpk：preload + archive 段拼接
              var data = new Uint8Array(buffer, dataSectionStart + entryOffset, entryLength);
              contentBytes = new Uint8Array(preloadBytes + entryLength);
              contentBytes.set(preload, 0);
              contentBytes.set(data, preloadBytes);
            } else if (preloadBytes > 0) {
              // 分卷文件中仅 preload 内嵌
              contentBytes = preload;
            }
            if (contentBytes) {
              missions.push({ path: fullPath, content: new TextDecoder('utf-8').decode(contentBytes) });
            }
          }
        }
      }
    }
    return missions;
  }

  // extractMissionMaps 从 mission txt 内容提取所有 "Map" 字段值（去重，保留顺序）。
  function extractMissionMaps(content) {
    var maps = [];
    var re = /"Map"\s*"([^"]+)"/gi;
    var m;
    while ((m = re.exec(content)) !== null) {
      if (maps.indexOf(m[1]) === -1) maps.push(m[1]);
    }
    return maps;
  }

  // compareVpkMap 对单个 Map 值做统合搜索，前端过滤「完全命中」（mission 或 chapter_map 完全相等）。
  async function compareVpkMap(mapVal) {
    var resp = await fetch('/api/search?q=' + encodeURIComponent(mapVal));
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    var data = await resp.json();
    var hits = [];
    (data.results || []).forEach(function (s) {
      // 只统计在线服务器（离线/僵尸记录不再展示）
      if (!s.online) return;
      var matched = (s.matches || []).filter(function (m) {
        return m.mission === mapVal || m.chapter_map === mapVal;
      });
      if (matched.length) {
        hits.push({ serverKey: s.server_key, serverName: s.name || s.server_key, matched: matched });
      }
    });
    return hits;
  }

  // handleVpkFile 处理单个 vpk：解析 → 提取 Map → 逐值比对 → 渲染。
  async function handleVpkFile(file) {
    var ext = (String(file.name).split('.').pop() || '').toLowerCase();
    if (ext !== 'vpk') {
      renderVpkResult('<div class="vpk-result-error">只支持 .vpk 文件</div>');
      return;
    }
    renderVpkResult('<div class="vpk-result-empty"><span class="loading-spinner"></span> 正在本地解析 ' + escapeHtml(file.name) + '…</div>');
    try {
      var buffer = await file.arrayBuffer();
      var missions = parseVPKMissions(buffer);
      if (!missions.length) {
        renderVpkResult('<div class="vpk-result-empty">VPK 内未找到 missions 文件夹下的 txt 文件</div>');
        return;
      }
      var html = '';
      for (var i = 0; i < missions.length; i++) {
        var ms = missions[i];
        var maps = extractMissionMaps(ms.content);
        if (!maps.length) {
          html += '<div class="vpk-result-item"><div class="vpk-file-path">' + escapeHtml(ms.path) + '</div>' +
            '<div class="vpk-map-row"><span class="vpk-map-status miss">未找到 Map 字段</span></div></div>';
          continue;
        }
        for (var j = 0; j < maps.length; j++) {
          html += '<div class="vpk-result-item" data-map="' + escapeHtml(maps[j]) + '">' +
            '<div class="vpk-file-path">' + escapeHtml(ms.path) + '</div>' +
            '<div class="vpk-map-row">' +
              '<span class="vpk-map-code">' + escapeHtml(maps[j]) + '</span>' +
              '<span class="vpk-map-status"><span class="loading-spinner"></span> 比对中…</span>' +
            '</div></div>';
        }
      }
      renderVpkResult(html);
      // 逐值并发比对
      var items = Array.prototype.slice.call(dom.vpkResult.querySelectorAll('.vpk-result-item[data-map]'));
      await Promise.all(items.map(function (item) {
        return (async function () {
          var mapVal = item.getAttribute('data-map');
          var statusEl = item.querySelector('.vpk-map-status');
          try {
            var hits = await compareVpkMap(mapVal);
            if (hits.length) {
              statusEl.className = 'vpk-map-status hit';
              statusEl.textContent = '完全命中 ' + hits.length + ' 台服务器';
              var hostsRow = document.createElement('div');
              hostsRow.className = 'vpk-map-hosts';
              hostsRow.innerHTML = hits.map(function (h) {
                return '<span class="vpk-map-host" data-server-key="' + escapeHtml(h.serverKey) + '">' + escapeHtml(h.serverName) + '</span>';
              }).join('');
              item.appendChild(hostsRow);
            } else {
              statusEl.className = 'vpk-map-status miss';
              statusEl.textContent = '无完全命中';
            }
          } catch (err) {
            statusEl.className = 'vpk-map-status miss';
            statusEl.textContent = '比对失败';
          }
        })();
      }));
    } catch (err) {
      renderVpkResult('<div class="vpk-result-error">解析失败：' + escapeHtml(err.message) + '</div>');
    }
  }

  function renderVpkResult(html) {
    if (!dom.vpkResult) return;
    dom.vpkResult.innerHTML = html;
    dom.vpkResult.classList.remove('hidden');
  }

  // 命中的服务器名可点击进入该服务器
  if (dom.vpkResult) {
    dom.vpkResult.addEventListener('click', function (e) {
      var host = e.target.closest('.vpk-map-host');
      if (host && host.dataset.serverKey) enterPreviewServer(host.dataset.serverKey);
    });
  }

  // 拖拽 / 点击选择文件
  if (dom.vpkDropZone && dom.vpkFileInput) {
    dom.vpkDropZone.addEventListener('click', function () { dom.vpkFileInput.click(); });
    dom.vpkDropZone.addEventListener('dragover', function (e) {
      e.preventDefault();
      dom.vpkDropZone.classList.add('dragover');
    });
    dom.vpkDropZone.addEventListener('dragleave', function () {
      dom.vpkDropZone.classList.remove('dragover');
    });
    dom.vpkDropZone.addEventListener('drop', function (e) {
      e.preventDefault();
      dom.vpkDropZone.classList.remove('dragover');
      var files = Array.from(e.dataTransfer.files || []);
      if (files.length) handleVpkFile(files[0]);
    });
    dom.vpkFileInput.addEventListener('change', function () {
      if (dom.vpkFileInput.files && dom.vpkFileInput.files.length) {
        handleVpkFile(dom.vpkFileInput.files[0]);
      }
      dom.vpkFileInput.value = '';
    });
  }

  // 点击服务器卡片 → 以游客身份进入该服务器页面
  if (dom.serverGrid) {
    dom.serverGrid.addEventListener('click', function (e) {
      var card = e.target.closest('.server-card');
      if (!card || !card.dataset.serverKey) return;
      enterPreviewServer(card.dataset.serverKey);
    });
  }

  // 进入预览服务器视图（状态 + 视图切换，不含 history 操作；供点击与前进键共用）
  function showPreviewServer(key) {
    state.previewMode = true;
    state.token = '';
    state.serverKey = key;
    state.playerName = 'Guest';
    state.serverName = '';
    state.filter = 'all';
    state.search = '';
    state.currentPage = 1;
    state.mixPool = [];
    state.commentsCache = {};
    state.tagFilterTags = new Set();
    state.tagNoMarkFilter = 'show';
    state.mixmapAvailable = false; // 预览模式一律无图池编辑器（renderServer 亦会强制）
    state.mixmapOpen = false;
    if (dom.mixmapOpenBtn) dom.mixmapOpenBtn.classList.add('hidden');
    if (dom.mixmapPanel) dom.mixmapPanel.classList.add('hidden');
    if (dom.searchInput) dom.searchInput.value = '';
    document.querySelectorAll('.filter-btn[data-filter]').forEach(function (b) {
      b.classList.toggle('active', b.dataset.filter === 'all');
    });
    if (dom.sortSelect) dom.sortSelect.value = 'default';
    dom.loginView.classList.add('hidden');
    dom.previewView.classList.add('hidden');
    dom.panelView.classList.remove('hidden');
    dom.playerName.textContent = 'Guest';
    loadServerState(false);
    startPolling(); // 预览模式无 token，用轮询替代 SSE
  }

  // enterPreviewServer 从预览列表点击进入：切换视图并记录历史，支持浏览器返回键回列表。
  // 注意 URL 必须带 ?server= 参数与列表页区分，否则相邻历史 URL 相同，返回/前进行为会异常。
  function enterPreviewServer(key) {
    showPreviewServer(key);
    if (window.history && window.history.pushState) {
      window.history.pushState(
        { wmPreviewServer: true, wmPreviewKey: key },
        '',
        window.location.pathname + '?server=' + encodeURIComponent(key)
      );
    }
  }

  // exitPreviewServerState 清理预览服务器状态并回到列表视图（不含 history 操作）。
  function exitPreviewServerState() {
    state.previewMode = false;
    state.serverKey = '';
    state.serverData = null;
    stopPolling();
    if (state.controller) { state.controller.abort(); state.controller = null; }
    closeDetailModal();
    showPreview();
  }

  // exitPreviewServer 退出按钮：清理并复位 history 栈（back 触发 popstate 到达列表记录，不再二次处理）。
  function exitPreviewServer() {
    exitPreviewServerState();
    if (window.history && window.history.state && window.history.state.wmPreviewServer) {
      window.history.back();
    }
  }

  // 浏览器返回/前进：按「到达的记录」决定显示列表还是服务器视图，双向都处理。
  window.addEventListener('popstate', function (e) {
    var st = e.state;
    if (st && st.wmPreviewServer) {
      // 到达服务器视图记录（前进；或刷新后回退再前进）。
      // 仅无登录态时进入预览视图，避免已登录用户被「前进」降级为游客。
      if (!state.previewMode && !state.token) showPreviewServer(st.wmPreviewKey);
    } else {
      // 到达列表记录（返回）
      if (state.previewMode) exitPreviewServerState();
    }
  });

  // ====== 登录 ======
  dom.loginForm.addEventListener('submit', async function (e) {
    e.preventDefault();
    const code = dom.codeInput.value.trim().toUpperCase();
    if (!code) { dom.loginTip.textContent = '请输入验证码'; return; }
    dom.loginBtn.disabled = true;
    dom.loginBtn.textContent = '验证中…';
    dom.loginTip.textContent = '';
    try {
      const data = await api('/api/login', { method: 'POST', json: { code: code } });
      if (!data.ok) throw new Error('登录失败');
      state.token = data.token;
      state.serverKey = data.server_key;
      state.playerName = data.player;
      state.serverName = data.server_name || data.server_key;
      state.canManagePresets = !!data.can_manage_presets;
      sessionStorage.setItem('wm_token', state.token);
      sessionStorage.setItem('wm_server_key', state.serverKey);
      sessionStorage.setItem('wm_player', state.playerName);
      sessionStorage.setItem('wm_server_name', state.serverName);
      toast('登录成功，欢迎 ' + state.playerName, 'success');
      showPanel();
    } catch (err) {
      dom.loginTip.textContent = err.message || '验证码无效或已过期';
    } finally {
      dom.loginBtn.disabled = false;
      dom.loginBtn.textContent = '进入';
    }
  });

  dom.codeInput.addEventListener('input', function () {
    dom.codeInput.value = dom.codeInput.value.toUpperCase();
  });

  // ====== 退出 / 会话过期 ======
  // doLogout 清理所有登录态并回到预览系统（服务器列表），可选 toast 提示。
  // 供「退出」按钮和「token 失效」检测共用，避免失效标签页持续空转重连。
  function doLogout(msg) {
    state.token = '';
    state.serverKey = '';
    state.serverData = null;
    state.commentsCache = {};
    state.ratingsMap = {};
    state.ratingsLoading = false;
    state.tagDefs = [];
    state.tagsMap = {};
    state.tagsLoading = false;
    state.tagFilterTags = new Set();
    state.tagNoMarkFilter = 'show';
    state.tagFilterOpen = false;
    state.sort = 'default';
    state.mixmapAvailable = false;
    state.mixmapOpen = false;
    state.canManagePresets = false;
    state.mixPresetPage = 1;
    state.mixPresetQuery = '';
    state.mixPresetTotal = 0;
    state.mixPool = [];
    state.mixPresets = [];
    state.mixDragFrom = -1;
    if (dom.sortSelect) dom.sortSelect.value = 'default';
    if (dom.mixmapPanel) dom.mixmapPanel.classList.add('hidden');
    if (dom.mixmapOpenBtn) dom.mixmapOpenBtn.classList.add('hidden');
    closeTagFilterDropdown();
    updateTagFilterBtn();
    state.sseRetryMs = 1000;
    if (state.controller) { state.controller.abort(); state.controller = null; }
    stopPolling();
    if (state._retryTimer) { clearTimeout(state._retryTimer); state._retryTimer = null; }
    sessionStorage.clear();
    if (state.eventSource) { state.eventSource.close(); state.eventSource = null; }
    // 验证码页已雪藏：退出登录后进入预览系统（服务器列表）
    showPreview();
    if (msg) toast(msg, 'info');
  }

  dom.logoutBtn.addEventListener('click', function () {
    // 预览（游客）模式：退出当前服务器页面；登录模式：登出
    if (state.previewMode) exitPreviewServer();
    else doLogout('已退出');
  });

  // 当前地图详情按钮
  dom.currentMapDetailBtn.addEventListener('click', function () {
    if (state.currentMission) openDetailModal(state.currentMission);
  });

  // ====== 加载服务器状态 ======
  async function loadServerState(silent) {
    if (state.controller) { state.controller.abort(); }
    state.controller = new AbortController();
    if (!silent) {
      dom.mapGrid.innerHTML = '<div class="empty-state"><span class="loading-spinner"></span> 加载地图列表…</div>';
    }
    try {
      const data = await api('/api/server/' + encodeURIComponent(state.serverKey) + '/state', { signal: state.controller.signal });
      state.serverData = data;
      renderServer();
      renderAll();
      // 列表先出，评分 / 标签异步补齐；筛选或排序变更时在各自 load 末尾再 renderAll。
      loadAllRatings();
      loadTagDefs();
      loadAllTags();
    } catch (err) {
      if (err.name === 'AbortError') return;
      // token 失效（401）：停止轮询/重连，回到预览系统，避免失效标签页持续空转
      if (err.status === 401) { doLogout('登录已过期，请重新登录'); return; }
      if (!silent) {
        dom.mapGrid.innerHTML = '<div class="empty-state">加载失败：' + escapeHtml(err.message) + '</div>';
      }
    }
  }

  function renderServer() {
    const d = state.serverData;
    if (!d) return;
    // mixmap 面板入口：无探测字段时默认隐藏（仅当明确 true 时显示）；
    // 预览（游客）模式一律不提供图池编辑器。
    state.mixmapAvailable = state.previewMode ? false : !!d.mixmap_available;
    updateMixmapEntry();
    dom.serverName.textContent = d.name || state.serverName || d.server_key;
    state.serverName = d.name || state.serverName;
    dom.gamemodeBadge.textContent = d.gamemode || '—';
    dom.currentMap.textContent = formatCurrentMap(d.current_map);
    // 查找当前地图所属战役
    state.currentMission = null;
    var camps = getCampaigns();
    for (var i = 0; i < camps.length; i++) {
      if (camps[i].chapters.some(function (ch) { return ch.chapter_map === d.current_map; })) {
        state.currentMission = camps[i].mission;
        break;
      }
    }
    dom.serverDot.classList.toggle('offline', !d.online);
  }

  // 当前地图显示为"战役名 [章节map]"，战役名取与卡片副标题(map-card-sub)相同的数据源：
  // 战役名取与卡片主标题(map-card-name)相同的数据源：中文优先。
  // 匹配到在线数据用 online.chinese_name，否则回退 mission_display_chi → mission_display_en → mission。
  // 未在战役列表中找到该章节、或战役名与章节map相同时，回退为仅显示章节map。
  function formatCurrentMap(currentMap) {
    if (!currentMap) return '—';
    var camps = getCampaigns();
    for (var i = 0; i < camps.length; i++) {
      var c = camps[i];
      var hit = c.chapters.some(function (ch) { return ch.chapter_map === currentMap; });
      if (!hit) continue;
      var subName = c.online ? c.online.chinese_name : (c.rep.mission_display_chi || c.rep.mission_display_en || c.mission);
      if (subName && subName !== currentMap) return subName + ' [' + currentMap + ']';
      return currentMap;
    }
    return currentMap;
  }

  // ====== 筛选 / 搜索 / 排序 ======
  document.querySelectorAll('.filter-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      // 标签按钮（无 data-filter）不参与官方/三方筛选，只开标签下拉
      if (!btn.dataset.filter) return;
      document.querySelectorAll('.filter-btn').forEach(function (b) {
        // 标签按钮无 data-filter：其样式由 #tagFilterBtn 常驻规则固定，不参与 active 切换
        if (!b.dataset.filter) return;
        b.classList.remove('active');
      });
      btn.classList.add('active');
      state.filter = btn.dataset.filter;
      state.currentPage = 1;
      renderAll();
    });
  });

  var searchTimer = null;
  dom.searchInput.addEventListener('input', function () {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(function () {
      state.search = dom.searchInput.value.trim();
      state.currentPage = 1;
      renderAll();
    }, 250);
  });

  if (dom.sortSelect) {
    dom.sortSelect.value = state.sort;
    dom.sortSelect.addEventListener('change', function () {
      state.sort = dom.sortSelect.value || 'default';
      state.currentPage = 1;
      renderAll();
    });
  }

  // ====== 标签筛选入口（工具栏右边缘按钮） ======
  function isTagFilterActive() {
    return state.tagFilterTags.size > 0 || state.tagNoMarkFilter === 'exclude';
  }

  function updateTagFilterBtn() {
    if (!dom.tagFilterBtn) return;
    // 按钮视觉由 #tagFilterBtn 常驻样式固定（等同「全部」按钮），仅维护 aria-expanded
    dom.tagFilterBtn.setAttribute('aria-expanded', state.tagFilterOpen ? 'true' : 'false');
  }

  function closeTagFilterDropdown() {
    state.tagFilterOpen = false;
    if (dom.tagFilterDropdown) dom.tagFilterDropdown.classList.add('hidden');
    updateTagFilterBtn();
  }

  function openTagFilterDropdown() {
    state.tagFilterOpen = true;
    renderTagFilterDropdown();
    if (dom.tagFilterDropdown) dom.tagFilterDropdown.classList.remove('hidden');
    updateTagFilterBtn();
  }

  function renderTagFilterDropdown() {
    if (!dom.tagFilterDropdown) return;
    var defs = state.tagDefs || [];
    var html = '';
    html += '<div class="tag-filter-section-title">筛选标签 (多选)</div>';
    if (!defs.length) {
      html += '<div class="tag-filter-empty">' +
        (state.tagsLoading ? '标签加载中…' : '暂无可用标签') +
        '</div>';
    } else {
      for (var i = 0; i < defs.length; i++) {
        var d = defs[i];
        var name = d.tag_name;
        var checked = state.tagFilterTags.has(name) ? ' checked' : '';
        html += '<label class="tag-filter-item">' +
          '<input type="checkbox" data-tag-name="' + escapeHtml(name) + '"' + checked + '>' +
          '<span>' + escapeHtml(name) + '</span>' +
          '</label>';
      }
    }
    html += '<div class="tag-filter-divider"></div>';
    html += '<div class="tag-filter-section-title">未配置标签</div>';
    html += '<label class="tag-filter-item">' +
      '<input type="radio" name="tagNoMark" value="show"' +
      (state.tagNoMarkFilter === 'show' ? ' checked' : '') + '>' +
      '<span>显示无标签</span></label>';
    html += '<label class="tag-filter-item">' +
      '<input type="radio" name="tagNoMark" value="exclude"' +
      (state.tagNoMarkFilter === 'exclude' ? ' checked' : '') + '>' +
      '<span>排除无标签</span></label>';
    if (isTagFilterActive()) {
      html += '<button type="button" class="tag-filter-clear" id="tagFilterClearBtn">清除筛选</button>';
    }
    dom.tagFilterDropdown.innerHTML = html;
  }

  if (dom.tagFilterBtn) {
    dom.tagFilterBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      if (state.tagFilterOpen) closeTagFilterDropdown();
      else openTagFilterDropdown();
    });
  }

  if (dom.tagFilterDropdown) {
    // 阻止下拉内部点击冒泡到 document 导致立刻关闭
    dom.tagFilterDropdown.addEventListener('click', function (e) {
      e.stopPropagation();
      if (e.target.closest('#tagFilterClearBtn')) {
        state.tagFilterTags = new Set();
        state.tagNoMarkFilter = 'show';
        state.currentPage = 1;
        renderTagFilterDropdown();
        updateTagFilterBtn();
        renderAll();
      }
    });
    // 用 change 处理 checkbox/radio，点击文字（label）也能正确同步
    dom.tagFilterDropdown.addEventListener('change', function (e) {
      var input = e.target;
      if (!input || input.tagName !== 'INPUT') return;
      if (input.type === 'checkbox' && input.dataset.tagName) {
        var tagName = input.dataset.tagName;
        if (input.checked) state.tagFilterTags.add(tagName);
        else state.tagFilterTags.delete(tagName);
        state.currentPage = 1;
        renderTagFilterDropdown();
        updateTagFilterBtn();
        renderAll();
      } else if (input.type === 'radio' && input.name === 'tagNoMark') {
        state.tagNoMarkFilter = input.value === 'exclude' ? 'exclude' : 'show';
        state.currentPage = 1;
        renderTagFilterDropdown();
        updateTagFilterBtn();
        renderAll();
      }
    });
  }

  document.addEventListener('click', function (e) {
    if (!state.tagFilterOpen) return;
    // 标签按钮与下拉在 tag-filter-wrap 内（按钮/下拉自身已 stopPropagation），
    // 点击筛选区（filter-group / tag-filter-wrap）外部才关闭
    var wrap = e.target.closest('.filter-group, .tag-filter-wrap');
    if (!wrap) closeTagFilterDropdown();
  });

  // 初始化按钮态
  updateTagFilterBtn();

  // ====== 分页 ======
  dom.prevPageBtn.addEventListener('click', function () {
    if (state.currentPage > 1) { state.currentPage--; renderAll(); dom.mapGrid.scrollIntoView({ behavior: 'smooth', block: 'start' }); }
  });
  dom.nextPageBtn.addEventListener('click', function () {
    var totalPages = Math.ceil(getFilteredCampaigns().length / state.pageSize) || 1;
    if (state.currentPage < totalPages) { state.currentPage++; renderAll(); dom.mapGrid.scrollIntoView({ behavior: 'smooth', block: 'start' }); }
  });

  // ====== 渲染入口 ======
  function renderAll() {
    // 聚合一次，复用给卡片渲染与分页，避免重复遍历 d.maps。
    var filtered = getFilteredCampaigns();
    renderMaps(filtered);
    renderPagination(filtered);
  }

  // 按 mission 聚合成战役列表。每个战役取一个代表章节（优先 is_first）承载展示信息，
  // 并保留该战役下的全部章节（chapters）供详情弹窗的章节选择区使用。
  function getCampaigns() {
    var d = state.serverData;
    if (!d || !d.maps || d.maps.length === 0) return [];
    var order = [];          // 保持 mission 首次出现顺序
    var byMission = {};       // mission -> campaign
    for (var i = 0; i < d.maps.length; i++) {
      var m = d.maps[i];
      var camp = byMission[m.mission];
      if (!camp) {
        camp = {
          mission: m.mission,
          rep: m,             // 代表章节（默认首个出现的）
          chapters: [],
          official: m.official,
          online: (m.online && m.match_level !== 'none') ? m.online : null,
          matchLevel: m.match_level,
        };
        byMission[m.mission] = camp;
        order.push(m.mission);
      }
      camp.chapters.push(m);
      // 代表章节优先选 is_first 的那一张
      if (m.is_first) camp.rep = m;
      // 只要任一章节匹配到在线数据，就作为战役的 online 展示源
      if (!camp.online && m.online && m.match_level !== 'none') {
        camp.online = m.online;
        camp.matchLevel = m.match_level;
      }
    }
    return order.map(function (name) { return byMission[name]; });
  }

  // 战役评分 id：优先线上库 identifier（与站点 / 桌面端一致），否则回退 mission。
  function campaignRatingId(c) {
    if (c && c.online && c.online.identifier) return c.online.identifier;
    return c ? c.mission : '';
  }

  // 排序用分数：有 count 的 avg 参与；无评分返回 -1（沉底）。
  function ratingSortScore(info) {
    if (!info || !info.count || info.count < 1) return -1;
    var avg = Number(info.avg);
    return isFinite(avg) ? avg : -1;
  }

  // 展示门槛：count >= 1 且 avg > 0 时显示徽章。
  function ratingDisplay(info) {
    if (!info || !info.count || info.count < 1) return null;
    var avg = Number(info.avg);
    if (!isFinite(avg) || avg <= 0) return null;
    return { avg: avg, count: info.count };
  }

  // 批量拉取评分（同源 /api/ratings → 上游 v2）。单次 ≤200，超出分片。
  async function loadAllRatings() {
    if (state.ratingsLoading) return;
    var camps = getCampaigns();
    if (!camps.length) return;

    var ids = [];
    var seen = {};
    for (var i = 0; i < camps.length; i++) {
      var id = campaignRatingId(camps[i]);
      if (!id || seen[id]) continue;
      seen[id] = true;
      ids.push(id);
    }
    if (!ids.length) return;

    // 仅请求尚未缓存的 id，地图集合变化时也能增量补齐。
    var missing = ids.filter(function (id) { return !state.ratingsMap[id]; });
    if (!missing.length) return;

    state.ratingsLoading = true;
    var BATCH = 200;
    try {
      for (var start = 0; start < missing.length; start += BATCH) {
        var part = missing.slice(start, start + BATCH);
        try {
          var resp = await api('/api/ratings?map_ids=' + encodeURIComponent(part.join(',')));
          var ratings = resp && resp.data && resp.data.ratings;
          if (resp && resp.success && ratings) {
            Object.keys(ratings).forEach(function (k) {
              state.ratingsMap[k] = ratings[k];
            });
          }
          // 对上游未返回的 id 记空对象，避免反复请求。
          for (var j = 0; j < part.length; j++) {
            if (!state.ratingsMap[part[j]]) {
              state.ratingsMap[part[j]] = { map_id: part[j], count: 0, avg: 0 };
            }
          }
        } catch (e) {
          // 单批失败不阻断列表；该批保持未缓存，下次可重试。
          console.warn('[webmap] load ratings batch failed', e);
        }
      }
      // 刷新卡片评分徽章；若当前为评分优先则同时更新顺序。
      renderAll();
    } finally {
      state.ratingsLoading = false;
    }
  }

  // 拉取标签定义（同源 /api/tag-defs → 上游 v2）。仅用 tag_name 填充筛选项。
  async function loadTagDefs() {
    try {
      var resp = await api('/api/tag-defs');
      if (resp && resp.success && Array.isArray(resp.data)) {
        state.tagDefs = resp.data;
        if (state.tagFilterOpen) renderTagFilterDropdown();
        updateTagFilterBtn();
      }
    } catch (e) {
      console.warn('[webmap] load tag defs failed', e);
    }
  }

  // 批量拉取地图标签（同源 /api/tags → 上游 v2）。单次 ≤100，对齐桌面端。
  async function loadAllTags() {
    if (state.tagsLoading) return;
    var camps = getCampaigns();
    if (!camps.length) return;

    var ids = [];
    var seen = {};
    for (var i = 0; i < camps.length; i++) {
      var id = campaignRatingId(camps[i]);
      if (!id || seen[id]) continue;
      seen[id] = true;
      ids.push(id);
    }
    if (!ids.length) return;

    // 仅请求尚未缓存的 id；空数组 [] 也算已缓存。
    var missing = ids.filter(function (id) { return !Object.prototype.hasOwnProperty.call(state.tagsMap, id); });
    if (!missing.length) return;

    state.tagsLoading = true;
    if (state.tagFilterOpen) renderTagFilterDropdown();
    var BATCH = 100;
    var changed = false;
    try {
      for (var start = 0; start < missing.length; start += BATCH) {
        var part = missing.slice(start, start + BATCH);
        try {
          var resp = await api('/api/tags?map_ids=' + encodeURIComponent(part.join(',')));
          if (resp && resp.success && resp.data) {
            if (resp.data.tags_map) {
              Object.keys(resp.data.tags_map).forEach(function (k) {
                state.tagsMap[k] = resp.data.tags_map[k] || [];
              });
            }
            // 上游 tag_colors 不消费：webmap 徽章统一黑白样式
          }
          // 上游未返回的 id 记空数组，避免反复请求。
          for (var j = 0; j < part.length; j++) {
            if (!Object.prototype.hasOwnProperty.call(state.tagsMap, part[j])) {
              state.tagsMap[part[j]] = [];
            }
          }
          changed = true;
        } catch (e) {
          console.warn('[webmap] load tags batch failed', e);
        }
      }
      if (changed) {
        if (state.tagFilterOpen) renderTagFilterDropdown();
        renderAll();
      }
    } finally {
      state.tagsLoading = false;
      if (state.tagFilterOpen) renderTagFilterDropdown();
    }
  }

  function getCampaignTags(c) {
    var id = campaignRatingId(c);
    if (!id) return [];
    return state.tagsMap[id] || [];
  }

  function campaignHasAnySelectedTag(c) {
    if (!state.tagFilterTags.size) return false;
    var tags = getCampaignTags(c);
    for (var i = 0; i < tags.length; i++) {
      if (state.tagFilterTags.has(tags[i])) return true;
    }
    return false;
  }

  function renderTagBadgesHtml(tags) {
    if (!tags || !tags.length) return '';
    var html = '';
    for (var i = 0; i < tags.length; i++) {
      // 统一黑白样式，不使用上游彩色（符合 webmap accent 规范）
      html += '<span class="map-tag-badge">' + escapeHtml(tags[i]) + '</span>';
    }
    return html;
  }

  function getFilteredCampaigns() {
    var camps = getCampaigns().filter(function (c) {
      if (state.filter === 'official' && !c.official) return false;
      if (state.filter === 'custom' && c.official) return false;
      if (state.search) {
        var q = state.search.toLowerCase();
        var r = c.rep;
        var hay = (c.mission + ' ' +
          r.mission_display_en + ' ' + r.mission_display_chi + ' ' +
          (c.online ? (c.online.chinese_name + ' ' + c.online.display_name + ' ' + c.online.identifier) : '') + ' ' +
          c.chapters.map(function (ch) { return ch.chapter_map; }).join(' ')).toLowerCase();
        if (hay.indexOf(q) === -1) return false;
      }
      // 标签筛选：对齐 zhrradiant-srvmap MapList
      // - 选中标签：命中任一 或（无标签且显示无标签）
      // - 未选标签但 exclude：仅保留有标签条目
      var tags = getCampaignTags(c);
      var hasTags = tags.length > 0;
      if (state.tagFilterTags.size) {
        if (!hasTags) return state.tagNoMarkFilter === 'show';
        var hit = false;
        for (var ti = 0; ti < tags.length; ti++) {
          if (state.tagFilterTags.has(tags[ti])) { hit = true; break; }
        }
        if (!hit) return false;
      } else if (state.tagNoMarkFilter === 'exclude') {
        if (!hasTags) return false;
      }
      return true;
    });

    // 评分优先：avg 降序；无评分沉底且块内保持原始相对顺序（stable sort 返回 0）；
    // 有评分同分时比票数，再相等则保持原序。
    if (state.sort === 'rating') {
      camps = camps.slice().sort(function (a, b) {
        var ra = state.ratingsMap[campaignRatingId(a)];
        var rb = state.ratingsMap[campaignRatingId(b)];
        var sa = ratingSortScore(ra);
        var sb = ratingSortScore(rb);
        if (sa !== sb) return sb - sa;
        // 双方都无评分：不改相对顺序（即 getCampaigns 的原始顺序）
        if (sa < 0) return 0;
        var ca = (ra && ra.count) || 0;
        var cb = (rb && rb.count) || 0;
        if (ca !== cb) return cb - ca;
        return 0;
      });
    }

    // 有选中标签且显示无标签：命中标签在前，无标签在后；同组保持当前相对顺序。
    if (state.tagFilterTags.size && state.tagNoMarkFilter === 'show') {
      camps = camps.slice().sort(function (a, b) {
        var aM = campaignHasAnySelectedTag(a) ? 0 : 1;
        var bM = campaignHasAnySelectedTag(b) ? 0 : 1;
        return aM - bM;
      });
    }
    return camps;
  }

  function getPaged(list) {
    var start = (state.currentPage - 1) * state.pageSize;
    return list.slice(start, start + state.pageSize);
  }

  // ====== 渲染地图卡片（战役级） ======
  function renderMaps(filtered) {
    var d = state.serverData;
    if (!d || !d.maps || d.maps.length === 0) {
      dom.mapGrid.innerHTML = '<div class="empty-state">暂无地图数据，管理员请运行 sm_webmap_reload</div>';
      dom.prevPageBtn.disabled = true;
      dom.nextPageBtn.disabled = true;
      return;
    }

    var paged = getPaged(filtered);

    if (paged.length === 0) {
      dom.mapGrid.innerHTML = '<div class="empty-state">无匹配地图</div>';
      return;
    }

    var currentMapVal = d.current_map;

    var html = paged.map(function (c) {
      var rep = c.rep;
      var online = c.online;
      var hasOnline = !!online;

      // 战役下任一章节 == 当前地图，则高亮
      var isCurrent = c.chapters.some(function (ch) { return ch.chapter_map === currentMapVal; });

      // 名称：匹配在线数据用中文名，否则用本地战役翻译
      var displayName = hasOnline ? online.chinese_name : (
        rep.mission_display_chi || rep.mission_display_en || c.mission
      );
      var subName = rep.mission_display_chi || rep.mission_display_en
        || (hasOnline ? online.display_name : c.mission);
      var imageUrl = hasOnline ? online.image_url : '';

      // 标签：章节数（移至主标题同行右边缘）
      var chapterTag = c.chapters.length > 1
        ? '<span class="map-card-name-tag">' + c.chapters.length + '章节</span>'
        : '';

      var badgeHtml = '';
      if (isCurrent) {
        badgeHtml += '<span class="map-card-current-badge">当前</span>';
      }

      var ratingInfo = ratingDisplay(state.ratingsMap[campaignRatingId(c)]);
      var ratingHtml = ratingInfo
        ? '<span class="map-card-rating">★ ' + ratingInfo.avg.toFixed(1) +
          '<span class="map-card-rating-count">(' + ratingInfo.count + ')</span></span>'
        : '';

      var mapTags = getCampaignTags(c);
      var tagsBarHtml = mapTags.length
        ? '<div class="map-card-tags">' + renderTagBadgesHtml(mapTags) + '</div>'
        : '';

      var degradedClass = (!hasOnline && !c.official) ? ' degraded' : '';

      var addPoolBtn = (state.mixmapAvailable && state.mixmapOpen)
        ? '<button type="button" class="btn btn-ghost btn-sm map-card-add-pool" data-add-pool="' + escapeHtml(c.mission) + '">加入图池</button>'
        : '';

      return '' +
        '<div class="map-card' + degradedClass + (isCurrent ? ' current' : '') + '"' +
        ' data-mission="' + escapeHtml(c.mission) + '">' +
        '<div class="map-card-image-wrap">' +
          badgeHtml +
          (imageUrl
            ? '<img class="map-card-image" src="' + escapeHtml(imageUrl) + '" alt="" loading="lazy" onerror="this.style.display=\'none\';this.nextElementSibling.style.display=\'flex\';">'
            : '') +
          '<div class="map-card-image-fallback" style="' + (imageUrl ? 'display:none;' : '') + '"><span>无预览</span></div>' +
          tagsBarHtml +
        '</div>' +
        '<div class="map-card-body">' +
          '<div class="map-card-name">' +
            '<span class="map-card-name-text">' + escapeHtml(displayName) + '</span>' +
            chapterTag +
          '</div>' +
          '<div class="map-card-sub-row">' +
            '<div class="map-card-sub">' + escapeHtml(subName) + '</div>' +
            ratingHtml +
          '</div>' +
          addPoolBtn +
        '</div>' +
        '</div>';
    }).join('');

    dom.mapGrid.innerHTML = html;
  }

  function renderPagination(filtered) {
    var totalPages = Math.ceil(filtered.length / state.pageSize) || 1;
    dom.pageInfo.textContent = '第 ' + state.currentPage + ' / ' + totalPages + ' 页（共 ' + filtered.length + ' 个战役）';
    dom.prevPageBtn.disabled = state.currentPage <= 1;
    dom.nextPageBtn.disabled = state.currentPage >= totalPages;
  }

  // ====== 地图卡片点击 → 详情弹窗 / 加入图池 ======
  dom.mapGrid.addEventListener('click', function (e) {
    var addBtn = e.target.closest('[data-add-pool]');
    if (addBtn) {
      e.preventDefault();
      e.stopPropagation();
      openChapterPicker(addBtn.getAttribute('data-add-pool'));
      return;
    }
    var card = e.target.closest('.map-card');
    if (!card) return;
    openDetailModal(card.dataset.mission);
  });

  // ====== 详情弹窗 ======
  var currentDetail = null; // { mission, enrichedMap }

  // 按 mission 找到该战役的代表章节 EnrichedMap（优先 is_first）。
  function findCampaignRep(mission) {
    var d = state.serverData;
    if (!d || !d.maps) return null;
    var rep = null;
    for (var i = 0; i < d.maps.length; i++) {
      var m = d.maps[i];
      if (m.mission !== mission) continue;
      if (!rep) rep = m;
      if (m.is_first) { rep = m; break; }
    }
    return rep;
  }

  function openDetailModal(mission) {
    var em = findCampaignRep(mission);
    if (!em) return;
    currentDetail = { mission: mission, enrichedMap: em };

    var online = em.online;
    var hasOnline = online && em.match_level !== 'none';

    // 图片
    var imgUrl = hasOnline ? online.image_url : '';
    if (imgUrl) {
      dom.detailImage.src = imgUrl;
      dom.detailImage.style.display = '';
      dom.detailImageFallback.style.display = 'none';
    } else {
      dom.detailImage.style.display = 'none';
      dom.detailImageFallback.style.display = 'flex';
    }
    dom.detailImageFallback.textContent = '暂无地图预览图';

    // 标题
    dom.detailTitle.textContent = hasOnline ? online.chinese_name : (em.mission_display_chi || em.mission_display_en || em.mission);
    dom.detailSubtitle.textContent = em.mission_display_chi || em.mission_display_en
      || (hasOnline && online ? online.display_name : (em.mission || em.chapter_map));

    // 标签徽章：叠在预览图右下（key 规则与 campaignRatingId 一致）
    if (dom.detailTags) {
      var tagId = hasOnline && online && online.identifier ? online.identifier : (em.mission || '');
      var tagsHtml = renderTagBadgesHtml(tagId ? (state.tagsMap[tagId] || []) : []);
      dom.detailTags.innerHTML = tagsHtml;
      if (tagsHtml) dom.detailTags.classList.remove('hidden');
      else dom.detailTags.classList.add('hidden');
    }

    // 字段
    // 字段：identifier 是战役级"地图文件识别名"，降级时用 mission 内部名（同为战役级），
    // chapter_map（章节级）留给"换图代码"字段展示
    dom.detailIdentifier.textContent = hasOnline ? online.identifier : (em.mission || em.chapter_map);
    // 换图代码：收集该战役所有章节的本地 chapter_map（与投票实际发出的一致）
    var allCodes = [];
    var d2 = state.serverData;
    if (d2 && d2.maps) {
      for (var j = 0; j < d2.maps.length; j++) {
        if (d2.maps[j].mission === em.mission) {
          var code = d2.maps[j].chapter_map;
          if (code && allCodes.indexOf(code) === -1) allCodes.push(code);
        }
      }
    }
    dom.detailCodes.textContent = allCodes.length ? allCodes.join(', ') : (em.chapter_map || '');

    // 章节选择
    renderChapters(em);

    // 投票按钮
    dom.detailVoteBtn.onclick = function () {
      // 获取选中的章节
      var selected = document.querySelector('.chapter-item.selected');
      var voteMap = selected ? selected.dataset.chapterMap : em.chapter_map;
      var voteMission = em.mission;
      var displayText = dom.detailTitle.textContent + ' · ' + (selected ? selected.querySelector('.chapter-label').textContent : voteMap);
      openVoteModal({ mission: voteMission, map: voteMap, displayText: displayText });
    };

    // 加入图池（仅图池编辑器打开时显示；此时隐藏投票按钮）
    if (dom.detailAddPoolBtn) {
      dom.detailAddPoolBtn.onclick = function () {
        var selected = document.querySelector('.chapter-item.selected');
        var mapCode = selected ? selected.dataset.chapterMap : em.chapter_map;
        var chapLabel = selected && selected.querySelector('.chapter-label')
          ? selected.querySelector('.chapter-label').textContent
          : (em.chapter_chi || em.chapter_en || mapCode);
        var campName = dom.detailTitle.textContent || em.mission;
        if (addMapToPool(mapCode, campName + ' · ' + chapLabel, em.mission))
          toast('已加入图池：' + mapCode, 'success');
      };
    }
    updateDetailButtons();

    // 评论
    if (hasOnline) {
      loadComments(online.identifier);
    } else {
      dom.commentList.innerHTML = '<p class="comment-empty">该地图未匹配在线数据，暂不支持评论</p>';
    }

    dom.detailModal.classList.remove('hidden');
    document.body.style.overflow = 'hidden';
  }

  function closeDetailModal() {
    dom.detailModal.classList.add('hidden');
    document.body.style.overflow = '';
    currentDetail = null;
  }

  // 详情窗口按钮互斥：图池编辑器打开时显示「加入图池」并隐藏「投票换图」；
  // 预览（游客）模式两种操作按钮都不提供（仅保留章节展示与评论）。
  function updateDetailButtons() {
    var showAdd = !state.previewMode && state.mixmapAvailable && state.mixmapOpen;
    if (dom.detailAddPoolBtn) dom.detailAddPoolBtn.classList.toggle('hidden', !showAdd);
    if (dom.detailVoteBtn) dom.detailVoteBtn.classList.toggle('hidden', showAdd || state.previewMode);
  }

  dom.detailCloseBtn.addEventListener('click', closeDetailModal);
  dom.detailModal.addEventListener('click', function (e) {
    if (e.target === dom.detailModal) closeDetailModal();
  });

  // ====== 章节选择 ======
  function renderChapters(em) {
    // 找到同一 mission 的所有章节
    var d = state.serverData;
    if (!d || !d.maps) { dom.chapterSection.classList.add('hidden'); return; }

    var chapters = d.maps.filter(function (m) {
      return m.mission === em.mission;
    });

    if (chapters.length <= 1) {
      dom.chapterSection.classList.add('hidden');
      return;
    }

    dom.chapterSection.classList.remove('hidden');
    dom.chapterList.innerHTML = chapters.map(function (ch, idx) {
      var code = ch.chapter_map;
      var label = ch.chapter_chi || ch.chapter_en || code;
      var isSelected = ch.chapter_map === em.chapter_map;
      var isFirst = ch.is_first;
      return '' +
        '<div class="chapter-item' + (isSelected ? ' selected' : '') + '"' +
        ' data-chapter-map="' + escapeHtml(code) + '">' +
        '<div class="chapter-radio"></div>' +
        '<span class="chapter-label">' + escapeHtml(label) + (isFirst ? ' (起始章节)' : '') + '</span>' +
        '<span class="chapter-code">' + escapeHtml(code) + '</span>' +
        '</div>';
    }).join('');

    // 章节点击切换
    dom.chapterList.querySelectorAll('.chapter-item').forEach(function (item) {
      item.addEventListener('click', function () {
        dom.chapterList.querySelectorAll('.chapter-item').forEach(function (el) { el.classList.remove('selected'); });
        item.classList.add('selected');
      });
    });
  }

  // ====== 评论系统 ======
  // 游客设备标识：本地生成并持久化（与上游 guest_key 机制一致）
  function getGuestKey() {
    try {
      var key = localStorage.getItem('webmapGuestKey') || '';
      if (key) return key;
      var buf = new Uint8Array(12);
      window.crypto.getRandomValues(buf);
      key = 'g' + Array.from(buf, function (b) { return ('0' + b.toString(16)).slice(-2); }).join('').slice(0, 30);
      localStorage.setItem('webmapGuestKey', key);
      return key;
    } catch (e) {
      return '';
    }
  }

  // 内联 SVG 图标（Font Awesome Free 同源，与桌面端 zhrradiant-srvmap 一致）
  var ICON_HEART = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 640"><path d="M305 151.1L320 171.8L335 151.1C360 116.5 400.2 96 442.9 96C516.4 96 576 155.6 576 229.1L576 231.7C576 343.9 436.1 474.2 363.1 529.9C350.7 539.3 335.5 544 320 544C304.5 544 289.2 539.4 276.9 529.9C203.9 474.2 64 343.9 64 231.7L64 229.1C64 155.6 123.6 96 197.1 96C239.8 96 280 116.5 305 151.1z"/></svg>';
  var ICON_XMARK = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 640"><path d="M183.1 137.4C170.6 124.9 150.3 124.9 137.8 137.4C125.3 149.9 125.3 170.2 137.8 182.7L275.2 320L137.9 457.4C125.4 469.9 125.4 490.2 137.9 502.7C150.4 515.2 170.7 515.2 183.2 502.7L320.5 365.3L457.9 502.6C470.4 515.1 490.7 515.1 503.2 502.6C515.7 490.1 515.7 469.8 503.2 457.3L365.8 320L503.1 182.6C515.6 170.1 515.6 149.8 503.1 137.3C490.6 124.8 470.3 124.8 457.8 137.3L320.5 274.7L183.1 137.4z"/></svg>';
  var ICON_REPLY = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 640"><path d="M268.2 82.4C280.2 87.4 288 99 288 112L288 192L400 192C497.2 192 576 270.8 576 368C576 481.3 494.5 531.9 475.8 542.1C473.3 543.5 470.5 544 467.7 544C456.8 544 448 535.1 448 524.3C448 516.8 452.3 509.9 457.8 504.8C467.2 496 480 478.4 480 448.1C480 395.1 437 352.1 384 352.1L288 352.1L288 432.1C288 445 280.2 456.7 268.2 461.7C256.2 466.7 242.5 463.9 233.3 454.8L73.3 294.8C60.8 282.3 60.8 262 73.3 249.5L233.3 89.5C242.5 80.3 256.2 77.6 268.2 82.6z"/></svg>';

  // 回复目标：{ id, name }，null 表示非回复状态
  var commentReplyTo = null;

  async function loadComments(mapId) {
    dom.commentList.innerHTML = '<p class="comment-empty"><span class="loading-spinner"></span> 加载评论…</p>';
    dom.commentEmpty.style.display = 'none';

    // 检查缓存
    if (state.commentsCache[mapId]) {
      renderComments(state.commentsCache[mapId]);
      return;
    }

    try {
      var url = '/api/comments?map_id=' + encodeURIComponent(mapId) + '&limit=50&guest_key=' + encodeURIComponent(getGuestKey());
      var resp = await fetch(url);
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
      var data = await resp.json();
      var comments = (data && data.data && data.data.comments) ? data.data.comments : [];
      state.commentsCache[mapId] = comments;
      renderComments(comments);
    } catch (err) {
      dom.commentList.innerHTML = '<p class="comment-empty">评论暂不可用</p>';
    }
  }

  function renderComments(comments) {
    if (!comments || comments.length === 0) {
      dom.commentList.innerHTML = '<p class="comment-empty">暂无评论，来发表第一条吧</p>';
      return;
    }

    // 区分顶级评论和回复
    var topLevel = [];
    var repliesMap = {}; // parent_id → [comments]
    comments.forEach(function (c) {
      if (c.parent_id && c.parent_id !== '0' && c.parent_id !== 0) {
        if (!repliesMap[c.parent_id]) repliesMap[c.parent_id] = [];
        repliesMap[c.parent_id].push(c);
      } else {
        topLevel.push(c);
      }
    });

    var html = '';
    topLevel.forEach(function (c) {
      html += renderCommentItem(c, 0);
      // BFS 聚合所有后代回复（数据层保留直接父，展示层拍平为一层，不丢孙级）
      var replies = [];
      (function collect(pid) {
        var kids = repliesMap[pid] || [];
        kids.forEach(function (k) {
          replies.push(k);
          collect(k.id);
        });
      })(c.id);
      if (replies.length) {
        // 子回复默认收纳：竖线区域 + 展开/收起切换
        html += '<div class="comment-replies-toggle" data-replies="' + c.id + '">展开 ' + replies.length + ' 条回复</div>';
        html += '<div class="comment-replies hidden" id="replies-' + c.id + '">';
        replies.forEach(function (r) {
          html += renderCommentItem(r, 1);
        });
        html += '</div>';
      }
    });

    dom.commentList.innerHTML = html;

    // 回复切换事件
    dom.commentList.querySelectorAll('.comment-replies-toggle').forEach(function (el) {
      el.addEventListener('click', function () {
        var id = el.dataset.replies;
        var repliesEl = document.getElementById('replies-' + id);
        if (repliesEl) {
          var isHidden = repliesEl.classList.contains('hidden');
          repliesEl.classList.toggle('hidden');
          el.textContent = isHidden ? '收起回复' : ('展开 ' + repliesEl.children.length + ' 条回复');
        }
      });
    });
  }

  function renderCommentItem(c, level) {
    var timeStr = c.created_at || '';
    var avatarChar = (c.user_name || '?').charAt(0).toUpperCase();
    var wrapperClass = level > 0 ? ' comment-reply' : '';
    // 有头像 URL 时用 img，加载失败回退首字母
    var avatarHtml = c.avatar_url
      ? '<img class="comment-avatar" src="' + escapeHtml(c.avatar_url) + '" alt="" loading="lazy" onerror="this.style.display=\'none\';this.nextElementSibling.style.display=\'flex\';">' +
        '<div class="comment-avatar comment-avatar-fallback" style="display:none;">' + escapeHtml(avatarChar) + '</div>'
      : '<div class="comment-avatar">' + escapeHtml(avatarChar) + '</div>';
    // 操作行：时间（左）+ 点赞 + 回复（右）；删除按钮在名字行右边缘，仅本人可见
    var likeBtn = '<button type="button" class="comment-like-btn' + (c.my_liked ? ' liked' : '') + '" data-like="' + c.id + '" title="点赞">' +
      '<span class="comment-like-count">' + (c.like_count || 0) + '</span>' + ICON_HEART + '</button>';
    var replyBtn = '<button type="button" class="comment-reply-btn" data-reply="' + c.id + '" title="回复">' + ICON_REPLY + '</button>';
    var delBtn = (c.is_mine && !c.deleted)
      ? '<button type="button" class="comment-del-btn" data-del="' + c.id + '" title="删除">' + ICON_XMARK + '</button>'
      : '';
    return '' +
      '<div class="comment-item' + wrapperClass + '" data-comment-id="' + c.id + '">' +
        '<div class="comment-item-header">' +
          avatarHtml +
          '<span class="comment-author">' + escapeHtml(c.user_name || '匿名') + '</span>' +
          delBtn +
        '</div>' +
        '<div class="comment-body">' + escapeHtml(c.content || '') + '</div>' +
        '<div class="comment-meta">' +
          '<span class="comment-time">' + escapeHtml(timeStr) + '</span>' +
          likeBtn +
          replyBtn +
        '</div>' +
      '</div>';
  }

  // 点赞 / 回复 / 删除事件（事件委托）
  dom.commentList.addEventListener('click', async function (e) {
    var replyBtn = e.target.closest('.comment-reply-btn');
    if (replyBtn) {
      var replyId = parseInt(replyBtn.dataset.reply, 10);
      if (!replyId) return;
      var itemEl = replyBtn.closest('.comment-item');
      var replyName = itemEl ? (itemEl.querySelector('.comment-author') || {}).textContent : '';
      commentReplyTo = { id: replyId, name: (replyName || '').trim() || '匿名' };
      dom.commentStatus.innerHTML = '正在回复 ' + escapeHtml(commentReplyTo.name) + ' <a href="javascript:void(0)" class="reply-cancel">取消</a>';
      dom.commentContent.focus();
      return;
    }

    var cancelBtn = e.target.closest('.reply-cancel');
    if (cancelBtn) {
      commentReplyTo = null;
      dom.commentStatus.textContent = '';
      return;
    }

    var likeBtn = e.target.closest('.comment-like-btn');
    if (likeBtn) {
      var likeId = parseInt(likeBtn.dataset.like, 10);
      if (!likeId) return;
      likeBtn.disabled = true;
      try {
        var resp = await fetch('/api/comments/like', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ comment_id: likeId, guest_key: getGuestKey() }),
        });
        var data = await resp.json();
        if (data && data.success && data.data && typeof data.data === 'object') {
          // 更新缓存并重渲染
          var found = null;
          Object.keys(state.commentsCache).forEach(function (mid) {
            var arr = state.commentsCache[mid];
            for (var i = 0; i < arr.length; i++) {
              if (arr[i].id === likeId) { found = arr[i]; break; }
            }
            if (found) return;
          });
          if (found) {
            found.my_liked = data.data.liked ? 1 : 0;
            found.like_count = data.data.count != null ? data.data.count : found.like_count;
          }
          if (currentDetail && currentDetail.enrichedMap) {
            renderComments(state.commentsCache[currentDetail.enrichedMap.online.identifier]);
          }
        } else {
          dom.commentStatus.textContent = (data && data.data && typeof data.data === 'string') ? data.data : '点赞失败';
        }
      } catch (err) {
        dom.commentStatus.textContent = '点赞失败';
      } finally {
        likeBtn.disabled = false;
      }
      return;
    }

    var delBtn = e.target.closest('.comment-del-btn');
    if (delBtn) {
      var delId = parseInt(delBtn.dataset.del, 10);
      if (!delId) return;
      if (!window.confirm('确定删除这条评论吗？')) return;
      try {
        var resp = await fetch('/api/comments/delete', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ comment_id: delId, guest_key: getGuestKey() }),
        });
        var data = await resp.json();
        if (data && data.success) {
          dom.commentStatus.textContent = '已删除';
          if (currentDetail && currentDetail.enrichedMap) {
            var mid = currentDetail.enrichedMap.online.identifier;
            delete state.commentsCache[mid];
            loadComments(mid);
          }
        } else {
          dom.commentStatus.textContent = (data && data.data && typeof data.data === 'string') ? data.data : '删除失败';
        }
      } catch (err) {
        dom.commentStatus.textContent = '删除失败';
      }
    }
  });

  dom.commentSubmitBtn.addEventListener('click', async function () {
    if (!currentDetail) return;
    var content = dom.commentContent.value.trim();
    if (!content) { dom.commentStatus.textContent = '请输入评论内容'; return; }
    var nick = dom.commentNick.value.trim();
    if (!nick) { dom.commentStatus.textContent = '请输入昵称'; return; }
    var em = currentDetail.enrichedMap;
    var hasOnline = em && em.online && em.match_level !== 'none';
    if (!hasOnline) {
      dom.commentStatus.textContent = '该地图未匹配在线数据，暂不支持评论';
      return;
    }
    var mapId = em.online.identifier;

    dom.commentSubmitBtn.disabled = true;
    dom.commentStatus.textContent = '提交中…';

    try {
      var url = '/api/comments';
      var body = {
        item_id: mapId,
        content: content,
        user_name: nick,
        guest_key: getGuestKey(),
        parent_id: commentReplyTo ? commentReplyTo.id : 0,
      };
      var resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!resp.ok) throw new Error('提交失败');
      dom.commentContent.value = '';
      commentReplyTo = null;
      dom.commentStatus.textContent = '评论发表成功！';
      // 清除缓存，重新加载
      delete state.commentsCache[mapId];
      loadComments(mapId);
    } catch (err) {
      dom.commentStatus.textContent = '评论失败：' + err.message;
    } finally {
      dom.commentSubmitBtn.disabled = false;
    }
  });

  // ====== 投票弹窗 ======
  function openVoteModal(info) {
    state.pendingVote = info;
    dom.voteTarget.textContent = info.displayText;
    dom.voteModal.classList.remove('hidden');
    dom.voteConfirmBtn.disabled = false;
  }

  dom.voteCancelBtn.addEventListener('click', function () { closeVoteModal(); });
  dom.voteModal.addEventListener('click', function (e) {
    if (e.target === dom.voteModal) closeVoteModal();
  });

  function closeVoteModal() {
    dom.voteModal.classList.add('hidden');
    state.pendingVote = null;
  }

  dom.voteConfirmBtn.addEventListener('click', async function () {
    if (!state.pendingVote) return;
    var info = state.pendingVote;
    dom.voteConfirmBtn.disabled = true;
    dom.voteConfirmBtn.innerHTML = '<span class="loading-spinner"></span> 发起中…';
    try {
      var data = await api('/api/action', {
        method: 'POST',
        json: { action: 'vote', mission: info.mission, map: info.map },
      });
      if (data.ok) {
        toast('投票已在游戏内发起，请等待同服玩家投票', 'success');
        closeVoteModal();
        closeDetailModal();
      } else {
        throw new Error(data.error || '发起失败');
      }
    } catch (err) {
      toast('发起投票失败：' + err.message, 'error');
    } finally {
      dom.voteConfirmBtn.disabled = false;
      dom.voteConfirmBtn.textContent = '发起投票';
    }
  });

  // ====== Mixmap 图池编辑器 ======
  function updateMixmapEntry() {
    if (!dom.mixmapOpenBtn || !dom.mixmapPanel) return;
    if (state.mixmapAvailable) {
      dom.mixmapOpenBtn.classList.remove('hidden');
      if (state.mixmapOpen) {
        dom.mixmapPanel.classList.remove('hidden');
        dom.mixmapOpenBtn.textContent = '收起 Mixmap 图池编辑器';
      } else {
        dom.mixmapPanel.classList.add('hidden');
        dom.mixmapOpenBtn.textContent = 'Mixmap 图池编辑器';
      }
    } else {
      state.mixmapOpen = false;
      dom.mixmapOpenBtn.classList.add('hidden');
      dom.mixmapPanel.classList.add('hidden');
    }
  }

  function setMixmapOpen(open) {
    var next = !!open && state.mixmapAvailable;
    var changed = next !== state.mixmapOpen;
    state.mixmapOpen = next;
    updateMixmapEntry();
    // 详情弹窗开着时同步「投票换图 / 加入图池」互斥
    if (dom.detailModal && !dom.detailModal.classList.contains('hidden')) updateDetailButtons();
    // 卡片「加入图池」按钮随面板开关显隐，仅状态变化时重渲
    if (changed && state.serverData) renderAll();
    if (state.mixmapOpen) {
      renderMixPool();
      switchMixTab('manual');
    }
  }

  function switchMixTab(tab) {
    var tabs = document.querySelectorAll('.mixmap-tab');
    tabs.forEach(function (btn) {
      btn.classList.toggle('active', btn.getAttribute('data-mix-tab') === tab);
    });
    if (dom.mixTabManual) dom.mixTabManual.classList.toggle('hidden', tab !== 'manual');
    if (dom.mixTabAuto) dom.mixTabAuto.classList.toggle('hidden', tab !== 'auto');
    if (dom.mixTabPreset) dom.mixTabPreset.classList.toggle('hidden', tab !== 'preset');
    if (tab === 'preset') loadMixPresets();
  }

  // 预设管理入口显隐：无权限时隐藏「预设管理」tab、保存行与面板，并回退到手动组池
  function updatePresetAccess() {
    var tabBtn = document.querySelector('.mixmap-tab[data-mix-tab="preset"]');
    var can = !!state.canManagePresets;
    if (tabBtn) tabBtn.classList.toggle('hidden', !can);
    if (dom.mixSaveRow) dom.mixSaveRow.classList.toggle('hidden', !can);
    if (!can) {
      if (dom.mixTabPreset) dom.mixTabPreset.classList.add('hidden');
      var activeTab = document.querySelector('.mixmap-tab.active');
      if (activeTab && activeTab.getAttribute('data-mix-tab') === 'preset') switchMixTab('manual');
    }
  }

  function mapDisplayName(mapCode, mission) {
    var d = state.serverData;
    if (d && d.maps) {
      for (var i = 0; i < d.maps.length; i++) {
        var m = d.maps[i];
        if (m.chapter_map === mapCode) {
          var camp = m.mission_display_chi || m.mission_display_en || m.mission;
          var chap = m.chapter_chi || m.chapter_en || mapCode;
          return camp + ' · ' + chap;
        }
      }
    }
    return mission ? (mission + ' · ' + mapCode) : mapCode;
  }

  // 返回 true 表示新加入，false 表示已存在（调用方据此决定提示文案）
  function addMapToPool(mapCode, displayName, mission) {
    if (!mapCode) return false;
    if (state.mixPool.length >= state.maxPoolMaps) {
      toast('图池最多 ' + state.maxPoolMaps + ' 张地图', 'error');
      return false;
    }
    // 去重：同 map 不重复加入
    for (var i = 0; i < state.mixPool.length; i++) {
      if (state.mixPool[i].map === mapCode) {
        toast('图池中已有 ' + mapCode, 'info');
        return false;
      }
    }
    state.mixPool.push({
      map: mapCode,
      displayName: displayName || mapDisplayName(mapCode, mission),
      mission: mission || '',
    });
    if (!state.mixmapOpen) setMixmapOpen(true);
    else renderMixPool();
    return true;
  }

  function addMissionFirstToPool(mission) {
    var em = findCampaignRep(mission);
    if (!em) return;
    var mapCode = em.chapter_map;
    // 优先 is_first 章节
    var d = state.serverData;
    if (d && d.maps) {
      for (var i = 0; i < d.maps.length; i++) {
        if (d.maps[i].mission === mission && d.maps[i].is_first) {
          mapCode = d.maps[i].chapter_map;
          em = d.maps[i];
          break;
        }
      }
    }
    var name = mapDisplayName(mapCode, mission);
    if (addMapToPool(mapCode, name, mission))
      toast('已加入图池：' + mapCode, 'success');
  }

  // ====== 卡片「加入图池」章节选择弹窗 ======
  var currentPick = null; // { mission, campName }

  // 单章节战役直接加入（保持原行为）；多章节弹窗让用户选择具体章节
  function openChapterPicker(mission) {
    var em = findCampaignRep(mission);
    if (!em) return;
    var d = state.serverData;
    var chapters = (d && d.maps) ? d.maps.filter(function (m) { return m.mission === mission; }) : [];
    if (chapters.length <= 1) {
      addMissionFirstToPool(mission);
      return;
    }

    var campName = em.mission_display_chi || em.mission_display_en || em.mission;
    dom.chapterPickCampaign.textContent = campName;

    // 默认选中起始章节（is_first）；无起始标记时选中第一项（与详情弹窗默认一致）
    var defaultIdx = 0;
    for (var fi = 0; fi < chapters.length; fi++) {
      if (chapters[fi].is_first) { defaultIdx = fi; break; }
    }

    dom.chapterPickList.innerHTML = chapters.map(function (ch, idx) {
      var code = ch.chapter_map;
      var label = ch.chapter_chi || ch.chapter_en || code;
      var isSelected = idx === defaultIdx;
      return '' +
        '<div class="chapter-item' + (isSelected ? ' selected' : '') + '"' +
        ' data-chapter-map="' + escapeHtml(code) + '"' +
        ' data-chapter-label="' + escapeHtml(label) + '">' +
        '<div class="chapter-radio"></div>' +
        '<span class="chapter-label">' + escapeHtml(label) + (ch.is_first ? ' (起始章节)' : '') + '</span>' +
        '<span class="chapter-code">' + escapeHtml(code) + '</span>' +
        '</div>';
    }).join('');

    dom.chapterPickList.querySelectorAll('.chapter-item').forEach(function (item) {
      item.addEventListener('click', function () {
        dom.chapterPickList.querySelectorAll('.chapter-item').forEach(function (el) { el.classList.remove('selected'); });
        item.classList.add('selected');
      });
    });

    currentPick = { mission: mission, campName: campName };
    dom.chapterPickModal.classList.remove('hidden');
  }

  function closeChapterPicker() {
    dom.chapterPickModal.classList.add('hidden');
    currentPick = null;
  }

  dom.chapterPickCancelBtn.addEventListener('click', closeChapterPicker);
  dom.chapterPickModal.addEventListener('click', function (e) {
    if (e.target === dom.chapterPickModal) closeChapterPicker();
  });

  dom.chapterPickConfirmBtn.addEventListener('click', function () {
    if (!currentPick) return;
    var selected = dom.chapterPickList.querySelector('.chapter-item.selected');
    var mapCode = selected ? selected.dataset.chapterMap : '';
    if (!mapCode) return;
    var chapLabel = selected.dataset.chapterLabel || mapCode;
    if (addMapToPool(mapCode, currentPick.campName + ' · ' + chapLabel, currentPick.mission))
      toast('已加入图池：' + mapCode, 'success');
    closeChapterPicker();
  });

  function removePoolAt(idx) {
    if (idx < 0 || idx >= state.mixPool.length) return;
    state.mixPool.splice(idx, 1);
    renderMixPool();
  }

  function movePoolItem(from, to) {
    if (from === to || from < 0 || to < 0 || from >= state.mixPool.length || to >= state.mixPool.length) return;
    var item = state.mixPool.splice(from, 1)[0];
    state.mixPool.splice(to, 0, item);
    renderMixPool();
  }

  function renderMixPool() {
    if (!dom.mixPoolList || !dom.mixPoolCount) return;
    dom.mixPoolCount.textContent = '图池 ' + state.mixPool.length + ' 张';
    if (!state.mixPool.length) {
      dom.mixPoolList.innerHTML = '<div class="mixmap-empty">从下方地图列表点「加入图池」可选择章节，或打开详情选章节加入。支持拖拽排序。</div>';
      return;
    }
    dom.mixPoolList.innerHTML = state.mixPool.map(function (item, idx) {
      return '' +
        '<div class="mixmap-pool-item" draggable="true" data-idx="' + idx + '">' +
          '<span class="mixmap-pool-handle" title="拖拽排序">☰</span>' +
          '<span class="mixmap-pool-idx">' + (idx + 1) + '</span>' +
          '<div class="mixmap-pool-meta">' +
            '<div class="mixmap-pool-name">' + escapeHtml(item.displayName || item.map) + '</div>' +
            '<div class="mixmap-pool-code">' + escapeHtml(item.map) + '</div>' +
          '</div>' +
          '<button type="button" class="mixmap-pool-remove" data-remove-idx="' + idx + '">移除</button>' +
        '</div>';
    }).join('');
  }

  // 拖拽排序
  if (dom.mixPoolList) {
    dom.mixPoolList.addEventListener('dragstart', function (e) {
      var item = e.target.closest('.mixmap-pool-item');
      if (!item) return;
      state.mixDragFrom = parseInt(item.getAttribute('data-idx'), 10);
      item.classList.add('dragging');
      if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', String(state.mixDragFrom));
      }
    });
    dom.mixPoolList.addEventListener('dragend', function (e) {
      var item = e.target.closest('.mixmap-pool-item');
      if (item) item.classList.remove('dragging');
      state.mixDragFrom = -1;
    });
    dom.mixPoolList.addEventListener('dragover', function (e) {
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    });
    dom.mixPoolList.addEventListener('drop', function (e) {
      e.preventDefault();
      var target = e.target.closest('.mixmap-pool-item');
      if (!target) return;
      var to = parseInt(target.getAttribute('data-idx'), 10);
      var from = state.mixDragFrom;
      if (isNaN(from)) return;
      movePoolItem(from, to);
    });
    dom.mixPoolList.addEventListener('click', function (e) {
      var btn = e.target.closest('[data-remove-idx]');
      if (!btn) return;
      removePoolAt(parseInt(btn.getAttribute('data-remove-idx'), 10));
    });
  }

  if (dom.mixmapOpenBtn) {
    dom.mixmapOpenBtn.addEventListener('click', function () {
      setMixmapOpen(!state.mixmapOpen);
    });
  }
  if (dom.mixmapCloseBtn) {
    dom.mixmapCloseBtn.addEventListener('click', function () {
      setMixmapOpen(false);
    });
  }

  document.querySelectorAll('.mixmap-tab').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var t = btn.getAttribute('data-mix-tab');
      if (t === 'preset' && !state.canManagePresets) return;
      switchMixTab(t);
    });
  });

  if (dom.mixPoolClearBtn) {
    dom.mixPoolClearBtn.addEventListener('click', function () {
      state.mixPool = [];
      renderMixPool();
    });
  }

  if (dom.mixPoolStartBtn) {
    dom.mixPoolStartBtn.addEventListener('click', async function () {
      if (!state.mixPool.length) {
        toast('请先向图池添加地图', 'error');
        return;
      }
      var maps = state.mixPool.map(function (x) { return x.map; });
      var presetName = (dom.mixPresetNameInput && dom.mixPresetNameInput.value.trim()) || '';
      dom.mixPoolStartBtn.disabled = true;
      var oldText = dom.mixPoolStartBtn.textContent;
      dom.mixPoolStartBtn.innerHTML = '<span class="loading-spinner"></span> 发起中…';
      try {
        var body = { maps: maps };
        if (presetName) body.preset_name = presetName;
        var data = await api('/api/mixmap/start', { method: 'POST', json: body });
        if (data.ok) {
          toast('已发起游戏内图池投票，请到游戏里确认', 'success');
        } else {
          throw new Error(data.error || '发起失败');
        }
      } catch (err) {
        toast('发起图池失败：' + err.message, 'error');
      } finally {
        dom.mixPoolStartBtn.disabled = false;
        dom.mixPoolStartBtn.textContent = oldText || '发起图池投票';
      }
    });
  }

  if (dom.mixPoolSaveBtn) {
    dom.mixPoolSaveBtn.addEventListener('click', async function () {
      if (!state.mixPool.length) {
        toast('图池为空，无法保存', 'error');
        return;
      }
      var name = dom.mixPresetNameInput ? dom.mixPresetNameInput.value.trim() : '';
      if (!name) {
        toast('请填写预设名称', 'error');
        return;
      }
      dom.mixPoolSaveBtn.disabled = true;
      try {
        var data = await api('/api/mixmap/presets', {
          method: 'POST',
          json: {
            name: name,
            maps: state.mixPool.map(function (x) { return x.map; }),
            gamemode: (state.serverData && state.serverData.gamemode) || '',
          },
        });
        if (!data.ok) throw new Error(data.error || '保存失败');
        toast('预设已保存：' + name, 'success');
        loadMixPresets();
      } catch (err) {
        toast('保存预设失败：' + err.message, 'error');
      } finally {
        dom.mixPoolSaveBtn.disabled = false;
      }
    });
  }

  // 自动组池
  document.querySelectorAll('[data-auto-type]').forEach(function (btn) {
    btn.addEventListener('click', async function () {
      var t = btn.getAttribute('data-auto-type');
      btn.disabled = true;
      try {
        var data = await api('/api/mixmap/auto', { method: 'POST', json: { type: t } });
        if (data.ok) {
          toast('已发起自动组池投票（' + t + '），请到游戏里确认', 'success');
        } else {
          throw new Error(data.error || '发起失败');
        }
      } catch (err) {
        toast('自动组池失败：' + err.message, 'error');
      } finally {
        btn.disabled = false;
      }
    });
  });

  async function loadMixPresets() {
    if (!dom.mixPresetList) return;
    dom.mixPresetList.innerHTML = '<div class="mixmap-empty"><span class="loading-spinner"></span> 加载预设…</div>';
    try {
      var q = (dom.mixPresetSearchInput ? dom.mixPresetSearchInput.value.trim() : '') || '';
      if (q !== state.mixPresetQuery) { state.mixPresetQuery = q; state.mixPresetPage = 1; }
      var page = Math.max(1, state.mixPresetPage || 1);
      var url = '/api/mixmap/presets?page=' + page + '&page_size=10';
      if (q) url += '&q=' + encodeURIComponent(q);
      var data = await api(url);
      state.mixPresets = (data && data.presets) ? data.presets : [];
      state.mixPresetTotal = (data && data.total) ? data.total : 0;
      renderMixPresets();
    } catch (err) {
      dom.mixPresetList.innerHTML = '<div class="mixmap-empty">加载失败：' + escapeHtml(err.message) + '</div>';
    }
  }

  function renderMixPresets() {
    if (!dom.mixPresetList) return;
    if (!state.mixPresets.length) {
      dom.mixPresetList.innerHTML = '<div class="mixmap-empty">' +
        (state.mixPresetTotal ? '没有匹配的预设' : '暂无预设，可在「手动组池」保存') + '</div>';
      updateMixPresetPagination();
      return;
    }
    var curMode = (state.serverData && state.serverData.gamemode) || '';
    dom.mixPresetList.innerHTML = state.mixPresets.map(function (p, idx) {
      var mismatch = !!(p.gamemode && curMode && p.gamemode !== curMode);
      var metaParts = [];
      if (p.gamemode) metaParts.push(escapeHtml(p.gamemode));
      if (p.maps) metaParts.push(p.maps.length + ' 张');
      if (p.owner_steam_id) metaParts.push(escapeHtml(p.owner_steam_id));
      var gmHint = metaParts.length
        ? '<div class="mixmap-pool-code"' + (mismatch ? ' style="opacity:.7;"' : '') + '>' + metaParts.join(' · ') + '</div>'
        : '';
      var delBtn = p.can_delete
        ? '<button type="button" class="mixmap-preset-del" data-preset-del="' + idx + '">删除</button>'
        : '';
      return '' +
        '<div class="mixmap-preset-item" data-preset-idx="' + idx + '">' +
          '<div class="mixmap-pool-meta">' +
            '<div class="mixmap-pool-name">' + escapeHtml(p.name || '') + '</div>' +
            gmHint +
          '</div>' +
          '<div class="mixmap-preset-actions">' +
            '<button type="button" class="btn btn-ghost btn-sm" data-preset-preview="' + idx + '">预览</button>' +
            '<button type="button" class="btn btn-ghost btn-sm" data-preset-load="' + idx + '">加载</button>' +
            delBtn +
          '</div>' +
        '</div>';
    }).join('');
    updateMixPresetPagination();
  }

  function updateMixPresetPagination() {
    var total = state.mixPresetTotal || 0;
    var page = Math.max(1, state.mixPresetPage || 1);
    var pageSize = 10;
    var pages = Math.max(1, Math.ceil(total / pageSize));
    if (dom.mixPresetPageInfo) {
      dom.mixPresetPageInfo.textContent = '第 ' + page + ' / ' + pages + ' 页（共 ' + total + ' 条）';
    }
    if (dom.mixPresetPagination) dom.mixPresetPagination.classList.toggle('hidden', total <= pageSize);
    if (dom.mixPresetPrevBtn) dom.mixPresetPrevBtn.disabled = page <= 1;
    if (dom.mixPresetNextBtn) dom.mixPresetNextBtn.disabled = page >= pages;
  }

  if (dom.mixPresetList) {
    dom.mixPresetList.addEventListener('click', async function (e) {
      var prevBtn = e.target.closest('[data-preset-preview]');
      if (prevBtn) {
        var pi = parseInt(prevBtn.getAttribute('data-preset-preview'), 10);
        var pp = state.mixPresets[pi];
        if (!pp || !pp.maps) return;
        openPresetPreview(pp);
        return;
      }
      var loadBtn = e.target.closest('[data-preset-load]');
      if (loadBtn) {
        var li = parseInt(loadBtn.getAttribute('data-preset-load'), 10);
        var preset = state.mixPresets[li];
        if (!preset || !preset.maps) return;
        state.mixPool = preset.maps.map(function (m) {
          return { map: m, displayName: mapDisplayName(m, ''), mission: '' };
        });
        if (dom.mixPresetNameInput) dom.mixPresetNameInput.value = preset.name || '';
        switchMixTab('manual');
        renderMixPool();
        toast('已加载预设：' + (preset.name || ''), 'success');
        return;
      }
      var delBtn = e.target.closest('[data-preset-del]');
      if (delBtn) {
        var di = parseInt(delBtn.getAttribute('data-preset-del'), 10);
        var dp = state.mixPresets[di];
        if (!dp) return;
        if (!window.confirm('删除预设「' + dp.name + '」？')) return;
        try {
          await api('/api/mixmap/presets/' + encodeURIComponent(dp.name), { method: 'DELETE' });
          toast('已删除预设', 'success');
          loadMixPresets();
        } catch (err) {
          toast('删除失败：' + err.message, 'error');
        }
      }
    });
  }

  if (dom.mixPresetRefreshBtn) {
    dom.mixPresetRefreshBtn.addEventListener('click', function () {
      state.mixPresetQuery = '';
      state.mixPresetPage = 1;
      if (dom.mixPresetSearchInput) dom.mixPresetSearchInput.value = '';
      loadMixPresets();
    });
  }

  if (dom.mixPresetSearchBtn) {
    dom.mixPresetSearchBtn.addEventListener('click', function () {
      state.mixPresetPage = 1;
      loadMixPresets();
    });
  }
  if (dom.mixPresetSearchInput) {
    dom.mixPresetSearchInput.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') {
        state.mixPresetPage = 1;
        loadMixPresets();
      }
    });
  }
  if (dom.mixPresetPrevBtn) {
    dom.mixPresetPrevBtn.addEventListener('click', function () {
      if ((state.mixPresetPage || 1) <= 1) return;
      state.mixPresetPage -= 1;
      loadMixPresets();
    });
  }
  if (dom.mixPresetNextBtn) {
    dom.mixPresetNextBtn.addEventListener('click', function () {
      state.mixPresetPage += 1;
      loadMixPresets();
    });
  }

  // ====== 预设预览弹窗 ======
  function openPresetPreview(p) {
    if (dom.presetPreviewTitle) dom.presetPreviewTitle.textContent = '预设预览：' + (p.name || '');
    var metaParts = [];
    if (p.gamemode) metaParts.push(escapeHtml(p.gamemode));
    if (p.maps) metaParts.push(p.maps.length + ' 张');
    if (p.owner_steam_id) metaParts.push(escapeHtml(p.owner_steam_id));
    if (dom.presetPreviewMeta) dom.presetPreviewMeta.innerHTML = metaParts.join(' · ');
    var lis = (p.maps || []).map(function (m, i) {
      return '<li><span class="preset-preview-idx">' + (i + 1) + '</span>' +
        escapeHtml(mapDisplayName(m, '')) +
        ' <span class="preset-preview-code">' + escapeHtml(m) + '</span></li>';
    }).join('');
    if (dom.presetPreviewList) dom.presetPreviewList.innerHTML = lis || '<li class="preset-preview-empty">（空）</li>';
    dom.presetPreviewModal.classList.remove('hidden');
    document.body.style.overflow = 'hidden';
  }

  function closePresetPreview() {
    dom.presetPreviewModal.classList.add('hidden');
    document.body.style.overflow = '';
  }

  if (dom.presetPreviewCloseBtn) dom.presetPreviewCloseBtn.addEventListener('click', closePresetPreview);
  if (dom.presetPreviewModal) {
    dom.presetPreviewModal.addEventListener('click', function (e) {
      if (e.target === dom.presetPreviewModal) closePresetPreview();
    });
  }

  // ====== SSE 实时推送 ======
  function connectSSE() {
    if (state._retryTimer) { clearTimeout(state._retryTimer); state._retryTimer = null; }
    if (state.eventSource) state.eventSource.close();
    if (!state.token || !state.serverKey) return;
    try {
      var es = new EventSource('/api/events?server=' + encodeURIComponent(state.serverKey) + '&token=' + encodeURIComponent(state.token));
      state.eventSource = es;

      es.addEventListener('map_data', function (e) {
        try { var d = JSON.parse(e.data); if (d.current_map) { loadServerState(true); } } catch (ex) {}
      });

      es.addEventListener('map_changed', function (e) {
        try { var d = JSON.parse(e.data); if (d.current_map) { toast('地图已切换：' + d.current_map, 'info'); loadServerState(true); } } catch (ex) {}
      });

      es.addEventListener('vote_result', function (e) {
        try { var d = JSON.parse(e.data); toast('投票结果: ' + (d.result || '完成'), 'info'); } catch (ex) {}
      });

      es.onopen = function () { state.sseRetryMs = 1000; stopPolling(); };
      es.onerror = function () {
        startPolling();
        var wasClosed = es.readyState === EventSource.CLOSED;
        es.close();
        state.eventSource = null;
        if (wasClosed) { state.sseRetryMs = Math.min(state.sseRetryMs * 2, 30000); }
        var retry = wasClosed ? state.sseRetryMs : Math.min(state.sseRetryMs * 2, 30000);
        state.sseRetryMs = retry;
        state._retryTimer = setTimeout(function () { connectSSE(); }, retry);
      };
    } catch (e) { startPolling(); }
  }

  function startPolling() {
    if (state.pollIntervalId) return;
    state.pollIntervalId = setInterval(async function () {
      // 登录模式需 token；预览模式无 token 也轮询（SSE 需要鉴权，游客用轮询）
      if (!state.serverKey) return;
      if (!state.previewMode && !state.token) return;
      try { loadServerState(true); } catch (e) { /* 静默 */ }
    }, 8000);
  }

  function stopPolling() {
    if (state.pollIntervalId) { clearInterval(state.pollIntervalId); state.pollIntervalId = null; }
  }

  // ====== Escape 键关闭弹窗 ======
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
      // 弹窗互斥出现，按优先级依次关闭：投票 → 章节选择 → 详情 → 预设预览。
      if (!dom.voteModal.classList.contains('hidden')) closeVoteModal();
      else if (!dom.chapterPickModal.classList.contains('hidden')) closeChapterPicker();
      else if (!dom.detailModal.classList.contains('hidden')) closeDetailModal();
      else if (!dom.presetPreviewModal.classList.contains('hidden')) closePresetPreview();
    }
  });

  // ====== 初始化 ======
  // 背景加载独立于登录状态，立即启动
  initBg();

  // 启动分流：URL 带 ?code= 时，优先用验证码重新登录（即使已有旧 token 也覆盖），
  // 避免玩家切换服务器时被旧会话短路、停留在原服面板。
  var startupParams = new URLSearchParams(window.location.search);
  var startupCode = startupParams.get('code');
  if (startupCode && startupCode.trim()) {
    // 切换服务器：丢弃旧会话，走自动登录
    sessionStorage.clear();
    state.token = '';
    state.serverKey = '';
    autoLoginFromUrlOrShow();
  } else if (state.token) {
    api('/api/me').then(function (data) {
      if (data && data.ok) state.canManagePresets = !!data.can_manage_presets;
      showPanel();
    }).catch(function () {
      sessionStorage.clear();
      state.token = '';
      autoLoginFromUrlOrShow();
    });
  } else {
    autoLoginFromUrlOrShow();
  }

  // 如果 URL 带 ?code=XXX，直接用验证码自动登录；成功进面板，
  // 失败回退到验证码页（雪藏入口：仅此场景出现）；无验证码则进入预览系统。
  function autoLoginFromUrlOrShow() {
    var params = new URLSearchParams(window.location.search);
    var codeFromUrl = params.get('code');
    if (!codeFromUrl) { showPreview(); return; }
    codeFromUrl = codeFromUrl.trim().toUpperCase();
    if (!codeFromUrl) { showPreview(); return; }
    dom.codeInput.value = codeFromUrl;
    dom.loginBtn.disabled = true;
    dom.loginBtn.textContent = '自动登录中…';
    dom.loginTip.textContent = '';
    api('/api/login', { method: 'POST', json: { code: codeFromUrl } }).then(function (data) {
      if (!data.ok) throw new Error('登录失败');
      state.token = data.token;
      state.serverKey = data.server_key;
      state.playerName = data.player;
      state.serverName = data.server_name || data.server_key;
      state.canManagePresets = !!data.can_manage_presets;
      sessionStorage.setItem('wm_token', state.token);
      sessionStorage.setItem('wm_server_key', state.serverKey);
      sessionStorage.setItem('wm_player', state.playerName);
      sessionStorage.setItem('wm_server_name', state.serverName);
      // 清除 URL 中的 code 参数，避免刷新重复登录
      if (window.history && window.history.replaceState) {
        var url = new URL(window.location);
        url.searchParams.delete('code');
        window.history.replaceState({}, '', url);
      }
      showPanel();
    }).catch(function () {
      // 自动登录失败：清除 URL 中的 code 参数（避免刷新后反复尝试无效验证码），
      // 显示验证码页供手动输入重试。
      if (window.history && window.history.replaceState) {
        var url = new URL(window.location);
        url.searchParams.delete('code');
        window.history.replaceState({}, '', url);
      }
      showLogin();
    }).finally(function () {
      dom.loginBtn.disabled = false;
      dom.loginBtn.textContent = '进入';
    });
  }

})();

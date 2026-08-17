// WebMap 后端入口。
//
// 用法:
//
//	webmap init    交互式配置向导（无参数时逐步提问，带参数时批量生成）
//	webmap serve   启动 HTTP 服务（配置不存在时自动进入向导）
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"webmap/internal/api"
	"webmap/internal/config"
	"webmap/internal/onlinemap"
	"webmap/internal/perm"
	"webmap/internal/query"
	"webmap/internal/rconcfg"
	"webmap/internal/session"
	"webmap/internal/store"
)

// embeddedWeb 将 backend/web 目录整体嵌入二进制，实现单文件分发。
// 运行时若磁盘上的 WebDir 不存在，则回退到这份内嵌资源。
//
//go:embed all:web
var embeddedWeb embed.FS

func main() {
	// 不带参数（例如在资源管理器双击 webmap.exe）时进入交互式控制台菜单
	if len(os.Args) < 2 {
		runLauncher()
		return
	}

	switch os.Args[1] {
	case "menu":
		runLauncher()
	case "init":
		cmdInit(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`WebMap - L4D2 浏览器换图插件后端

推荐用法（可视化）:
  直接运行（或在资源管理器双击 webmap.exe）
      进入交互式控制台菜单：配置向导 / 启动服务 / 查看状态，全程无需记忆参数。
  webmap menu                 同上，显式进入交互式菜单

进阶用法（命令行）:
  webmap init                 交互式配置向导（逐步提问，回车用默认值）
  webmap init -data ./data    批量模式（带任意参数即跳过提问直接生成）
  webmap serve                启动 HTTP 服务（默认 data/config.json）
  webmap serve -c 路径        指定配置文件（不存在时自动进入向导）

init 可选参数（不传则交互提问）:
  -data string      数据目录 (默认 "./data")
  -listen string    监听地址 (默认 ":11223")
  -secret string    推送密钥（留空自动生成）
  -rcon-mode string RCON 模式: unified / per_port
  -rcon-pass string unified 模式下的 RCON 密码
`)
}

// =====================================================================
// init 命令
// =====================================================================

// wizardResult 交互向导的收集结果。
type wizardResult struct {
	dataDir       string
	listen        string
	secret        string
	rconMode      string
	rconPass      string
	perPort       map[string]string
	hostOverride  map[string]string
	onlineMapURL  string
	bgMode        string
	bgURL         string
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dataDir := fs.String("data", "./data", "数据目录")
	listen := fs.String("listen", ":11223", "监听地址")
	secret := fs.String("secret", "", "推送密钥（留空自动生成）")
	rconMode := fs.String("rcon-mode", "", `RCON 模式: "unified" 或 "per_port"`)
	rconPass := fs.String("rcon-pass", "", "unified 模式下的 RCON 密码")
	fs.Parse(args)

	// 无参数时进入交互式向导
	if len(args) == 0 {
		result := runWizard(bufio.NewReader(os.Stdin))
		generateConfig(result.dataDir, result.listen, result.secret, result.rconMode, result.rconPass, result.onlineMapURL, result.bgMode, result.bgURL, result.perPort, result.hostOverride)
		return
	}

	// 批量模式（带参数）
	mode := *rconMode
	if mode == "" {
		mode = "unified"
	}
	generateConfig(*dataDir, normalizeListen(*listen), *secret, mode, *rconPass, config.DefaultOnlineMapURL, "default", config.DefaultBgURL, map[string]string{}, map[string]string{})
}

// runWizard 运行交互式配置向导。
func runWizard(reader *bufio.Reader) *wizardResult {
	printBanner()

	w := &wizardResult{
		perPort:      map[string]string{},
		hostOverride: map[string]string{},
	}

	fmt.Println()
	w.dataDir = prompt(reader, "数据目录", "./data")
	w.listen = normalizeListen(prompt(reader, "监听地址 (端口)", ":11223"))

	// 推送密钥
	fmt.Println()
	fmt.Println("─── 推送密钥 (push_secret) ───")
	fmt.Println("用于校验游戏服务器的上报请求，须与插件 webmap_push_secret 一致。")
	autoSecret := randHex(24)
	fmt.Printf("已为你自动生成: %s\n", autoSecret)
	w.secret = prompt(reader, "直接回车使用自动生成的密钥，或输入自定义密钥", autoSecret)

	// 在线地图元数据（预览图 / 中文名 / 下载链接）
	fmt.Println()
	fmt.Println("─── 在线地图元数据（可选）───")
	fmt.Println("用于匹配第三方地图的预览图、中文名、下载链接。")
	onlineIdx := promptChoice(reader, "选择在线源模式", []string{
		"启用在线源 - 使用远程 CSV 获取第三方地图信息（推荐）",
		"仅本地      - 只显示服务器上报的地图，无预览图",
	})
	if onlineIdx == 0 {
		fmt.Println("已内置默认数据源，回车即可使用。")
		input := prompt(reader, "CSV 地址（回车使用内置默认源，或输入自定义地址）", "")
		if input == "" {
			w.onlineMapURL = config.DefaultOnlineMapURL
		} else {
			w.onlineMapURL = input
		}
	} else {
		w.onlineMapURL = ""
	}

	// 前端背景图模式
	fmt.Println()
	fmt.Println("─── 前端背景图（可选）───")
	fmt.Println("控制 Web 界面是否显示背景图及遮罩层。")
	bgIdx := promptChoice(reader, "选择背景图模式", []string{
		"默认图链 - 使用预设的随机背景图链接（推荐）",
		"自定义   - 输入自定义图片链接",
		"无背景   - 不显示背景图",
	})
	switch bgIdx {
	case 0:
		w.bgMode = "default"
		w.bgURL = config.DefaultBgURL
		fmt.Printf("默认图链：%s\n", config.DefaultBgURL)
	case 1:
		w.bgMode = "custom"
		w.bgURL = prompt(reader, "自定义图片链接（支持 http/https URL）", "")
	case 2:
		w.bgMode = "none"
		w.bgURL = ""
	}

	// RCON 模式
	fmt.Println()
	fmt.Println("─── RCON 密码配置 ───")
	fmt.Println("浏览器不直接接触 RCON，所有指令由后端代理执行。")
	fmt.Println("密码只存在后端的 rcon.json，不会下发给浏览器。")
	modeIdx := promptChoice(reader, "选择密码模式", []string{
		"unified  - 全部端口共用一个密码",
		"per_port - 每个端口各自密码",
	})
	if modeIdx == 0 {
		w.rconMode = "unified"
		w.rconPass = prompt(reader, "RCON 密码 (所有端口共用)", "")
	} else {
		w.rconMode = "per_port"
		fmt.Println("输入每个端口的密码（输入空行结束）：")
		for {
			port := prompt(reader, "端口号 (如 27015，留空结束)", "")
			if port == "" {
				break
			}
			pwd := prompt(reader, "端口 "+port+" 的 RCON 密码", "")
			w.perPort[port] = pwd
		}
	}

	// 确认
	fmt.Println()
	fmt.Println("════════════════════════════════════════")
	fmt.Println("确认配置：")
	fmt.Printf("  数据目录    : %s\n", w.dataDir)
	fmt.Printf("  监听地址    : %s\n", w.listen)
	fmt.Printf("  推送密钥    : %s\n", maskSecret(w.secret))
	if w.onlineMapURL != "" {
		if w.onlineMapURL == config.DefaultOnlineMapURL {
			fmt.Println("  在线地图CSV : 默认数据源")
		} else {
			fmt.Printf("  在线地图CSV : %s\n", w.onlineMapURL)
		}
	} else {
		fmt.Println("  在线地图CSV : （仅本地）")
	}
	fmt.Printf("  前端背景    : %s\n", bgModeLabel(w.bgMode))
	if w.bgMode == "custom" {
		fmt.Printf("  背景图链接  : %s\n", w.bgURL)
	}
	fmt.Printf("  RCON 模式   : %s\n", w.rconMode)
	if w.rconMode == "unified" {
		fmt.Printf("  RCON 密码   : %s\n", maskSecret(w.rconPass))
	} else {
		fmt.Printf("  端口密码数  : %d\n", len(w.perPort))
	}
	fmt.Println("════════════════════════════════════════")

	confirm := prompt(reader, "确认生成配置？(y/n)", "y")
	if strings.ToLower(confirm) != "y" {
		fmt.Println("已取消。")
		os.Exit(0)
	}

	return w
}

// generateConfig 生成 config.json + rcon.json
func generateConfig(dataDir, listen, secret, rconMode, rconPass, onlineMapURL, bgMode, bgURL string, perPort, hostOverride map[string]string) {
	if secret == "" {
		secret = randHex(24)
	}

	// config.json
	cfg := &config.Config{
		Listen:       listen,
		PushSecret:   secret,
		DataDir:      dataDir,
		WebDir:       "./web",
		SessionTTL:   7200,
		CodeTTL:      300,
		RconTimeout:  5,
		OnlineMapURL: onlineMapURL,
		BgMode:       bgMode,
		BgURL:        bgURL,
	}
	cfgPath := filepath.Join(dataDir, "config.json")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		die("创建目录失败: %v", err)
	}
	if err := writeJSONFile(cfgPath, cfg); err != nil {
		die("写入 config.json 失败: %v", err)
	}

	// rcon.json
	rpath := filepath.Join(dataDir, "rcon.json")
	rcfg := &rconcfg.Config{
		Mode:            rconMode,
		UnifiedPassword: rconPass,
		PerPort:         perPort,
		HostOverride:    hostOverride,
	}
	if err := writeJSONFile(rpath, rcfg); err != nil {
		die("写入 rcon.json 失败: %v", err)
	}

	fmt.Println()
	fmt.Println("✓ 配置已生成：")
	fmt.Printf("  %s\n  %s\n", cfgPath, rpath)
	fmt.Println()
	fmt.Println("下一步：")
	fmt.Printf("  运行: webmap serve -c %s\n", cfgPath)
	fmt.Println()
	fmt.Println("─── 重要：插件侧配置 ───")
	fmt.Println("在游戏服务器的 cfg/sourcemod/webmap.cfg 中设置：")
	fmt.Printf("  webmap_backend_url  \"http://<后端IP>:端口\"\n")
	fmt.Printf("  webmap_push_secret \"%s\"\n", secret)
	fmt.Println("  （密钥必须与上方完全一致）")
}

// printBanner 打印向导欢迎横幅。
func printBanner() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║     L4D2 WebMap 初始化配置向导           ║")
	fmt.Println("║     逐步问答，按回车使用 [方括号] 默认值  ║")
	fmt.Println("╚══════════════════════════════════════════╝")
}

// prompt 提问并读取一行输入，回车则用默认值。
func prompt(reader *bufio.Reader, label string, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// promptChoice 多选一提问，返回选中的索引（从 0 开始）。
func promptChoice(reader *bufio.Reader, label string, options []string) int {
	for {
		fmt.Println()
		for i, opt := range options {
			fmt.Printf("  %d) %s\n", i+1, opt)
		}
		fmt.Printf("%s [1]: ", label)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return 0 // 默认第一项
		}
		idx, err := strconv.Atoi(line)
		if err == nil && idx >= 1 && idx <= len(options) {
			return idx - 1
		}
		fmt.Println("输入无效，请重试。")
	}
}

// normalizeListen 规范化监听地址：若只输入纯端口号（如 "8080"），自动补冒号前缀 ":8080"。
// 用户习惯直接输端口号，但 Go net.Listen 要求 ":port" 或 "host:port" 格式。
func normalizeListen(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	if !strings.Contains(addr, ":") {
		if _, err := strconv.Atoi(addr); err == nil {
			return ":" + addr
		}
	}
	return addr
}

// maskSecret 遮蔽密码用于展示。
func maskSecret(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

// bgModeLabel 返回背景模式的中文标签。
func bgModeLabel(mode string) string {
	switch mode {
	case "default":
		return "默认图链"
	case "custom":
		return "自定义链接"
	case "none":
		return "无背景"
	default:
		return mode
	}
}

// =====================================================================
// serve 命令
// =====================================================================

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("c", "data/config.json", "配置文件路径")
	fs.Parse(args)

	// 配置文件不存在时自动进入交互向导
	if _, err := os.Stat(*cfgPath); os.IsNotExist(err) {
		fmt.Println("未找到配置文件：", *cfgPath)
		fmt.Println()
		fmt.Println("将进入初始化配置向导，或按 Ctrl+C 退出。")
		reader := bufio.NewReader(os.Stdin)
		cont := prompt(reader, "是否现在开始配置？(y/n)", "y")
		if strings.ToLower(cont) != "y" {
			os.Exit(0)
		}
		result := runWizard(reader)
		generateConfig(result.dataDir, result.listen, result.secret, result.rconMode, result.rconPass, result.onlineMapURL, result.bgMode, result.bgURL, result.perPort, result.hostOverride)
		// 重新推导 cfgPath（用户可能在向导里改了 dataDir）
		*cfgPath = filepath.Join(result.dataDir, "config.json")
	}

	if err := runServer(*cfgPath); err != nil {
		die("%v", err)
	}
}

// runServer 加载配置并启动 HTTP 服务。
// 阻塞运行，直到收到中断信号（Ctrl+C）优雅关闭，或服务启动出错返回。
// 供 serve 子命令与交互式菜单共同调用。
func runServer(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if cfg.PushSecret == "" {
		return fmt.Errorf("config.json 中 push_secret 不能为空，请先运行配置向导")
	}
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 初始化各模块
	st := store.New(cfg.ServersDir())
	sessMgr := session.New(cfg)
	rconMgr, err := rconcfg.New(cfg.RconPath())
	if err != nil {
		return fmt.Errorf("加载 rcon.json 失败: %w", err)
	}
	if cfg.ProbeIntervalSec > 0 {
		// 存活探测（默认开启）：主动探测 TCP 端口，休眠中的服务器
		// （引擎休眠导致 SourceMod/插件暂停，无推送数据）只要进程存活就能保持在线；
		// 探测不可达才标记离线。探测地址与 RCON 解析一致（host_override 优先）。
		go probeLoop(st, rconMgr, cfg)
	} else {
		// 未开启探测时回退推送超时兜底扫描
		st.StartSweep(cfg.OfflineAfterDuration())
	}
	// 连续离线自动清理：超过 offline_cleanup_min 分钟持续离线的服务器自动清空文件与记录
	st.StartOfflineCleanup(60*time.Second, cfg.OfflineCleanupDuration())

	// 站点权限层（data/permissions.json；不存在自动生成）
	permStore, err := perm.New(cfg.PermPath())
	if err != nil {
		log.Printf("[perm] 警告: %v（使用默认权限配置，预设管理默认仅最高权限者可用）", err)
	}

	apiSrv := &api.Server{
		Cfg:        cfg,
		Store:      st,
		Session:    sessMgr,
		RconCfg:    rconMgr,
		MixPresets: store.NewMixmapPresetStore(cfg.MixmapPresetsDir()),
		Perm:       permStore,
	}

	// 在线地图元数据（可选）。拉取失败不阻塞启动。
	if cfg.OnlineMapURL != "" {
		om := onlinemap.New(cfg.OnlineMapURL, cfg.OnlineMapCachePath())
		if _, err := om.FetchAndParse(); err != nil {
			log.Printf("[main] 在线地图初始拉取失败: %v", err)
		}
		om.StartScheduler(cfg.OnlineMapRefreshH)
		apiSrv.OnlineMap = om
	}

	// 路由
	mux := http.NewServeMux()
	apiSrv.Register(mux)

	// 静态前端：优先磁盘 WebDir，不存在时回退到内嵌资源，实现单文件分发
	webFS, webDirDesc := resolveWebFS(cfg.WebDir)
	fsRoot := http.FileServer(http.FS(webFS))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fsRoot))
	// 根路径指向 index.html（也优先磁盘、回退内嵌）
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})

	// 启动 session 后台清理
	sessMgr.StartCleanup(cfg.CleanupDuration())

	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		mux.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           loggedMux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 捕获中断信号用于优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// ListenAndServe 在独立 goroutine 运行，主流程 select 等待信号或启动错误
	srvErr := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		srvErr <- err
	}()

	// 打印醒目的访问地址
	printServeBanner(cfg, webDirDesc)

	select {
	case err := <-srvErr:
		// 未经关闭即返回，多为端口被占用等启动错误
		sessMgr.Stop()
		if err != nil {
			return fmt.Errorf("HTTP 服务失败: %w", err)
		}
		return nil
	case <-sigCh:
		fmt.Println()
		log.Println("正在关闭服务…")
		sessMgr.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("关闭超时或出错: %v", err)
		}
		<-srvErr // 等待 ListenAndServe 返回
		log.Println("服务已停止")
		return nil
	}
}

// --- 工具 ---

// probeLoop 周期探测所有服务器的 TCP 端口（= 游戏/RCON 端口）存活状态：
//   - Alive（TCP 连接成功；休眠中的服务器由内核完成握手，照样可达）→ 在线
//   - Dead（connection refused / no route to host，端口无监听或主机不可达）→
//     连续 ProbeFailThreshold 次判离线（防偶发抖动）
//   - Error（SYN 超时/DNS 等，可能网络配置问题）→ 不动状态，避免误判全离线
//
// 探测地址与 RCON 连接使用同一套 host 解析（rcon.json 的 host_override 优先，
// 否则用插件推送的 host），保证"后端能连上 RCON 的地址"= "探测的地址"，
// 跨服务器部署（后端与游戏服不在同一机/同一 Docker 网）时天然一致。
//
// 启动后立即执行第一轮，随后按 interval 周期执行；
// 并发探测（上限 8），结果在主协程串行落库，避免并发写缓存。
func probeLoop(st *store.Store, rconCfg *rconcfg.Manager, cfg *config.Config) {
	interval := cfg.ProbeInterval()
	timeout := cfg.ProbeTimeout()
	threshold := cfg.ProbeFailThreshold
	if threshold < 1 {
		threshold = 1
	}
	failures := make(map[string]int)

	for {
		targets := st.ProbeTargets()
		if len(targets) > 0 {
			seen := make(map[string]bool, len(targets))
			for _, t := range targets {
				seen[t.Key] = true
			}

			type outcome struct {
				key    string
				result query.Result
			}
			results := make([]outcome, len(targets))
			var wg sync.WaitGroup
			sem := make(chan struct{}, 8)
			for i, t := range targets {
				wg.Add(1)
				sem <- struct{}{}
				go func(i int, t store.ProbeTarget) {
					defer wg.Done()
					defer func() { <-sem }()
					// 探测地址 = RCON 地址：host_override 优先，否则插件推送的 host
					host := rconCfg.GetHost(t.Key, t.Host)
					res, err := query.Probe(host, t.Port, timeout)
					if err != nil && res == query.Error {
						log.Printf("[probe] %s 探测出错: %v", t.Key, err)
					}
					results[i] = outcome{key: t.Key, result: res}
				}(i, t)
			}
			wg.Wait()

			for _, o := range results {
				switch o.result {
				case query.Alive:
					failures[o.key] = 0
					st.ProbeOnline(o.key)
				case query.Dead:
					failures[o.key]++
					if failures[o.key] >= threshold {
						st.MarkOffline(o.key)
					}
				}
			}

			// 清理已被自动清除记录的 key 的失败计数，避免残留计数影响重新注册的服务器
			for k := range failures {
				if !seen[k] {
					delete(failures, k)
				}
			}
		}
		time.Sleep(interval)
	}
}

// resolveWebFS 解析静态前端文件系统：优先磁盘 WebDir，不存在则回退到内嵌资源。
// 返回供 http.FileServer 使用的 fs.FS，以及用于日志展示的描述字符串。
func resolveWebFS(webDir string) (fs.FS, string) {
	if webDir != "" {
		if abs, err := filepath.Abs(webDir); err == nil {
			if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
				return os.DirFS(abs), abs
			}
		}
	}
	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		// 不应发生：//go:embed all:web 保证 "web" 子树存在
		return embeddedWeb, "(embedded:web)"
	}
	return sub, "(embedded)"
}

func writeJSONFile(path string, v interface{}) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func die(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "✗ "+format+"\n", a...)
	os.Exit(1)
}

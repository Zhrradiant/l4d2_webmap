// launcher.go 实现「双击即用」的交互式终端控制台。
//
// 不带任何参数运行 webmap（例如在资源管理器里双击 webmap.exe）时进入本菜单，
// 用编号选择完成：启动服务、配置向导、编辑 RCON 密码、查看运行状态、
// 查看插件侧应填写的配置——无需记忆任何命令行参数。
//
// 仅使用 Go 标准库，保持单二进制、零第三方依赖。
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"webmap/internal/config"
	"webmap/internal/rconcfg"
	"webmap/internal/store"
)

// defaultCfgPath 菜单默认使用的配置文件路径（相对当前工作目录）。
// 双击 exe 时工作目录即 exe 所在目录，data 亦位于其下，二者一致。
const defaultCfgPath = "data/config.json"

// appVersion / appAuthor 用于终端菜单与启动横幅的署名展示。
const (
	appVersion = "0.1.2"
	appAuthor  = "Zhrradiant"
)

// =====================================================================
// 主菜单循环
// =====================================================================

// runLauncher 进入交互式主菜单，循环展示状态与操作，直到用户选择退出。
func runLauncher() {
	colorEnabled = enableVirtualTerminal()
	reader := bufio.NewReader(os.Stdin)
	cfgPath := defaultCfgPath

	for {
		clearScreen()
		printLauncherHeader()
		printStatusSummary(cfgPath)
		printMenu()

		fmt.Print("请输入选项编号: ")
		line, err := reader.ReadString('\n')
		choice := strings.ToLower(strings.TrimSpace(line))
		if err != nil && choice == "" {
			// stdin 关闭（EOF）或读取出错且无输入时退出，避免空转刷屏
			fmt.Println(dim("已退出。"))
			return
		}
		switch choice {
		case "1":
			launcherStart(reader, cfgPath)
		case "2":
			cfgPath = launcherConfigure(reader, cfgPath)
		case "3":
			launcherEditRcon(reader, cfgPath)
		case "4":
			launcherStatus(reader, cfgPath)
		case "5":
			launcherPluginInfo(reader, cfgPath)
		case "6":
			launcherSelectiveConfig(reader, cfgPath)
		case "0", "q", "quit", "exit":
			fmt.Println(dim("已退出。"))
			return
		case "":
			// 空输入，直接重绘菜单
		default:
			fmt.Println(warn("无效选项，请重新选择。"))
			pause(reader)
		}
	}
}

// =====================================================================
// 界面绘制
// =====================================================================

func printLauncherHeader() {
	fmt.Println(accent(hr()))
	fmt.Println(accent("   L4D2 WebMap  ·  后端控制台"))
	fmt.Println(dim(fmt.Sprintf("   v%s  ·  by %s", appVersion, appAuthor)))
	fmt.Println(accent(hr()))
	fmt.Println()
}

// printStatusSummary 展示当前配置概览，帮助用户一眼判断该做什么。
func printStatusSummary(cfgPath string) {
	fmt.Println(bold("当前状态"))
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Printf("  配置文件 : %s  %s\n", cfgPath, warn("尚未配置"))
		fmt.Println(dim("  提示：请先选择『2』运行配置向导完成初始化。"))
		fmt.Println()
		return
	}
	fmt.Printf("  配置文件 : %s  %s\n", cfgPath, ok("已就绪"))
	fmt.Printf("  监听地址 : %s\n", cfg.Listen)
	fmt.Printf("  数据目录 : %s\n", cfg.DataDir)
	fmt.Printf("  推送密钥 : %s\n", maskSecret(cfg.PushSecret))
	if mgr, err := rconcfg.New(cfg.RconPath()); err == nil {
		snap := mgr.Snapshot()
		switch snap.Mode {
		case "unified":
			fmt.Printf("  RCON 模式: %s\n", "unified（全服统一密码）")
		case "per_port":
			fmt.Printf("  RCON 模式: %s\n", fmt.Sprintf("per_port（%d 个端口密码）", len(snap.PerPort)))
		default:
			fmt.Printf("  RCON 模式: %s\n", snap.Mode)
		}
	}
	fmt.Printf("  在线地图CSV: %s\n", csvStatus(cfg.OnlineMapURL))
	fmt.Printf("  前端背景    : %s\n", bgModeLabel(cfg.BgMode))
	fmt.Printf("  已登记服务器: %d 台\n", len(store.New(cfg.ServersDir()).List()))
	fmt.Println()
}

func printMenu() {
	fmt.Println(bold("请选择操作"))
	fmt.Println("  1) " + accent("启动服务") + dim("  （前台运行，Ctrl+C 停止并返回菜单）"))
	fmt.Println("  2) " + accent("配置向导") + dim("  （监听端口 / 推送密钥 / 在线地图CSV / RCON 密码）"))
	fmt.Println("  3) " + accent("编辑 RCON 密码"))
	fmt.Println("  4) " + accent("查看运行状态与已连接服务器"))
	fmt.Println("  5) " + accent("查看插件侧应填写的配置"))
	fmt.Println("  6) " + accent("选择性配置") + dim("  （自选要修改的项，无需全部重配）"))
	fmt.Println("  0) " + dim("退出"))
	fmt.Println()
}

// =====================================================================
// 各菜单动作
// =====================================================================

// launcherStart 启动 HTTP 服务（阻塞至 Ctrl+C 后返回菜单）。
func launcherStart(reader *bufio.Reader, cfgPath string) {
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Println(warn("尚未找到配置文件，请先选择『2』运行配置向导。"))
		pause(reader)
		return
	}
	fmt.Println(dim("正在启动服务……"))
	if err := runServer(cfgPath); err != nil {
		fmt.Println()
		fmt.Println(danger("服务已退出：" + err.Error()))
	}
	pause(reader)
}

// launcherConfigure 运行配置向导，生成 config.json / rcon.json。
// 返回（可能更新后的）配置文件路径。
func launcherConfigure(reader *bufio.Reader, cfgPath string) string {
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Println(warn("已存在配置，继续将重新生成并覆盖 config.json / rcon.json。"))
		if strings.ToLower(prompt(reader, "继续？(y/n)", "y")) != "y" {
			return cfgPath
		}
	}
	result := runWizard(reader)
	generateConfig(result.dataDir, result.listen, result.secret, result.rconMode, result.rconPass, result.onlineMapURL, result.bgMode, result.bgURL, result.perPort, result.hostOverride)
	newPath := filepath.Join(result.dataDir, "config.json")
	pause(reader)
	return newPath
}

// launcherEditRcon 单独修改 RCON 密码配置（读现有 rcon.json → 交互重设 → 写回）。
func launcherEditRcon(reader *bufio.Reader, cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Println(warn("尚未配置，请先选择『2』运行配置向导。"))
		pause(reader)
		return
	}
	rconPath := cfg.RconPath()
	mgr, err := rconcfg.New(rconPath)
	if err != nil {
		fmt.Println(danger("加载 rcon.json 失败：" + err.Error()))
		pause(reader)
		return
	}
	cur := mgr.Snapshot()
	fmt.Println()
	fmt.Printf("当前 RCON 模式：%s\n", accent(cur.Mode))
	fmt.Println(dim("浏览器不直接接触 RCON，密码只存后端 rcon.json，不会下发给浏览器。"))

	idx := promptChoice(reader, "选择密码模式", []string{
		"unified  - 全部端口共用一个密码",
		"per_port - 每个端口各自密码",
	})

	newCfg := &rconcfg.Config{
		PerPort:      map[string]string{},
		HostOverride: cur.HostOverride,
	}
	if newCfg.HostOverride == nil {
		newCfg.HostOverride = map[string]string{}
	}
	if idx == 0 {
		newCfg.Mode = "unified"
		newCfg.UnifiedPassword = prompt(reader, "RCON 密码 (所有端口共用)", cur.UnifiedPassword)
	} else {
		newCfg.Mode = "per_port"
		fmt.Println("输入每个端口的密码（输入空行结束）：")
		for {
			port := prompt(reader, "端口号 (如 27015，留空结束)", "")
			if port == "" {
				break
			}
			newCfg.PerPort[port] = prompt(reader, "端口 "+port+" 的 RCON 密码", cur.PerPort[port])
		}
	}

	if err := mgr.Save(newCfg); err != nil {
		fmt.Println(danger("保存失败：" + err.Error()))
	} else {
		fmt.Println(ok("✓ RCON 配置已保存至 " + rconPath))
	}
	pause(reader)
}

// launcherStatus 查看运行参数与已登记的游戏服务器（读取 data/servers/*.json）。
func launcherStatus(reader *bufio.Reader, cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Println(warn("尚未配置，请先选择『2』运行配置向导。"))
		pause(reader)
		return
	}
	fmt.Println()
	fmt.Println(bold("运行参数"))
	fmt.Printf("  监听地址 : %s\n", cfg.Listen)
	fmt.Printf("  数据目录 : %s\n", cfg.DataDir)

	fmt.Println()
	fmt.Println(bold("访问地址（服务运行时在浏览器打开）"))
	for _, u := range accessURLs(cfg.Listen) {
		fmt.Printf("  %s\n", ok(u))
	}

	list := store.New(cfg.ServersDir()).List()
	fmt.Println()
	fmt.Printf("%s（共 %d 台）\n", bold("已登记的游戏服务器"), len(list))
	if len(list) == 0 {
		fmt.Println(dim("  暂无。游戏服务器安装插件并上报后会自动出现在这里。"))
	} else {
		for _, s := range list {
			status := danger("● 离线")
			if s.Online {
				status = ok("● 在线")
			}
			name := s.Name
			if name == "" {
				name = "(未命名)"
			}
			curMap := s.CurrentMap
			if curMap == "" {
				curMap = "-"
			}
			fmt.Printf("  %s  %s  key=%s  当前地图=%s\n", status, name, s.ServerKey, curMap)
		}
	}
	pause(reader)
}

// launcherPluginInfo 展示游戏服务器 webmap.cfg 里应填写的内容，方便复制。
func launcherPluginInfo(reader *bufio.Reader, cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Println(warn("尚未配置，请先选择『2』运行配置向导。"))
		pause(reader)
		return
	}
	port := listenPort(cfg.Listen)
	urls := accessURLs(cfg.Listen)

	// 优先取一个非本机回环地址作为 backend_url 示例
	backend := fmt.Sprintf("http://<后端IP>:%s", port)
	for _, u := range urls {
		if !strings.Contains(u, "127.0.0.1") {
			backend = strings.TrimSuffix(u, "/")
			break
		}
	}

	fmt.Println()
	fmt.Println(bold("在游戏服务器 cfg/sourcemod/webmap.cfg 中填写："))
	fmt.Println()
	fmt.Printf("  webmap_backend_url  %s\n", accent("\""+backend+"\""))
	fmt.Printf("  webmap_push_secret  %s\n", accent("\""+cfg.PushSecret+"\""))
	fmt.Printf("  webmap_server_key   %s\n", "\"cn-01\"   "+dim("（多台服务器时各填唯一值）"))
	fmt.Println()
	fmt.Println(dim("  push_secret 必须与后端完全一致；改动后在游戏内执行 sm_webmap_reload。"))

	if len(urls) > 1 {
		fmt.Println()
		fmt.Println(dim("  本机可用地址（选游戏服务器能访问到的那个）："))
		for _, u := range urls {
			fmt.Printf("    %s\n", strings.TrimSuffix(u, "/"))
		}
	}
	pause(reader)
}

// launcherSelectiveConfig 选择性配置：列出所有可配置项让用户自选要修改哪些，无需全部重配。
func launcherSelectiveConfig(reader *bufio.Reader, cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Println(warn("尚未配置，请先选择『2』运行配置向导。"))
		pause(reader)
		return
	}

	for {
		clearScreen()
		printLauncherHeader()
		fmt.Println(bold("选择性配置 —— 选择要修改的项"))
		fmt.Println(dim("输入编号（多个用逗号分隔，如 1,3），0 返回并保存"))
		fmt.Println()

		// 展示当前值
		fmt.Printf("  1) %s : %s\n", bold("监听地址"), cfg.Listen)
		fmt.Printf("  2) %s : %s\n", bold("推送密钥"), maskSecret(cfg.PushSecret))
		fmt.Printf("  3) %s : %s\n", bold("在线地图CSV"), csvStatus(cfg.OnlineMapURL))
		fmt.Printf("  4) %s : %s\n", bold("前端背景"), bgModeLabel(cfg.BgMode))
		if cfg.BgMode == "custom" {
			fmt.Printf("      背景链接 : %s\n", cfg.BgURL)
		}

		// RCON 状态
		rconPath := cfg.RconPath()
		mgr, rconErr := rconcfg.New(rconPath)
		if rconErr == nil {
			snap := mgr.Snapshot()
			switch snap.Mode {
			case "unified":
				fmt.Printf("  5) %s : unified（全服统一密码: %s）\n", bold("RCON 密码"), maskSecret(snap.UnifiedPassword))
			case "per_port":
				fmt.Printf("  5) %s : per_port（%d 个端口密码）\n", bold("RCON 密码"), len(snap.PerPort))
			default:
				fmt.Printf("  5) %s : %s\n", bold("RCON 密码"), snap.Mode)
			}
		} else {
			fmt.Printf("  5) %s : %s\n", bold("RCON 密码"), warn("加载失败"))
		}

		fmt.Println()
		fmt.Printf("  0) %s\n", dim("保存并返回主菜单"))

		fmt.Println()
		line := prompt(reader, "要修改的项编号", "")
		line = strings.TrimSpace(line)
		if line == "" || line == "0" {
			// 保存 config.json
			if err := writeJSONFile(cfgPath, cfg); err != nil {
				fmt.Println(danger("保存 config.json 失败：" + err.Error()))
			} else {
				fmt.Println(ok("✓ 配置已保存至 " + cfgPath))
			}
			pause(reader)
			return
		}

		// 解析编号
		choices := parseChoiceSet(line)
		if len(choices) == 0 {
			fmt.Println(warn("无效输入，请输入编号（如 1,3,5）"))
			pause(reader)
			continue
		}

		anyChanged := false
		for _, ch := range choices {
			switch ch {
			case 1:
				fmt.Println()
				fmt.Printf("当前监听地址: %s\n", cfg.Listen)
				cfg.Listen = normalizeListen(prompt(reader, "新监听地址", cfg.Listen))
				anyChanged = true
			case 2:
				fmt.Println()
				fmt.Printf("当前推送密钥: %s\n", maskSecret(cfg.PushSecret))
				fmt.Println(dim("用于校验游戏服务器上报请求，须与插件 webmap_push_secret 一致。"))
				newSec := prompt(reader, "新推送密钥（回车自动生成 24 位随机密钥）", "")
				if newSec == "" {
					newSec = randHex(24)
					fmt.Printf("已自动生成: %s\n", newSec)
				}
				cfg.PushSecret = newSec
				anyChanged = true
			case 3:
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
						cfg.OnlineMapURL = config.DefaultOnlineMapURL
					} else {
						cfg.OnlineMapURL = input
					}
				} else {
					cfg.OnlineMapURL = ""
				}
				anyChanged = true
			case 4:
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
					cfg.BgMode = "default"
					cfg.BgURL = config.DefaultBgURL
					fmt.Printf("默认图链：%s\n", config.DefaultBgURL)
				case 1:
					cfg.BgMode = "custom"
					cfg.BgURL = prompt(reader, "自定义图片链接（支持 http/https URL）", cfg.BgURL)
				case 2:
					cfg.BgMode = "none"
					cfg.BgURL = ""
				}
				anyChanged = true
			case 5:
				// 委托给已有的 RCON 编辑逻辑
				launcherEditRcon(reader, cfgPath)
				anyChanged = true
			}
		}

		if !anyChanged {
			continue
		}

		// 每轮修改后提示并允许继续选择
		fmt.Println()
		fmt.Println(ok("✓ 已修改选定项。可继续选择其他项，或输入 0 保存返回。"))
		pause(reader)
	}
}

// parseChoiceSet 解析逗号分隔的数字字符串为去重整数集合。
// 例如 "1,3,5" → [1,3,5]，忽略空和无效数字。
func parseChoiceSet(s string) []int {
	parts := strings.Split(s, ",")
	seen := map[int]bool{}
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 1 || n > 5 {
			continue
		}
		if !seen[n] {
			seen[n] = true
			result = append(result, n)
		}
	}
	return result
}

// =====================================================================
// 启动横幅（runServer 调用）
// =====================================================================

// printServeBanner 在服务启动后打印醒目的访问地址清单。
func printServeBanner(cfg *config.Config, webDir string) {
	fmt.Println()
	fmt.Println(accent(hr()))
	fmt.Println(accent("  WebMap 后端已启动 —— 按 Ctrl+C 停止"))
	fmt.Println(dim(fmt.Sprintf("  v%s  ·  by %s", appVersion, appAuthor)))
	fmt.Println(accent(hr()))
	fmt.Printf("  监听地址 : %s\n", cfg.Listen)
	fmt.Printf("  数据目录 : %s\n", cfg.DataDir)
	fmt.Printf("  前端来源 : %s\n", webDir)
	fmt.Println()
	fmt.Println(bold("  在浏览器打开以下任一地址："))
	for _, u := range accessURLs(cfg.Listen) {
		fmt.Printf("    %s\n", ok(u))
	}
	fmt.Println(accent(hr()))
	fmt.Println()
}

// =====================================================================
// 网络辅助
// =====================================================================

// listenPort 从监听地址（":11223" / "0.0.0.0:11223" / "127.0.0.1:11223"）提取端口。
func listenPort(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[i+1:]
	}
	return listen
}

// listenHost 从监听地址提取主机部分（可能为空，表示监听所有网卡）。
func listenHost(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[:i]
	}
	return ""
}

// accessURLs 根据监听地址推导可在浏览器打开的地址列表。
func accessURLs(listen string) []string {
	port := listenPort(listen)
	host := listenHost(listen)
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{fmt.Sprintf("http://%s:%s/", host, port)}
	}
	urls := []string{fmt.Sprintf("http://127.0.0.1:%s/", port)}
	for _, ip := range localIPv4s() {
		urls = append(urls, fmt.Sprintf("http://%s:%s/", ip, port))
	}
	return urls
}

// localIPv4s 返回本机所有非回环 IPv4 地址。
func localIPv4s() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			out = append(out, ip4.String())
		}
	}
	return out
}

// =====================================================================
// 颜色 / 清屏辅助（VT 启用后生效，否则降级为纯文本）
// =====================================================================

// colorEnabled 由 enableVirtualTerminal() 的结果决定是否输出 ANSI 颜色。
var colorEnabled bool

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiOrange = "\033[38;5;208m" // 近似「求生橙」
)

func colorize(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

func accent(s string) string { return colorize(ansiOrange, s) }
func ok(s string) string     { return colorize(ansiGreen, s) }
func warn(s string) string   { return colorize(ansiYellow, s) }
func danger(s string) string { return colorize(ansiRed, s) }
func dim(s string) string    { return colorize(ansiDim, s) }
func bold(s string) string   { return colorize(ansiBold, s) }

func hr() string { return strings.Repeat("═", 48) }

// clearScreen 清屏（VT 可用时用转义序列，否则打印空行分隔）。
func clearScreen() {
	if colorEnabled {
		fmt.Print("\033[H\033[2J")
	} else {
		fmt.Print("\n\n")
	}
}

// pause 等待用户回车后返回菜单。
func pause(reader *bufio.Reader) {
	fmt.Print(dim("\n按回车返回菜单… "))
	_, _ = reader.ReadString('\n')
}

// csvStatus 展示在线地图 CSV 地址的简短状态。
func csvStatus(url string) string {
	if url == "" {
		return dim("未配置")
	}
	// 截短展示：只保留域名 + 最后一段文件名
	const maxLen = 64
	if len(url) <= maxLen {
		return ok(url)
	}
	return ok(url[:maxLen-3] + "...")
}

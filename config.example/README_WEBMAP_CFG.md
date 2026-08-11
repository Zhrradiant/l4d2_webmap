// WebMap 插件配置 (AutoExecConfig 自动生成于 cfg/sourcemod/webmap.cfg)
// 修改后在服务器控制台执行 sm plugins reload l4d2_webmap 或换图生效

// 后端地址（指向你部署的 Go 后端）
webmap_backend_url "http://127.0.0.1:11223"

// 服务器唯一标识（每个服不同，用于后端区分）
// 留空或保持 "default" 时，自动用「对外IP_端口」作为唯一标识（如 1.2.3.4_27015）
webmap_server_key "default"

// 服务器显示名（展示在网页上）
// 留空则自动用游戏 hostname；仍为空时网页回退显示 server_key
webmap_server_name ""

// 推送鉴权密钥（必须与后端 config.json 的 push_secret 完全一致）
webmap_push_secret ""

// 验证码有效期（秒）
webmap_code_ttl "300"

// 验证码位数（4）
webmap_code_length "4"

// 对外连接 IP（供后端 RCON 连接，留空则自动探测 hostip）
// 多服部署建议显式填写
webmap_host ""

// 对外连接端口（0 = 使用 hostport）
webmap_port "0"

// 投票通过判定模式
//   0 = 宽松（旧逻辑）：仅需赞成多于反对 (yes > no)，未投票忽略
//       场景：1 投赞成、0 投反对、其余未投 → 通过换图
//   1 = 严格：赞成必须严格超过「反对 + 未投票」(yes*2 > PlayerCount)
//       即赞成票必须过半数才通过，未投票视作反对
//       场景：2 人可投，需 2 人都投赞成才换图；任意一人不投即不通过
//   线上更改后即时生效，无需重载插件
webmap_vote_pass_mode "0"

// 是否启用终局评分弹窗
webmap_rating_enabled "1"

// 自动触发评分的时机（三选一）
//   0 = 终局章节开始（round_start，默认）：会话 + 事件驱动逐人投递，玩家进服后陆续补发
//   1 = 触发终局救援（FinaleStart）：延迟后全员扫描一次，菜单被占用可重试
//   2 = 确认救援成功（finale_vehicle_leaving）：撤离瞬间一次性弹，不重试（被占用即丢弃）
webmap_rating_trigger "0"

// 触发后延迟秒数再进行判定 / 弹菜单（路径 0/1/2 通用）
webmap_rating_delay "3.0"

// 路径0 专用：delay 到点 finale 仍未就绪时的最多重排次数
webmap_rating_finale_recheck_count "5"

// 路径0 专用：玩家就绪后首次显示等待秒数（独立于 delay）
webmap_rating_first_delay "2.0"

// 路径0 专用：首次检查（不计数重试）的最大次数，防无限重排
webmap_rating_first_max "10"

// 菜单被占用时的计数重试间隔秒数（路径 0 / 路径 1）
webmap_rating_menu_retry_interval "3.0"

// 最大计数重试次数；路径 2 不重试
webmap_rating_menu_retry_count "20"

// 菜单超时（秒）
webmap_rating_menu_time "30"

// 官方图是否评分（0=仅三方地图）
webmap_rating_official "0"

// 标题是否显示均分
webmap_rating_show_avg "1"

// 旁观者是否弹出评分
webmap_rating_spectators "0"

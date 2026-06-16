// Package service — Telegram Bot 交互命令服务。
//
// 处理通过 Telegram Bot API 接收的用户命令，提供系统状态查询、
// 媒体搜索、下载管理等功能。同时支持 Webhook 和 Long Polling 两种模式。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// TelegramUpdate 是 Telegram Bot API 推送的 update 对象。
type TelegramUpdate struct {
	UpdateID      int                    `json:"update_id"`
	Message       *TelegramMessage       `json:"message,omitempty"`
	CallbackQuery *TelegramCallbackQuery `json:"callback_query,omitempty"`
}

// TelegramMessage 是 Telegram 消息对象。
type TelegramMessage struct {
	MessageID int          `json:"message_id"`
	From      TelegramUser `json:"from"`
	Chat      TelegramChat `json:"chat"`
	Text      string       `json:"text,omitempty"`
	Date      int          `json:"date"`
}

type TelegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    TelegramUser     `json:"from"`
	Message *TelegramMessage `json:"message,omitempty"`
	Data    string           `json:"data,omitempty"`
}

// TelegramUser 是 Telegram 用户对象。
type TelegramUser struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

// TelegramChat 是 Telegram 聊天对象。
type TelegramChat struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}

type telegramCommandReply struct {
	Text    string
	Buttons [][]telegramInlineButton
}

type telegramInlineButton struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

// TelegramBotService 处理 Telegram Bot 的交互命令。
type TelegramBotService struct {
	log    *zap.Logger
	repo   *repository.Container
	crypto *CryptoService
	auth   *AuthService
	device *DeviceService
	backup *BackupService

	pollingMu     sync.Mutex
	pollingCancel map[string]context.CancelFunc // bot_token -> cancel

	pendingMu sync.Mutex
	pending   map[int64]pendingInput // telegram_user_id -> awaited text input
}

// pendingInput tracks a button-initiated action that awaits the user's next
// text message (e.g. tapping「注册」then sending "用户名 密码").
type pendingInput struct {
	Kind      string // register / redeem_register / redeem_renew / setname / setpass / openreg_limit / gencode_user
	CreatedAt time.Time
}

// SetDeviceService wires the device-management service used by the device
// menu (list / kick) and enforcement notifications.
func (s *TelegramBotService) SetDeviceService(d *DeviceService) { s.device = d }

// SetBackupService wires database backup/restore commands.
func (s *TelegramBotService) SetBackupService(b *BackupService) { s.backup = b }

// NotifyUserByID sends a Telegram message to the local user identified by
// userID, resolved through their Telegram binding. Used by enforcement to warn
// users before destructive actions. No-op when the user has no binding.
func (s *TelegramBotService) NotifyUserByID(ctx context.Context, userID, text string) {
	if userID == "" || strings.TrimSpace(text) == "" {
		return
	}
	var binding model.TelegramBinding
	if err := s.repo.DB.WithContext(ctx).Where("user_id = ?", userID).First(&binding).Error; err != nil {
		return
	}
	targetChatID := telegramPrivateChatIDFromBinding(binding)
	if targetChatID == 0 {
		return
	}
	channel := s.findChannelByChatID(ctx, int(binding.ChatID))
	if channel == nil {
		channels, err := s.repo.NotifyChannel.ListByType(ctx, "telegram")
		if err != nil || len(channels) == 0 {
			return
		}
		channel = &channels[0]
	}
	_ = s.reply(ctx, channel, int(targetChatID), telegramCommandReply{Text: text})
}

// NewTelegramBotService 创建 Telegram Bot 服务。
func NewTelegramBotService(log *zap.Logger, repo *repository.Container, crypto *CryptoService, auth *AuthService) *TelegramBotService {
	return &TelegramBotService{
		log:           log,
		repo:          repo,
		crypto:        crypto,
		auth:          auth,
		pollingCancel: make(map[string]context.CancelFunc),
		pending:       make(map[int64]pendingInput),
	}
}

// TelegramRegistrationSettingKey 控制普通用户是否可以通过 Bot 注册新账号。
// 默认关闭，只有管理员在系统设置 / Bot 管理命令中显式开启后才允许注册。
const TelegramRegistrationSettingKey = "telegram.registration_enabled"

var errTelegramAccountAlreadyBound = errors.New("该媒体账号已绑定其他 Telegram，请联系管理员解绑")

// registrationEnabled 读取注册开关；默认关闭。
func (s *TelegramBotService) registrationEnabled(ctx context.Context) bool {
	v, err := s.repo.Setting.Get(ctx, TelegramRegistrationSettingKey)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

// setRegistrationEnabled 持久化注册开关。
func (s *TelegramBotService) setRegistrationEnabled(ctx context.Context, enabled bool) error {
	return s.repo.Setting.Set(ctx, TelegramRegistrationSettingKey, strconv.FormatBool(enabled))
}

// HandleWebhook 处理 Telegram 推送的 Webhook/Polling 消息。
func (s *TelegramBotService) HandleWebhook(ctx context.Context, body []byte) error {
	var update TelegramUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		return fmt.Errorf("invalid update: %w", err)
	}
	return s.handleTelegramUpdate(ctx, update, nil)
}

func (s *TelegramBotService) handleTelegramUpdate(ctx context.Context, update TelegramUpdate, channelHint *model.NotifyChannel) error {
	if update.CallbackQuery != nil {
		return s.handleCallback(ctx, update.CallbackQuery, channelHint)
	}

	if update.Message == nil || update.Message.Text == "" {
		return nil
	}

	msg := update.Message
	text := strings.TrimSpace(msg.Text)

	// Button-initiated text prompts (register / redeem / change name·password /
	// open-reg limit) arrive as ordinary messages. Consume them here before the
	// command gate so the button-driven menu can collect free-form input.
	if !telegramIsCommandText(text) {
		if msg.Chat.Type == "" || msg.Chat.Type == "private" {
			if channel := s.channelForMessage(ctx, msg, channelHint); channel != nil {
				if reply, handled := s.handlePendingText(ctx, channel, msg, text); handled {
					if reply.Text != "" {
						if err := s.reply(ctx, channel, msg.Chat.ID, reply); err != nil {
							s.log.Error("reply failed", zap.Error(err))
						}
					}
					s.deleteTelegramSourceMessage(channel, msg.Chat.ID, msg.MessageID)
					return nil
				}
				if looksLikeRedemptionCode(text) {
					reply := s.cmdRedeem(ctx, channel, msg, []string{text})
					if reply.Text != "" {
						if err := s.reply(ctx, channel, msg.Chat.ID, reply); err != nil {
							s.log.Error("reply failed", zap.Error(err))
						}
					}
					s.deleteTelegramSourceMessage(channel, msg.Chat.ID, msg.MessageID)
					return nil
				}
			}
		}
		return nil
	}
	if msg.Chat.Type != "" && msg.Chat.Type != "private" && !telegramSupportedCommand(telegramCommandName(text)) {
		return nil
	}

	s.log.Info("telegram command received",
		zap.Int("chat_id", msg.Chat.ID),
		zap.String("user", msg.From.Username),
		zap.String("text", text),
	)

	// 获取该消息可使用的 Telegram 通知渠道配置。群组/频道消息必须来自
	// 已配置的群组/频道；私聊消息会选择一个可验证该用户成员身份的 Bot。
	channel := s.channelForMessage(ctx, msg, channelHint)
	if channel == nil {
		s.log.Warn("telegram channel not allowed or not configured",
			zap.Int("chat_id", msg.Chat.ID),
			zap.String("chat_type", msg.Chat.Type),
			zap.Int("telegram_user_id", msg.From.ID),
		)
		return nil
	}

	// 解析并执行命令
	reply, err := s.executeCommand(ctx, channel, msg, text)
	if err != nil {
		s.log.Error("command failed", zap.Error(err))
		_ = s.replyForMessage(ctx, channel, msg, telegramCommandReply{Text: "命令执行失败: " + err.Error()})
		s.deleteTelegramSourceMessage(channel, msg.Chat.ID, msg.MessageID)
		return nil
	}

	if reply.Text != "" {
		if err := s.replyForMessage(ctx, channel, msg, reply); err != nil {
			s.log.Error("reply failed", zap.Error(err))
		}
		s.deleteTelegramSourceMessage(channel, msg.Chat.ID, msg.MessageID)
	}

	return nil
}

func telegramIsCommandText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "/") && telegramCommandName(text) != ""
}

func telegramCommandName(text string) string {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return ""
	}
	cmd := strings.ToLower(strings.TrimSpace(parts[0]))
	if !strings.HasPrefix(cmd, "/") {
		return ""
	}
	if at := strings.Index(cmd, "@"); at > 0 {
		cmd = cmd[:at]
	}
	return cmd
}

func telegramIsGroupChat(chatType string) bool {
	return chatType != "" && chatType != "private"
}

func telegramPrivateMessageForUser(msg *TelegramMessage) *TelegramMessage {
	if msg == nil || !telegramIsGroupChat(msg.Chat.Type) {
		return msg
	}
	copied := *msg
	copied.Chat = TelegramChat{ID: msg.From.ID, Type: "private"}
	return &copied
}

func telegramGroupPrivateAdminHint() string {
	return "群组内不展示管理面板；管理员可在已绑定群组直接发送文本管理命令，涉及账号凭据的操作仍请私聊 Bot。"
}

func telegramGroupPrivateUserHint(action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "此操作"
	}
	return action + "包含账号凭据或敏感信息，请私聊 Bot 操作；群组内仅开放账号状态、签到、设备与成人目录开关。"
}

func telegramGroupPrivateDeliverySentHint() string {
	return "已把你的 Bot 面板/执行结果私聊发送给你。若没收到，请先私聊 Bot 发送 <code>/start</code>。"
}

func telegramGroupPrivateDeliveryFailedHint() string {
	return "无法私聊发送给你。请先打开 Bot 私聊窗口发送 <code>/start</code>，再回群里使用命令。"
}

// cmdStart 处理 /start 命令。
func (s *TelegramBotService) cmdStart(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	name := msg.From.FirstName
	if msg.From.Username != "" {
		name = "@" + msg.From.Username
	}
	if telegramIsGroupChat(msg.Chat.Type) && len(args) > 0 {
		return telegramCommandReply{Text: telegramGroupPrivateUserHint("绑定账号")}
	}
	if len(args) == 0 {
		if binding := s.telegramBinding(ctx, msg.From.ID); binding != nil {
			user, _ := s.repo.User.FindByID(ctx, binding.UserID)
			if user == nil {
				_ = s.repo.DB.WithContext(ctx).Unscoped().Delete(&model.TelegramBinding{}, "id = ?", binding.ID).Error
				return telegramCommandReply{Text: "之前绑定的媒体中心账号已不存在，请重新绑定：\n<code>/start 用户名 密码</code>"}
			}
			status := "未隐藏"
			if user.HideAdult {
				status = "已隐藏"
			}
			return telegramCommandReply{
				Text: fmt.Sprintf("<b>MediaStationGo 已绑定</b>\n\n你好 %s，当前账号：<b>%s</b>\n成人目录：<b>%s</b>", name, userNameOrFallback(user), status),
				Buttons: [][]telegramInlineButton{{{
					Text: map[bool]string{true: "显示成人目录", false: "隐藏成人目录"}[user.HideAdult],
					Data: "adult_toggle",
				}}},
			}
		}
		hint := "如果没有账号，请联系管理员注册。"
		if s.openRegEnabled(ctx) {
			hint = "如果还没有账号，可直接注册：\n<code>/register 用户名 密码</code>\n或：<code>/register 用户名-密码</code>"
		}
		return telegramCommandReply{Text: "<b>欢迎使用 MediaStationGo</b>\n\n普通用户请先绑定账号：\n<code>/start 用户名 密码</code>\n或：<code>/start 用户名-密码</code>\n\n" + hint}
	}
	channel := s.findChannelForMessage(ctx, msg)
	if !s.telegramUserCanBind(ctx, channel, msg.From.ID) {
		return telegramCommandReply{Text: "当前 Telegram 账号不在管理员配置的绑定群组/频道中，无法绑定媒体中心账号。请先加入管理员配置的群组或频道；如果尚未配置，请联系管理员。"}
	}
	username, password := parseStartCredentials(args)
	if username == "" || password == "" {
		return telegramCommandReply{Text: "绑定格式不正确，请使用：\n<code>/start 用户名 密码</code>\n或：<code>/start 用户名-密码</code>"}
	}
	existingBinding := s.telegramBinding(ctx, msg.From.ID)
	user, err := s.repo.User.FindByUsername(ctx, username)
	if err != nil || user == nil {
		if existingBinding != nil {
			_ = s.unbindTelegramUser(ctx, msg.From.ID)
			return telegramCommandReply{Text: "当前绑定的媒体账号信息已失效，已自动解绑。请使用新的用户名和密码重新绑定。"}
		}
		return telegramCommandReply{Text: "未找到此用户，请联系管理员注册。"}
	}
	if !user.IsActive {
		return telegramCommandReply{Text: "此账号已被禁用，请联系管理员。"}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if existingBinding != nil && existingBinding.UserID == user.ID {
			_ = s.unbindTelegramUser(ctx, msg.From.ID)
			return telegramCommandReply{Text: "当前绑定账号的密码已失效，已自动解绑。请使用新密码重新绑定。"}
		}
		return telegramCommandReply{Text: "账号或密码错误。"}
	}
	if err := s.upsertTelegramBinding(ctx, msg, user.ID); err != nil {
		return telegramCommandReply{Text: "绑定失败：" + err.Error()}
	}
	return telegramCommandReply{
		Text: fmt.Sprintf("绑定成功：<b>%s</b>\n\n普通用户只能使用此 Bot 管理自己的成人目录隐藏状态；系统状态、搜索、下载和统计命令仅管理员可用。", user.Username),
		Buttons: [][]telegramInlineButton{{{
			Text: map[bool]string{true: "显示成人目录", false: "隐藏成人目录"}[user.HideAdult],
			Data: "adult_toggle",
		}}},
	}
}

// cmdRegister 处理 /register 命令：在管理员开启注册后，普通用户可通过 Bot
// 注册一个新的媒体中心账号，并自动绑定到当前 Telegram 账号。
func (s *TelegramBotService) cmdRegister(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) == 1 && looksLikeRedemptionCode(args[0]) {
		return s.redeemRegisterFlow(ctx, channel, msg, args[0])
	}
	if !s.openRegEnabled(ctx) {
		return telegramCommandReply{Text: "注册功能未开放，请联系管理员开启后再试。"}
	}
	// 开注名额已用尽则拦截（容量随凭证授权实时变化，名额单独计数）。
	if c := s.loadCapacity(ctx); c.Remaining() <= 0 {
		return telegramCommandReply{Text: "注册名额已满，请等待管理员重新开放或扩容授权。"}
	}
	if s.auth == nil {
		return telegramCommandReply{Text: "注册功能暂不可用，请联系管理员。"}
	}
	if channel == nil {
		channel = s.findChannelForMessage(ctx, msg)
	}
	if !s.telegramUserCanBind(ctx, channel, msg.From.ID) {
		return telegramCommandReply{Text: "当前 Telegram 账号不在管理员配置的绑定群组/频道中，无法注册账号。请先加入管理员配置的群组或频道；如果尚未配置，请联系管理员。"}
	}
	if binding := s.telegramBinding(ctx, msg.From.ID); binding != nil {
		if user, _ := s.repo.User.FindByID(ctx, binding.UserID); user != nil {
			return telegramCommandReply{Text: fmt.Sprintf("当前 Telegram 已绑定账号：<b>%s</b>，无需重复注册。\n如需切换账号请使用 <code>/start 用户名 密码</code>。", userNameOrFallback(user))}
		}
	}
	username, password := parseStartCredentials(args)
	if username == "" || password == "" {
		return telegramCommandReply{Text: "注册格式不正确，请使用：\n<code>/register 用户名 密码</code>\n或：<code>/register 用户名-密码</code>"}
	}
	user, _, err := s.auth.Register(ctx, username, password)
	if err != nil {
		switch {
		case errors.Is(err, ErrUsernameTaken):
			return telegramCommandReply{Text: "该用户名已被占用，请换一个；如果是你本人的账号，请改用 <code>/start 用户名 密码</code> 绑定。"}
		case errors.Is(err, ErrUserLimitReached):
			return telegramCommandReply{Text: "注册失败：已达到用户数量上限，请联系管理员。"}
		default:
			return telegramCommandReply{Text: "注册失败：" + err.Error()}
		}
	}
	// 注册成功，扣减一个开注名额（名额用尽自动关闭注册）。
	s.consumeOpenRegSlot(ctx)
	if err := s.upsertTelegramBinding(ctx, msg, user.ID); err != nil {
		return telegramCommandReply{Text: fmt.Sprintf("账号 <b>%s</b> 注册成功，但自动绑定失败：%s\n请稍后使用 <code>/start %s 密码</code> 重新绑定。", user.Username, err.Error(), user.Username)}
	}
	return telegramCommandReply{
		Text: fmt.Sprintf("注册并绑定成功：<b>%s</b>\n\n你现在可以用此账号登录网页与第三方客户端。普通用户只能在此 Bot 管理成人目录显隐；其他功能仅管理员可用。", user.Username),
		Buttons: [][]telegramInlineButton{{{
			Text: map[bool]string{true: "显示成人目录", false: "隐藏成人目录"}[user.HideAdult],
			Data: "adult_toggle",
		}}},
	}
}

// cmdRegistrationToggle handles /registration and /openreg. It uses the same
// quota-aware open-registration state as the inline Bot menu.
func (s *TelegramBotService) cmdRegistrationToggle(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		c := s.loadCapacity(ctx)
		state := "已关闭"
		if c.OpenRegOn {
			if c.OpenRegLimit > 0 {
				state = fmt.Sprintf("已开启（%d/%d 名额）", c.OpenRegUsed, c.OpenRegLimit)
			} else {
				state = "已开启（不限名额，受授权上限约束）"
			}
		}
		return telegramCommandReply{Text: fmt.Sprintf("普通用户 Bot 注册功能当前<b>%s</b>。\n剩余可注册：<b>%d</b> 人。\n\n开启：<code>/registration on 10</code>\n不限：<code>/registration on 0</code>\n关闭：<code>/registration off</code>", state, c.Remaining())}
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on", "true", "1", "open", "enable", "enabled", "开启", "打开", "开":
		limit := 0
		if len(args) > 1 {
			n, err := strconv.Atoi(strings.TrimSpace(args[1]))
			if err != nil || n < 0 {
				return telegramCommandReply{Text: "名额必须是非负整数，0 表示不限名额。"}
			}
			limit = n
		}
		if err := s.openRegistration(ctx, limit); err != nil {
			return telegramCommandReply{Text: "开启失败：" + err.Error()}
		}
		label := "不限名额"
		if limit > 0 {
			label = fmt.Sprintf("%d 个名额", limit)
		}
		return telegramCommandReply{Text: "普通用户 Bot 注册功能已开启：" + label + "。"}
	case "off", "false", "0", "close", "disable", "disabled", "关闭", "关":
		if err := s.closeRegistration(ctx); err != nil {
			return telegramCommandReply{Text: "关闭失败：" + err.Error()}
		}
		return telegramCommandReply{Text: "普通用户 Bot 注册功能已关闭。"}
	default:
		return telegramCommandReply{Text: "参数无效，请使用 <code>/registration on [名额]</code> 或 <code>/registration off</code>。"}
	}
}

// cmdHelp 处理 /help 命令。
func (s *TelegramBotService) cmdHelp(ctx context.Context, msg *TelegramMessage) string {
	channel := s.findChannelForMessage(ctx, msg)
	if telegramIsGroupChat(msg.Chat.Type) {
		adminHint := ""
		if s.telegramUserIsAdmin(ctx, channel, msg.From.ID) {
			adminHint = "\n\n管理员可在已绑定群组直接发送文本管理命令；管理面板和账号凭据操作请私聊 Bot。"
		}
		return "<b>MediaStationGo 群组可用命令</b>\n\n" +
			"<b>/menu</b> — 打开群组自助菜单\n" +
			"<b>/account</b> — 查看账号状态\n" +
			"<b>/signin</b> — 签到\n" +
			"<b>/devices</b> — 查看登录设备\n" +
			"<b>/kick all|编号</b> — 踢下线设备\n" +
			"<b>/hideadult on|off</b> — 隐藏或显示成人目录\n\n" +
			"绑定、注册、兑换、改名、改密等包含敏感信息的操作请私聊 Bot。" +
			adminHint
	}
	if !s.telegramUserIsAdmin(ctx, channel, msg.From.ID) {
		register := ""
		if s.openRegEnabled(ctx) {
			register = "<b>/register 用户名 密码</b> — 注册新账号\n"
		}
		return "<b>MediaStationGo 用户命令</b>\n\n" +
			register +
			"<b>/start 用户名 密码</b> — 绑定账号\n" +
			"<b>/account</b> — 查看账号状态\n" +
			"<b>/signin</b> — 签到\n" +
			"<b>/devices</b> — 查看登录设备\n" +
			"<b>/kick all|编号</b> — 踢下线设备\n" +
			"<b>/setname 新用户名</b> — 修改用户名\n" +
			"<b>/setpass 新密码</b> — 修改密码\n" +
			"<b>/redeem 兑换码</b> — 注册或续期兑换\n" +
			"<b>/hideadult on|off</b> — 隐藏或显示成人目录\n\n" +
			"系统状态、搜索、下载列表与统计命令仅管理员可用。"
	}
	return "<b>MediaStationGo 命令列表</b>\n\n" +
		"<b>/start</b> — 开始使用\n" +
		"<b>/help</b> — 帮助信息\n" +
		"<b>/account</b> / <b>/devices</b> / <b>/kick all|编号</b> — 用户自助设备管理\n" +
		"<b>/signin</b> / <b>/redeem 兑换码</b> — 签到与兑换\n" +
		"<b>/setname 新用户名</b> / <b>/setpass 新密码</b> — 用户自助改名改密\n" +
		"<b>/register 用户名 密码</b> — 注册新账号（需管理员开启）\n" +
		"<b>/registration on [名额]|off</b> — 开启/关闭普通用户注册（管理员）\n" +
		"<b>/capacity</b> / <b>/users</b> — 容量与用户管理（管理员）\n" +
		"<b>/gencode register|renew 天数 [有效天数]</b> — 生成兑换码（管理员）\n" +
		"<b>/renew_user 用户名 天数</b> / <b>/delete_user 用户名 confirm</b> — 续期/删除用户（管理员）\n" +
		"<b>/unbind 用户1 用户2</b> — 批量解绑 Telegram 绑定（管理员）\n" +
		"<b>/unbind_duplicates</b> / <b>/unbind_inactive 天数</b> — 清理重复/无效绑定或久未登录绑定（管理员）\n" +
		"<b>/antishare on play=3 login=3 warn=2</b> — 防共享策略（管理员）\n" +
		"<b>/cleanup run</b> — 预览保号清理候选（管理员）\n" +
		"<b>/cleanup run confirm</b> — 确认清理候选账号（管理员）\n" +
		"<b>/cleanup on|off</b> — 保号规则开关（管理员）\n" +
		"<b>/cleanup_mode any|all|count 2</b> — 保号模式（管理员）\n" +
		"<b>/cleanup_rule list|add|edit|修改|del|enable|disable</b> — Mgo 保号规则（管理员）\n" +
		"<b>/ban 用户名</b> / <b>/unban 用户名</b> — 禁用/解禁用户（管理员）\n" +
		"<b>/hideadult on|off</b> — 隐藏/显示当前绑定账号的成人目录\n" +
		"<b>/status</b> — 系统运行状态\n" +
		"<b>/search 关键词</b> — 搜索媒体库\n" +
		"<b>/downloads</b> — 下载列表\n" +
		"<b>/stats</b> — 媒体库统计\n\n" +
		"<b>Mgo 管理命令：</b>\n" +
		"用户：<code>/ucr</code> <code>/uinfo</code> <code>/rmemby</code> <code>/only_rm_record</code> <code>/renewall</code>\n" +
		"审计：<code>/userip</code> <code>/auditip</code> <code>/auditdevice</code> <code>/auditclient</code> <code>/udeviceid</code>\n" +
		"清理：<code>/syncunbound</code> <code>/syncgroupm</code> <code>/check_ex</code> <code>/deleted</code> <code>/low_activity</code>\n" +
		"权限：<code>/embyadmin</code> <code>/banall</code> <code>/unbanall</code> <code>/prouser</code> <code>/revuser</code> <code>/embylibs_blockall</code> <code>/embylibs_unblockall</code>\n" +
		"运维：<code>/proadmin</code> <code>/revadmin</code> <code>/backup_db</code> <code>/restore_from_db</code>\n\n" +
		"<b>自动推送事件：</b>\n" +
		"• 订阅命中新资源\n" +
		"• 下载任务完成\n" +
		"• 刮削失败告警\n" +
		"• 系统异常通知"
}

// cmdStatus 处理 /status 命令。
func (s *TelegramBotService) cmdHideAdult(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	channel := s.findChannelForMessage(ctx, msg)
	if !s.telegramUserCanBind(ctx, channel, msg.From.ID) {
		return telegramCommandReply{Text: "当前 Telegram 账号不在管理员配置的绑定群组/频道中，无法使用成人目录隐藏开关。"}
	}
	binding := s.telegramBinding(ctx, msg.From.ID)
	if binding == nil {
		return telegramCommandReply{Text: "请先绑定账号：<code>/start 用户名 密码</code>"}
	}
	user, err := s.repo.User.FindByID(ctx, binding.UserID)
	if err != nil || user == nil {
		return telegramCommandReply{Text: "绑定用户不存在，请重新 /start 绑定。"}
	}
	next := true
	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "off", "false", "0", "show", "显示", "关闭":
			next = false
		case "on", "true", "1", "hide", "隐藏", "开启":
			next = true
		default:
			next = !user.HideAdult
		}
	} else {
		next = !user.HideAdult
	}
	if err := s.repo.User.UpdateFields(ctx, user.ID, map[string]any{"hide_adult": next}); err != nil {
		return telegramCommandReply{Text: "更新失败：" + err.Error()}
	}
	status := map[bool]string{true: "已隐藏", false: "已显示"}[next]
	return telegramCommandReply{
		Text: "成人目录" + status + "。此设置会同步影响网页与第三方客户端。",
		Buttons: [][]telegramInlineButton{{{
			Text: map[bool]string{true: "显示成人目录", false: "隐藏成人目录"}[next],
			Data: "adult_toggle",
		}}},
	}
}

// cmdStatus 处理 /status 命令。
func (s *TelegramBotService) cmdStatus(ctx context.Context) (telegramCommandReply, error) {
	libraryIDs, err := s.activeTelegramStatsLibraryIDs(ctx)
	if err != nil {
		return telegramCommandReply{}, err
	}
	var mediaCount int64
	s.mediaStatsQuery(libraryIDs).Count(&mediaCount)

	var totalSize int64
	if err := s.mediaStatsQuery(libraryIDs).Select("COALESCE(SUM(size_bytes), 0)").Row().Scan(&totalSize); err != nil {
		return telegramCommandReply{}, err
	}
	totalSizeGB := float64(totalSize) / 1024 / 1024 / 1024

	return telegramCommandReply{Text: fmt.Sprintf(
		"<b>系统运行状态</b>\n\n"+
			"🎬 媒体总数: <b>%d</b>\n"+
			"💾 存储占用: <b>%.1f GB</b>",
		mediaCount, totalSizeGB,
	)}, nil
}

// cmdSearch 处理 /search 命令。
func (s *TelegramBotService) cmdSearch(ctx context.Context, args []string) (telegramCommandReply, error) {
	if len(args) == 0 {
		return telegramCommandReply{Text: "请提供搜索关键词\n例: <code>/search 哥斯拉</code>"}, nil
	}

	keyword := strings.Join(args, " ")
	var results []model.Media
	err := s.repo.DB.Where("title LIKE ?", "%"+keyword+"%").
		Order("year DESC").Limit(8).
		Find(&results).Error
	if err != nil {
		return telegramCommandReply{}, err
	}

	if len(results) == 0 {
		return telegramCommandReply{Text: fmt.Sprintf("未找到与 <b>%s</b> 相关的媒体", keyword)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>搜索: %s</b>\n\n", keyword))
	for i, m := range results {
		year := ""
		if m.Year > 0 {
			year = fmt.Sprintf(" (%d)", m.Year)
		}
		ep := ""
		if m.SeasonNum > 0 && m.EpisodeNum > 0 {
			ep = fmt.Sprintf(" S%02dE%02d", m.SeasonNum, m.EpisodeNum)
		}
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>%s%s — %s\n", i+1, m.Title, year, ep, formatSize(m.SizeBytes)))
	}

	return telegramCommandReply{Text: sb.String()}, nil
}

// cmdDownloads 处理 /downloads 命令。
func (s *TelegramBotService) cmdDownloads(ctx context.Context) (telegramCommandReply, error) {
	type Row struct {
		Title  string
		Status string
	}
	var rows []Row
	if err := s.repo.DB.Raw(
		"SELECT COALESCE(NULLIF(title,''),'下载任务') as title, COALESCE(status,'unknown') as status FROM download_tasks ORDER BY created_at DESC LIMIT 8",
	).Scan(&rows).Error; err != nil {
		return telegramCommandReply{}, err
	}

	if len(rows) == 0 {
		return telegramCommandReply{Text: "当前没有下载任务。"}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>下载任务 (%d)</b>\n\n", len(rows)))
	for _, r := range rows {
		icon := "⏳"
		switch r.Status {
		case "completed":
			icon = "✅"
		case "downloading":
			icon = "📥"
		case "error":
			icon = "❌"
		}
		name := strings.TrimSpace(r.Title)
		if name == "" {
			name = "下载任务"
		}
		if len(name) > 60 {
			name = name[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s %s\n", icon, name))
	}

	return telegramCommandReply{Text: sb.String()}, nil
}

// cmdStats 处理 /stats 命令。
func (s *TelegramBotService) cmdStats(ctx context.Context) (telegramCommandReply, error) {
	libs, err := s.activeTelegramStatsLibraries(ctx)
	if err != nil {
		return telegramCommandReply{}, err
	}
	libraryIDs := make([]string, 0, len(libs))
	for _, lib := range libs {
		libraryIDs = append(libraryIDs, lib.ID)
	}
	var totalMedia int64
	s.mediaStatsQuery(libraryIDs).Count(&totalMedia)

	var totalSize int64
	if err := s.mediaStatsQuery(libraryIDs).Select("COALESCE(SUM(size_bytes), 0)").Row().Scan(&totalSize); err != nil {
		return telegramCommandReply{}, err
	}

	type LibStat struct {
		Name  string
		Type  string
		Count int64
	}
	stats := make([]LibStat, 0, len(libs))
	for _, lib := range libs {
		var count int64
		if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("library_id = ?", lib.ID).Count(&count).Error; err != nil {
			return telegramCommandReply{}, err
		}
		stats = append(stats, LibStat{Name: lib.Name, Type: lib.Type, Count: count})
	}

	var sb strings.Builder
	sb.WriteString("<b>媒体库统计</b>\n\n")
	sb.WriteString(fmt.Sprintf("📚 总数: <b>%d</b>\n", totalMedia))
	sb.WriteString(fmt.Sprintf("💾 大小: <b>%s</b>\n", formatSize(totalSize)))

	if len(stats) > 0 {
		sb.WriteString("\n<b>各库分布:</b>\n")
		for _, l := range stats {
			icon := "🎬"
			switch l.Type {
			case "tv":
				icon = "📺"
			case "anime":
				icon = "🍥"
			case "music":
				icon = "🎵"
			}
			sb.WriteString(fmt.Sprintf("%s <b>%s</b>: %d\n", icon, l.Name, l.Count))
		}
	}

	return telegramCommandReply{Text: sb.String()}, nil
}

func (s *TelegramBotService) activeTelegramStatsLibraries(ctx context.Context) ([]model.Library, error) {
	if s == nil || s.repo == nil || s.repo.Library == nil {
		return nil, nil
	}
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return nil, err
	}
	libs = FilterDisplayCloudLibraries(ctx, s.repo, libs)
	out := libs[:0]
	for _, lib := range libs {
		if lib.Enabled {
			out = append(out, lib)
		}
	}
	return out, nil
}

func (s *TelegramBotService) activeTelegramStatsLibraryIDs(ctx context.Context) ([]string, error) {
	libs, err := s.activeTelegramStatsLibraries(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(libs))
	for _, lib := range libs {
		ids = append(ids, lib.ID)
	}
	return ids, nil
}

func (s *TelegramBotService) mediaStatsQuery(libraryIDs []string) *gorm.DB {
	q := s.repo.DB.Model(&model.Media{})
	if len(libraryIDs) == 0 {
		return q.Where("1 = 0")
	}
	return q.Where("library_id IN ?", libraryIDs)
}

// ── Polling ──

// StartPolling 为所有已启用的 Telegram 通知渠道启动长轮询。
func (s *TelegramBotService) StartPolling(ctx context.Context) {
	channels, err := s.repo.NotifyChannel.ListByType(ctx, "telegram")
	if err != nil {
		s.log.Error("failed to list telegram channels for polling", zap.Error(err))
		return
	}

	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		configStr := ch.Config
		if s.crypto != nil && configStr != "" {
			configStr = s.crypto.Decrypt(configStr)
		}
		var cfg map[string]string
		if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
			continue
		}
		botToken := cfg["bot_token"]
		if botToken == "" {
			continue
		}
		if err := registerTelegramBotCommands(ctx, cfg); err != nil && s.log != nil {
			s.log.Warn("telegram setMyCommands failed", zap.Error(sanitizeTelegramError(err)))
		}

		s.pollingMu.Lock()
		if _, running := s.pollingCancel[botToken]; running {
			s.pollingMu.Unlock()
			continue
		}
		pollCtx, cancel := context.WithCancel(context.Background())
		s.pollingCancel[botToken] = cancel
		s.pollingMu.Unlock()

		channel := ch
		go s.pollLoop(pollCtx, cfg, &channel)
		s.log.Info("started telegram polling", zap.String("channel", ch.Name))
	}
}

// StopPolling 停止所有 Telegram 长轮询。
func (s *TelegramBotService) StopPolling() {
	s.pollingMu.Lock()
	defer s.pollingMu.Unlock()
	for token, cancel := range s.pollingCancel {
		cancel()
		delete(s.pollingCancel, token)
	}
	s.log.Info("telegram polling stopped")
}

// pollLoop 对单个 Bot Token 执行长轮询。
func (s *TelegramBotService) pollLoop(ctx context.Context, cfg map[string]string, channel *model.NotifyChannel) {
	var offset int64 = 0
	pollURL, err := telegramMethodURL(cfg, cfg["bot_token"], "getUpdates")
	if err != nil {
		s.log.Warn("telegram polling config invalid", zap.Error(err))
		return
	}
	clients := telegramHTTPClients(45*time.Second, cfg)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		reqBody, _ := json.Marshal(map[string]interface{}{
			"offset":  offset,
			"timeout": 30,
		})
		respBody, err := telegramPollingRequest(ctx, clients, pollURL, string(reqBody))
		if err != nil {
			s.log.Debug("telegram polling failed", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}

		var result struct {
			OK     bool             `json:"ok"`
			Result []TelegramUpdate `json:"result"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil || !result.OK {
			time.Sleep(3 * time.Second)
			continue
		}

		for _, upd := range result.Result {
			if upd.UpdateID >= int(offset) {
				offset = int64(upd.UpdateID) + 1
			}
			if !telegramUpdateActionable(upd) {
				continue
			}
			go func(u TelegramUpdate) {
				handlerCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				_ = s.handleTelegramUpdate(handlerCtx, u, channel)
			}(upd)
		}
	}
}

// telegramUpdateActionable 判断一条 update 是否需要分发处理。
// 长轮询默认会返回 message 与 callback_query 两类更新；命令消息需有文本，
// 而内联按钮回调（callback_query）必须被分发，否则成人目录显隐开关会失效。
func telegramUpdateActionable(upd TelegramUpdate) bool {
	if upd.CallbackQuery != nil {
		return true
	}
	return upd.Message != nil && upd.Message.Text != ""
}

func telegramPollingRequest(ctx context.Context, clients []*http.Client, pollURL, body string) ([]byte, error) {
	var lastErr error
	for _, client := range clients {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, pollURL, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = sanitizeTelegramError(err)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("telegram api error %d: %s", resp.StatusCode, sanitizeTelegramText(string(respBody)))
			continue
		}
		return respBody, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("telegram polling failed")
}

// ── Message Sending ──

const defaultTelegramMessageDeleteDelay = 120 * time.Second

type telegramSendMessageResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int `json:"message_id"`
	} `json:"result"`
}

// reply 通过 Telegram Bot API 发送回复消息。
func (s *TelegramBotService) reply(ctx context.Context, channel *model.NotifyChannel, chatID int, reply telegramCommandReply) error {
	cfg := s.telegramChannelConfig(channel)
	if strings.TrimSpace(cfg["bot_token"]) == "" {
		return fmt.Errorf("bot_token not configured")
	}

	payload := map[string]interface{}{
		"chat_id":    strconv.Itoa(chatID),
		"text":       reply.Text,
		"parse_mode": "HTML",
	}
	if len(reply.Buttons) > 0 {
		keyboard := make([][]map[string]string, 0, len(reply.Buttons))
		for _, row := range reply.Buttons {
			buttons := make([]map[string]string, 0, len(row))
			for _, button := range row {
				buttons = append(buttons, map[string]string{
					"text":          button.Text,
					"callback_data": button.Data,
				})
			}
			keyboard = append(keyboard, buttons)
		}
		payload["reply_markup"] = map[string]interface{}{"inline_keyboard": keyboard}
	}
	var sent telegramSendMessageResponse
	if err := telegramPostJSONDecode(ctx, cfg, "sendMessage", payload, 15*time.Second, &sent); err != nil {
		return err
	}
	if sent.Result.MessageID > 0 {
		s.scheduleTelegramMessageDelete(cfg, chatID, sent.Result.MessageID)
	}
	return nil
}

func (s *TelegramBotService) replyForMessage(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, reply telegramCommandReply) error {
	if msg == nil {
		return nil
	}
	if strings.TrimSpace(reply.Text) == "" {
		return nil
	}
	if !telegramIsGroupChat(msg.Chat.Type) {
		return s.reply(ctx, channel, msg.Chat.ID, reply)
	}
	if err := s.reply(ctx, channel, msg.From.ID, reply); err != nil {
		if s.log != nil {
			s.log.Warn("telegram private reply from group failed",
				zap.Int("group_chat_id", msg.Chat.ID),
				zap.Int("telegram_user_id", msg.From.ID),
				zap.Error(sanitizeTelegramError(err)),
			)
		}
		return s.reply(ctx, channel, msg.Chat.ID, telegramCommandReply{Text: telegramGroupPrivateDeliveryFailedHint()})
	}
	return s.reply(ctx, channel, msg.Chat.ID, telegramCommandReply{Text: telegramGroupPrivateDeliverySentHint()})
}

func (s *TelegramBotService) deleteTelegramSourceMessage(channel *model.NotifyChannel, chatID, messageID int) {
	if messageID <= 0 {
		return
	}
	s.scheduleTelegramMessageDelete(s.telegramChannelConfig(channel), chatID, messageID)
}

func (s *TelegramBotService) scheduleTelegramMessageDelete(cfg map[string]string, chatID, messageID int) {
	if chatID == 0 || messageID <= 0 || strings.TrimSpace(cfg["bot_token"]) == "" {
		return
	}
	delay := telegramMessageDeleteDelay(cfg)
	if delay < 0 {
		return
	}
	cfgCopy := make(map[string]string, len(cfg))
	for k, v := range cfg {
		cfgCopy[k] = v
	}
	go func() {
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			<-timer.C
		}
		deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := telegramPostJSON(deleteCtx, cfgCopy, "deleteMessage", map[string]interface{}{
			"chat_id":    strconv.Itoa(chatID),
			"message_id": messageID,
		}, 10*time.Second)
		if err != nil && s.log != nil {
			s.log.Debug("telegram deleteMessage failed",
				zap.Int("chat_id", chatID),
				zap.Int("message_id", messageID),
				zap.Error(sanitizeTelegramError(err)),
			)
		}
	}()
}

func telegramMessageDeleteDelay(cfg map[string]string) time.Duration {
	for _, key := range []string{"auto_delete_seconds", "message_delete_seconds", "delete_after_seconds"} {
		raw := strings.TrimSpace(cfg[key])
		if raw == "" {
			continue
		}
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		if seconds < 0 {
			return -1
		}
		return time.Duration(seconds) * time.Second
	}
	return defaultTelegramMessageDeleteDelay
}

// findChannelByChatID 根据 chat_id 查找已配置的通知渠道。
func (s *TelegramBotService) findChannelByChatID(ctx context.Context, chatID int) *model.NotifyChannel {
	channels, err := s.repo.NotifyChannel.ListByType(ctx, "telegram")
	if err != nil {
		return nil
	}
	target := strconv.Itoa(chatID)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		configStr := ch.Config
		if s.crypto != nil && configStr != "" {
			configStr = s.crypto.Decrypt(configStr)
		}
		var cfg map[string]string
		if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
			continue
		}
		if cfg["chat_id"] == target || cfg["command_chat_id"] == target ||
			cfg["group_chat_id"] == target || cfg["channel_chat_id"] == target {
			return &ch
		}
	}
	if len(channels) == 1 && channels[0].Enabled {
		return &channels[0]
	}
	return nil
}

func (s *TelegramBotService) findChannelForMessage(ctx context.Context, msg *TelegramMessage) *model.NotifyChannel {
	if msg == nil {
		return nil
	}
	if msg.Chat.Type != "" && msg.Chat.Type != "private" {
		return s.findChannelByChatID(ctx, msg.Chat.ID)
	}
	channels, err := s.repo.NotifyChannel.ListByType(ctx, "telegram")
	if err != nil {
		return nil
	}
	var first *model.NotifyChannel
	for i := range channels {
		ch := channels[i]
		if !ch.Enabled {
			continue
		}
		if first == nil {
			first = &ch
		}
		if s.telegramUserIsAdmin(ctx, &ch, msg.From.ID) || s.telegramUserCanBind(ctx, &ch, msg.From.ID) {
			return &ch
		}
	}
	return first
}

func (s *TelegramBotService) channelForMessage(ctx context.Context, msg *TelegramMessage, hint *model.NotifyChannel) *model.NotifyChannel {
	if hint == nil {
		return s.findChannelForMessage(ctx, msg)
	}
	if msg == nil {
		return hint
	}
	if msg.Chat.Type != "" && msg.Chat.Type != "private" && !s.telegramChatAllowed(hint, msg.Chat.ID) {
		return nil
	}
	return hint
}

func (s *TelegramBotService) handleCallback(ctx context.Context, cb *TelegramCallbackQuery, channelHint *model.NotifyChannel) error {
	if cb == nil || cb.Message == nil {
		return nil
	}
	msg := *cb.Message
	msg.From = cb.From
	channel := s.channelForMessage(ctx, &msg, channelHint)
	if channel == nil {
		channel = s.findChannelByChatID(ctx, cb.Message.Chat.ID)
	}
	// 立即应答回调，关闭按钮上的加载状态，避免客户端长时间转圈。
	if telegramIsGroupChat(cb.Message.Chat.Type) {
		s.answerCallbackWithText(ctx, channel, cb.ID, "为了隐私，群组内按钮面板已禁用。请私聊 Bot 或在群里发送 /menu，我会把面板私聊给你。", true)
		s.deleteTelegramSourceMessage(channel, cb.Message.Chat.ID, cb.Message.MessageID)
		return nil
	}
	if cb.Message.Chat.Type == "private" && cb.Message.Chat.ID != cb.From.ID {
		s.answerCallbackWithText(ctx, channel, cb.ID, "这个面板不属于你，请发送 /menu 打开自己的面板。", true)
		return nil
	}
	s.answerCallback(ctx, channel, cb.ID)
	data := strings.TrimSpace(cb.Data)
	if data == "adult_toggle" {
		reply := s.cmdHideAdult(ctx, &msg, nil)
		if reply.Text != "" {
			err := s.reply(ctx, channel, cb.Message.Chat.ID, reply)
			s.deleteTelegramSourceMessage(channel, cb.Message.Chat.ID, cb.Message.MessageID)
			return err
		}
		return nil
	}
	if reply, handled := s.handleMenuCallback(ctx, channel, &msg, data); handled {
		if reply.Text != "" {
			err := s.reply(ctx, channel, cb.Message.Chat.ID, reply)
			s.deleteTelegramSourceMessage(channel, cb.Message.Chat.ID, cb.Message.MessageID)
			return err
		}
	}
	return nil
}

// answerCallback 应答 Telegram 回调查询，关闭按钮上的加载提示。
func (s *TelegramBotService) answerCallback(ctx context.Context, channel *model.NotifyChannel, callbackID string) {
	s.answerCallbackWithText(ctx, channel, callbackID, "", false)
}

func (s *TelegramBotService) answerCallbackWithText(ctx context.Context, channel *model.NotifyChannel, callbackID, text string, showAlert bool) {
	if channel == nil || strings.TrimSpace(callbackID) == "" {
		return
	}
	cfg := s.telegramChannelConfig(channel)
	if strings.TrimSpace(cfg["bot_token"]) == "" {
		return
	}
	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	if strings.TrimSpace(text) != "" {
		payload["text"] = text
		payload["show_alert"] = showAlert
	}
	if err := telegramPostJSON(ctx, cfg, "answerCallbackQuery", payload, 8*time.Second); err != nil {
		s.log.Debug("telegram answerCallbackQuery failed", zap.Error(sanitizeTelegramError(err)))
	}
}

func (s *TelegramBotService) telegramBinding(ctx context.Context, telegramUserID int) *model.TelegramBinding {
	if telegramUserID == 0 {
		return nil
	}
	var binding model.TelegramBinding
	err := s.repo.DB.WithContext(ctx).Where("telegram_user_id = ?", int64(telegramUserID)).First(&binding).Error
	if err != nil {
		return nil
	}
	return &binding
}

func (s *TelegramBotService) unbindTelegramUser(ctx context.Context, telegramUserID int) error {
	if s == nil || s.repo == nil || s.repo.DB == nil || telegramUserID == 0 {
		return nil
	}
	return s.repo.DB.WithContext(ctx).Unscoped().
		Where("telegram_user_id = ?", int64(telegramUserID)).
		Delete(&model.TelegramBinding{}).Error
}

func (s *TelegramBotService) telegramUserIsAdmin(ctx context.Context, channel *model.NotifyChannel, telegramUserID int) bool {
	if s.telegramUserIDConfigured(channel, telegramUserID) {
		return true
	}
	binding := s.telegramBinding(ctx, telegramUserID)
	if binding == nil {
		return false
	}
	user, err := s.repo.User.FindByID(ctx, binding.UserID)
	return err == nil && user != nil && user.Role == "admin" && user.IsActive
}

func (s *TelegramBotService) telegramChatAllowed(channel *model.NotifyChannel, chatID int) bool {
	if channel == nil {
		return false
	}
	configStr := channel.Config
	if s.crypto != nil && configStr != "" {
		configStr = s.crypto.Decrypt(configStr)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return false
	}
	target := strconv.Itoa(chatID)
	for _, key := range []string{"group_chat_id", "channel_chat_id", "command_chat_id"} {
		if configured := strings.TrimSpace(cfg[key]); configured != "" && configured == target {
			return true
		}
	}
	if strings.TrimSpace(cfg["group_chat_id"]) != "" || strings.TrimSpace(cfg["channel_chat_id"]) != "" || strings.TrimSpace(cfg["command_chat_id"]) != "" {
		return false
	}
	return strings.TrimSpace(cfg["chat_id"]) == target
}

func (s *TelegramBotService) telegramUserCanBind(ctx context.Context, channel *model.NotifyChannel, telegramUserID int) bool {
	if telegramUserID == 0 || channel == nil {
		return false
	}
	if s.telegramUserIDConfigured(channel, telegramUserID) {
		return true
	}
	cfg := s.telegramChannelConfig(channel)
	groupID := strings.TrimSpace(cfg["group_chat_id"])
	channelID := strings.TrimSpace(cfg["channel_chat_id"])
	if groupID == "" && channelID == "" {
		return false
	}
	for _, chatID := range []string{groupID, channelID} {
		if chatID == "" {
			continue
		}
		if s.telegramUserIsChatMember(ctx, channel, chatID, telegramUserID) {
			return true
		}
	}
	return false
}

func (s *TelegramBotService) telegramUserIsChatMember(ctx context.Context, channel *model.NotifyChannel, chatID string, telegramUserID int) bool {
	cfg := s.telegramChannelConfig(channel)
	if strings.TrimSpace(cfg["bot_token"]) == "" || chatID == "" || telegramUserID == 0 {
		return false
	}
	payload := map[string]interface{}{
		"chat_id": chatID,
		"user_id": telegramUserID,
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := telegramPostJSONDecode(ctx, cfg, "getChatMember", payload, 8*time.Second, &result); err != nil {
		s.log.Warn("telegram getChatMember failed", zap.String("chat_id", chatID), zap.Int("telegram_user_id", telegramUserID), zap.Error(sanitizeTelegramError(err)))
		return false
	}
	if !result.OK {
		return false
	}
	switch strings.ToLower(result.Result.Status) {
	case "creator", "administrator", "member", "restricted":
		return true
	default:
		return false
	}
}

func (s *TelegramBotService) telegramUserIDConfigured(channel *model.NotifyChannel, telegramUserID int) bool {
	if channel == nil || telegramUserID == 0 {
		return false
	}
	cfg := s.telegramChannelConfig(channel)
	target := strconv.Itoa(telegramUserID)
	for _, value := range telegramConfiguredUserIDs(cfg["admin_user_ids"]) {
		if value == target {
			return true
		}
	}
	if strings.TrimSpace(cfg["admin_user_ids"]) == "" && strings.TrimSpace(cfg["chat_id"]) == target {
		return true
	}
	return false
}

func (s *TelegramBotService) telegramChannelConfig(channel *model.NotifyChannel) map[string]string {
	return telegramConfigFromChannel(s.crypto, channel)
}

func telegramConfigFromChannel(crypto *CryptoService, channel *model.NotifyChannel) map[string]string {
	if channel == nil {
		return map[string]string{}
	}
	configStr := channel.Config
	if crypto != nil && configStr != "" {
		configStr = crypto.Decrypt(configStr)
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil || cfg == nil {
		return map[string]string{}
	}
	normalizeTelegramConfig(cfg)
	return cfg
}

func normalizeTelegramConfig(cfg map[string]string) {
	if cfg == nil {
		return
	}
	chatID := strings.TrimSpace(cfg["chat_id"])
	if chatID == "" {
		return
	}
	if strings.HasPrefix(chatID, "-") {
		if strings.TrimSpace(cfg["group_chat_id"]) == "" && strings.TrimSpace(cfg["channel_chat_id"]) == "" && strings.TrimSpace(cfg["command_chat_id"]) == "" {
			cfg["group_chat_id"] = chatID
		}
		return
	}
	if strings.TrimSpace(cfg["admin_user_ids"]) == "" {
		cfg["admin_user_ids"] = chatID
	}
}

func (s *TelegramBotService) upsertTelegramBinding(ctx context.Context, msg *TelegramMessage, userID string) error {
	name := strings.TrimSpace(msg.From.FirstName)
	if msg.From.Username != "" {
		name = "@" + strings.TrimSpace(msg.From.Username)
	}
	telegramUserID := int64(msg.From.ID)
	return s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.TelegramBinding
		err := tx.Where("telegram_user_id = ?", telegramUserID).First(&existing).Error
		if err == nil {
			if existing.UserID != userID {
				if err := s.ensureTelegramAccountBindingAvailableTx(ctx, tx, userID, telegramUserID); err != nil {
					return err
				}
			}
			if err := tx.Model(&existing).Updates(map[string]any{
				"telegram_name": name,
				"chat_id":       telegramBindingChatIDForMessage(msg, &existing),
				"user_id":       userID,
			}).Error; telegramBindingUniqueErr(err) {
				return errTelegramAccountAlreadyBound
			} else if err != nil {
				return err
			}
			return nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Unscoped().Where("telegram_user_id = ?", telegramUserID).Delete(&model.TelegramBinding{}).Error; err != nil {
			return err
		}
		if err := s.ensureTelegramAccountBindingAvailableTx(ctx, tx, userID, telegramUserID); err != nil {
			return err
		}
		err = tx.Create(&model.TelegramBinding{
			TelegramUserID: telegramUserID,
			TelegramName:   name,
			ChatID:         telegramBindingChatIDForMessage(msg, nil),
			UserID:         userID,
		}).Error
		if telegramBindingUniqueErr(err) {
			return errTelegramAccountAlreadyBound
		}
		return err
	})
}

func telegramBindingChatIDForMessage(msg *TelegramMessage, existing *model.TelegramBinding) int64 {
	if msg == nil {
		if existing != nil {
			return existing.ChatID
		}
		return 0
	}
	if msg.Chat.Type == "" || msg.Chat.Type == "private" {
		return int64(msg.Chat.ID)
	}
	if existing != nil && existing.ChatID > 0 {
		return existing.ChatID
	}
	return int64(msg.From.ID)
}

func telegramPrivateChatIDFromBinding(binding model.TelegramBinding) int64 {
	if binding.ChatID > 0 {
		return binding.ChatID
	}
	return binding.TelegramUserID
}

func (s *TelegramBotService) ensureTelegramAccountBindingAvailable(ctx context.Context, userID string, telegramUserID int64) error {
	return s.ensureTelegramAccountBindingAvailableTx(ctx, s.repo.DB.WithContext(ctx), userID, telegramUserID)
}

func (s *TelegramBotService) ensureTelegramAccountBindingAvailableTx(ctx context.Context, tx *gorm.DB, userID string, telegramUserID int64) error {
	var bound model.TelegramBinding
	err := tx.WithContext(ctx).
		Where("user_id = ? AND telegram_user_id <> ?", userID, telegramUserID).
		First(&bound).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var user model.User
	if err := tx.WithContext(ctx).Where("id = ?", bound.UserID).First(&user).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		_ = tx.WithContext(ctx).Unscoped().Delete(&model.TelegramBinding{}, "id = ?", bound.ID).Error
		return nil
	} else if err != nil {
		return err
	}
	return errTelegramAccountAlreadyBound
}

func telegramBindingUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "idx_telegram_bindings_user_id_active") ||
		strings.Contains(msg, "telegram_bindings.user_id") ||
		(strings.Contains(msg, "unique") && strings.Contains(msg, "telegram_bindings"))
}

func parseStartCredentials(args []string) (string, string) {
	if len(args) >= 2 {
		return strings.TrimSpace(args[0]), strings.TrimSpace(strings.Join(args[1:], " "))
	}
	if len(args) == 1 {
		raw := strings.TrimSpace(args[0])
		for _, sep := range []string{"-", "：", ":"} {
			if parts := strings.SplitN(raw, sep, 2); len(parts) == 2 {
				return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			}
		}
	}
	return "", ""
}

func userNameOrFallback(user *model.User) string {
	if user == nil || strings.TrimSpace(user.Username) == "" {
		return "未知用户"
	}
	return user.Username
}

// ── Webhook Management ──

// SetWebhook 注册 Telegram Bot Webhook URL。
func (s *TelegramBotService) SetWebhook(ctx context.Context, botToken, webhookURL string) error {
	cfg := map[string]string{"bot_token": botToken}
	if err := registerTelegramBotCommands(ctx, cfg); err != nil && s.log != nil {
		s.log.Warn("telegram setMyCommands failed", zap.Error(sanitizeTelegramError(err)))
	}
	payload := map[string]interface{}{
		"url":             webhookURL,
		"allowed_updates": []string{"message", "callback_query"},
	}
	return telegramPostJSON(ctx, cfg, "setWebhook", payload, 15*time.Second)
}

// GetWebhookInfo 获取 Webhook 配置信息。
func (s *TelegramBotService) GetWebhookInfo(ctx context.Context, botToken string) (map[string]interface{}, error) {
	cfg := map[string]string{"bot_token": botToken}
	var result map[string]interface{}
	if err := telegramGetJSONDecode(ctx, cfg, "getWebhookInfo", 10*time.Second, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// formatSize 格式化字节数为可读字符串。
func formatSize(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(bytes)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

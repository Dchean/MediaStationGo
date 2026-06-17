package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

// pendingTTL bounds how long a button-initiated text prompt stays valid.
const pendingTTL = 5 * time.Minute

var (
	errRegistrationCodeAlreadyUsed = errors.New("registration code already used")
	errRegistrationCodeExpired     = errors.New("registration code expired")
)

func (s *TelegramBotService) setPending(userID int64, kind string) {
	s.pendingMu.Lock()
	s.pending[userID] = pendingInput{Kind: kind, CreatedAt: time.Now()}
	s.pendingMu.Unlock()
}

func (s *TelegramBotService) takePending(userID int64) (pendingInput, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	p, ok := s.pending[userID]
	if ok {
		delete(s.pending, userID)
	}
	if ok && time.Since(p.CreatedAt) > pendingTTL {
		return pendingInput{}, false
	}
	return p, ok
}

// boundUser resolves the local user bound to a Telegram account, or nil.
func (s *TelegramBotService) boundUser(ctx context.Context, telegramUserID int) *model.User {
	binding := s.telegramBinding(ctx, telegramUserID)
	if binding == nil {
		return nil
	}
	u, _ := s.repo.User.FindByID(ctx, binding.UserID)
	return u
}

// mainMenu builds the button-based menu, tailored to the user's binding and
// admin status. Ordinary users only see self-service actions; admins get an
// extra management section.
func (s *TelegramBotService) mainMenu(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage) telegramCommandReply {
	isAdmin := s.telegramUserIsAdmin(ctx, channel, msg.From.ID)
	isGroup := telegramIsGroupChat(msg.Chat.Type)
	user := s.boundUser(ctx, msg.From.ID)

	var rows [][]telegramInlineButton
	var header string

	if isGroup {
		if user == nil {
			header = "<b>MediaStationGo 群组自助菜单</b>\n\n你还没有绑定媒体中心账号。绑定、注册、兑换等包含敏感信息的操作请私聊 Bot。"
		} else {
			adult := map[bool]string{true: "已隐藏", false: "已显示"}[user.HideAdult]
			header = fmt.Sprintf("<b>MediaStationGo 群组自助菜单</b>\n\n账号：<b>%s</b>\n到期：<b>%s</b>\n成人目录：<b>%s</b>",
				user.Username, formatExpiry(user.ExpiredAt), adult)
			rows = append(rows,
				[]telegramInlineButton{
					{Text: "👤 我的账号", Data: "act_account"},
					{Text: "📅 签到", Data: "act_signin"},
				},
				[]telegramInlineButton{
					{Text: "📱 我的设备", Data: "act_devices"},
					{Text: map[bool]string{true: "🔞 显示成人目录", false: "🔞 隐藏成人目录"}[user.HideAdult], Data: "adult_toggle"},
				},
			)
		}
		if isAdmin {
			header += "\n\n<b>管理员入口</b>"
			rows = append(rows,
				[]telegramInlineButton{{Text: "—— 管理员 ——", Data: "noop"}},
				[]telegramInlineButton{
					{Text: "📊 容量/状态", Data: "adm_capacity"},
					{Text: "👥 用户管理", Data: "adm_users"},
				},
				[]telegramInlineButton{
					{Text: "🔓 开注设置", Data: "adm_openreg"},
					{Text: "🎟 生成兑换码", Data: "adm_gencode"},
				},
				[]telegramInlineButton{
					{Text: "⚙️ 设备策略", Data: "adm_devicepolicy"},
					{Text: "🛠 管理命令", Data: "adm_mgo_commands"},
				},
			)
		}
		return telegramCommandReply{Text: header, Buttons: rows}
	}

	if user == nil {
		header = "<b>MediaStationGo</b>\n\n你还没有绑定媒体中心账号。"
		rows = append(rows, []telegramInlineButton{{Text: "🔗 绑定账号", Data: "act_bind"}})
		if s.openRegEnabled(ctx) {
			rows = append(rows, []telegramInlineButton{{Text: "📝 注册新账号", Data: "act_register"}})
		}
		rows = append(rows, []telegramInlineButton{{Text: "🎟 兑换码注册", Data: "act_redeem_register"}})
	} else {
		adult := map[bool]string{true: "已隐藏", false: "已显示"}[user.HideAdult]
		header = fmt.Sprintf("<b>MediaStationGo</b>\n\n账号：<b>%s</b>\n到期：<b>%s</b>\n成人目录：<b>%s</b>",
			user.Username, formatExpiry(user.ExpiredAt), adult)
		rows = append(rows,
			[]telegramInlineButton{
				{Text: "👤 我的账号", Data: "act_account"},
				{Text: "📅 签到", Data: "act_signin"},
			},
			[]telegramInlineButton{
				{Text: "📱 我的设备", Data: "act_devices"},
				{Text: map[bool]string{true: "🔞 显示成人目录", false: "🔞 隐藏成人目录"}[user.HideAdult], Data: "adult_toggle"},
			},
			[]telegramInlineButton{
				{Text: "✏️ 改用户名", Data: "act_setname"},
				{Text: "🔑 改密码", Data: "act_setpass"},
			},
			[]telegramInlineButton{{Text: "🎟 兑换码续期", Data: "act_redeem_renew"}},
		)
	}

	if isAdmin {
		rows = append(rows,
			[]telegramInlineButton{{Text: "—— 管理员 ——", Data: "noop"}},
			[]telegramInlineButton{
				{Text: "📊 容量/状态", Data: "adm_capacity"},
				{Text: "👥 用户管理", Data: "adm_users"},
			},
			[]telegramInlineButton{
				{Text: "🔓 开注设置", Data: "adm_openreg"},
				{Text: "🎟 生成兑换码", Data: "adm_gencode"},
			},
			[]telegramInlineButton{
				{Text: "⚙️ 设备策略", Data: "adm_devicepolicy"},
				{Text: "🛠 管理命令", Data: "adm_mgo_commands"},
			},
		)
	}

	return telegramCommandReply{Text: header, Buttons: rows}
}

// handleMenuCallback routes inline-button taps. Returns (reply, handled).
func (s *TelegramBotService) handleMenuCallback(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, data string) (telegramCommandReply, bool) {
	isAdmin := s.telegramUserIsAdmin(ctx, channel, msg.From.ID)
	isGroup := telegramIsGroupChat(msg.Chat.Type)

	switch {
	case data == "noop":
		return telegramCommandReply{}, true
	case data == "menu_main":
		return s.mainMenu(ctx, channel, msg), true
	case data == "act_bind":
		if isGroup {
			return telegramCommandReply{Text: telegramGroupPrivateUserHint("绑定账号")}, true
		}
		return telegramCommandReply{Text: "请发送：<code>/start 用户名 密码</code> 绑定已有账号。"}, true
	case data == "act_register":
		if isGroup {
			return telegramCommandReply{Text: telegramGroupPrivateUserHint("注册账号")}, true
		}
		if !s.openRegEnabled(ctx) {
			return telegramCommandReply{Text: "注册功能未开放，请联系管理员。"}, true
		}
		s.setPending(int64(msg.From.ID), "register")
		return telegramCommandReply{Text: "请发送新账号的 <b>用户名 密码</b>（空格分隔），例如：<code>alice mypass123</code>"}, true
	case data == "act_redeem_register":
		if isGroup {
			return telegramCommandReply{Text: telegramGroupPrivateUserHint("兑换码注册")}, true
		}
		s.setPending(int64(msg.From.ID), "redeem_register")
		return telegramCommandReply{Text: "请发送你的<b>注册兑换码</b>，例如：<code>ABCD2345EFGH</code>\n（兑换后会要求设置用户名密码）"}, true
	case data == "act_redeem_renew":
		if isGroup {
			return telegramCommandReply{Text: telegramGroupPrivateUserHint("兑换码续期")}, true
		}
		s.setPending(int64(msg.From.ID), "redeem_renew")
		return telegramCommandReply{Text: "请发送你的<b>续期兑换码</b>，将为当前绑定账号续期。"}, true
	case data == "act_account":
		return s.replyAccount(ctx, msg), true
	case data == "act_signin":
		return s.replySignIn(ctx, msg), true
	case data == "act_devices":
		return s.replyDevices(ctx, msg), true
	case data == "act_setname":
		if isGroup {
			return telegramCommandReply{Text: telegramGroupPrivateUserHint("修改用户名")}, true
		}
		s.setPending(int64(msg.From.ID), "setname")
		return telegramCommandReply{Text: "请发送：<code>当前密码 新用户名</code>。"}, true
	case data == "act_setpass":
		if isGroup {
			return telegramCommandReply{Text: telegramGroupPrivateUserHint("修改密码")}, true
		}
		s.setPending(int64(msg.From.ID), "setpass")
		return telegramCommandReply{Text: "请发送：<code>当前密码 新密码</code>（新密码至少 6 位）。"}, true
	case strings.HasPrefix(data, "kick:"):
		return s.replyKick(ctx, msg, strings.TrimPrefix(data, "kick:")), true
	}

	// ── 管理员专属 ──
	if isGroup && !isAdmin {
		return telegramCommandReply{}, true
	}
	if !isAdmin {
		return telegramCommandReply{Text: "此功能仅管理员可用。"}, true
	}
	switch {
	case data == "adm_capacity":
		return s.replyCapacity(ctx), true
	case data == "adm_openreg":
		return s.replyOpenRegMenu(ctx), true
	case data == "adm_openreg_close":
		_ = s.closeRegistration(ctx)
		return telegramCommandReply{Text: "已关闭注册。"}, true
	case strings.HasPrefix(data, "adm_openreg_set:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(data, "adm_openreg_set:"))
		if err := s.openRegistration(ctx, n); err != nil {
			return telegramCommandReply{Text: "开注失败：" + err.Error()}, true
		}
		label := "不限"
		if n > 0 {
			label = fmt.Sprintf("%d 个名额", n)
		}
		return telegramCommandReply{Text: "已开放注册：" + label + "。"}, true
	case data == "adm_gencode":
		return s.replyGenCodeMenu(), true
	case strings.HasPrefix(data, "gc:"):
		return s.replyGenCode(ctx, msg, data), true
	case data == "adm_users":
		return s.replyUserList(ctx), true
	case strings.HasPrefix(data, "usr:"):
		return s.replyUserActions(ctx, strings.TrimPrefix(data, "usr:")), true
	case strings.HasPrefix(data, "uban:"):
		return s.replyUserBan(ctx, strings.TrimPrefix(data, "uban:"), false), true
	case strings.HasPrefix(data, "uunban:"):
		return s.replyUserBan(ctx, strings.TrimPrefix(data, "uunban:"), true), true
	case strings.HasPrefix(data, "udel:"):
		return s.replyUserDelete(ctx, strings.TrimPrefix(data, "udel:")), true
	case strings.HasPrefix(data, "urenew:"):
		return s.replyUserRenew(ctx, strings.TrimPrefix(data, "urenew:")), true
	case data == "adm_devicepolicy":
		return s.replyDevicePolicy(ctx), true
	case data == "adm_mgo_commands":
		return telegramCommandReply{Text: telegramMgoAdminCommandHelp(), Buttons: [][]telegramInlineButton{{{Text: "⬅️ 返回菜单", Data: "menu_main"}}}}, true
	case strings.HasPrefix(data, "dp_toggle:"):
		return s.replyDevicePolicyToggle(ctx, strings.TrimPrefix(data, "dp_toggle:")), true
	}
	return telegramCommandReply{}, false
}

// handlePendingText consumes a button-initiated text prompt. Returns (reply,
// handled). handled=false means there was no pending prompt for this user.
func (s *TelegramBotService) handlePendingText(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, text string) (telegramCommandReply, bool) {
	p, ok := s.takePending(int64(msg.From.ID))
	if !ok {
		return telegramCommandReply{}, false
	}
	switch p.Kind {
	case "register":
		return s.cmdRegister(ctx, channel, msg, strings.Fields(text)), true
	case "redeem_register":
		return s.redeemRegisterFlow(ctx, channel, msg, text), true
	case "redeem_renew":
		return s.redeemRenewFlow(ctx, msg, text), true
	case "setname":
		return s.selfSetName(ctx, msg, text), true
	case "setpass":
		return s.selfSetPass(ctx, msg, text), true
	case "openreg_limit":
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || n < 0 {
			return telegramCommandReply{Text: "请输入有效的非负整数。"}, true
		}
		if err := s.openRegistration(ctx, n); err != nil {
			return telegramCommandReply{Text: "开注失败：" + err.Error()}, true
		}
		return telegramCommandReply{Text: fmt.Sprintf("已开放注册：%d 个名额。", n)}, true
	}
	return telegramCommandReply{}, false
}

// ── 用户自助 ──────────────────────────────────────────────────────────────

func (s *TelegramBotService) cmdKick(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号：<code>/start 用户名 密码</code>"}
	}
	if len(args) == 0 {
		return telegramCommandReply{Text: "请指定要踢下线的设备：<code>/kick all</code> 或 <code>/kick 设备编号</code>。先用 <code>/devices</code> 查看编号。"}
	}
	target := strings.TrimSpace(args[0])
	if strings.EqualFold(target, "all") || target == "全部" {
		if s.device != nil {
			if err := s.device.KickAllDevices(ctx, user.ID); err != nil {
				return telegramCommandReply{Text: "踢下线失败：" + err.Error()}
			}
		} else if err := s.repo.UserDevice.SetKickedByUser(ctx, user.ID, true); err != nil {
			return telegramCommandReply{Text: "踢下线失败：" + err.Error()}
		}
		return telegramCommandReply{Text: "已踢下线此账号的全部设备。"}
	}
	devices, _ := s.repo.UserDevice.ListByUser(ctx, user.ID)
	if len(devices) == 0 {
		return telegramCommandReply{Text: "当前没有记录到登录设备。"}
	}
	var chosen *model.UserDevice
	if n, err := strconv.Atoi(target); err == nil && n >= 1 && n <= len(devices) {
		chosen = &devices[n-1]
	} else {
		for i := range devices {
			if devices[i].ID == target || devices[i].DeviceID == target {
				chosen = &devices[i]
				break
			}
		}
	}
	if chosen == nil {
		return telegramCommandReply{Text: "未找到该设备。请用 <code>/devices</code> 查看设备编号后重试。"}
	}
	if err := s.repo.UserDevice.SetKicked(ctx, chosen.ID, true); err != nil {
		return telegramCommandReply{Text: "踢下线失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("已踢下线：<b>%s</b>。", deviceLabel(chosen.DeviceName, chosen.Client))}
}

func (s *TelegramBotService) cmdSetName(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) < 2 {
		return telegramCommandReply{Text: "请发送：<code>/setname 当前密码 新用户名</code>"}
	}
	return s.selfSetName(ctx, msg, strings.Join(args, " "))
}

func (s *TelegramBotService) cmdSetPass(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) < 2 {
		return telegramCommandReply{Text: "请发送：<code>/setpass 当前密码 新密码</code>"}
	}
	return s.selfSetPass(ctx, msg, strings.Join(args, " "))
}

func (s *TelegramBotService) cmdRedeem(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "请发送：<code>/redeem 兑换码</code>\n未绑定账号时自动尝试注册码；已绑定账号时自动尝试续期码。"}
	}
	code := strings.Join(args, " ")
	if s.boundUser(ctx, msg.From.ID) == nil {
		return s.redeemRegisterFlow(ctx, channel, msg, code)
	}
	return s.redeemRenewFlow(ctx, msg, code)
}

func (s *TelegramBotService) cmdRedeemRegister(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "请发送：<code>/redeem_register 注册兑换码</code>"}
	}
	return s.redeemRegisterFlow(ctx, channel, msg, strings.Join(args, " "))
}

func (s *TelegramBotService) cmdRedeemRenew(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "请发送：<code>/redeem_renew 续期兑换码</code>"}
	}
	return s.redeemRenewFlow(ctx, msg, strings.Join(args, " "))
}

func (s *TelegramBotService) replyAccount(ctx context.Context, msg *TelegramMessage) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号：<code>/start 用户名 密码</code>"}
	}
	streak := 0
	if rec, _ := s.repo.SignIn.Get(ctx, user.ID); rec != nil {
		streak = rec.StreakDays
	}
	devices, _ := s.repo.UserDevice.ListByUser(ctx, user.ID)
	text := fmt.Sprintf("<b>我的账号</b>\n\n用户名：<b>%s</b>\n状态：<b>%s</b>\n到期：<b>%s</b>\n连续签到：<b>%d 天</b>\n登录设备：<b>%d 台</b>",
		user.Username,
		map[bool]string{true: "正常", false: "已禁用"}[user.IsActive],
		formatExpiry(user.ExpiredAt), streak, len(devices))
	return telegramCommandReply{Text: text, Buttons: [][]telegramInlineButton{{{Text: "⬅️ 返回菜单", Data: "menu_main"}}}}
}

func (s *TelegramBotService) replySignIn(ctx context.Context, msg *TelegramMessage) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号后再签到。"}
	}
	res, err := s.signIn(ctx, user.ID)
	if err != nil {
		return telegramCommandReply{Text: "签到失败：" + err.Error()}
	}
	if res.AlreadySigned {
		return telegramCommandReply{Text: fmt.Sprintf("今天已经签到过啦～\n连续签到 <b>%d</b> 天，累计 <b>%d</b> 天。", res.Streak, res.Total)}
	}
	return telegramCommandReply{Text: fmt.Sprintf("签到成功 ✅\n连续签到 <b>%d</b> 天，累计 <b>%d</b> 天。", res.Streak, res.Total)}
}

func (s *TelegramBotService) replyDevices(ctx context.Context, msg *TelegramMessage) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号。"}
	}
	devices, _ := s.repo.UserDevice.ListByUser(ctx, user.ID)
	if len(devices) == 0 {
		return telegramCommandReply{Text: "当前没有记录到登录设备。"}
	}
	var sb strings.Builder
	sb.WriteString("<b>我的登录设备</b>\n点击下方按钮可一键踢下线：\n")
	var rows [][]telegramInlineButton
	for i, d := range devices {
		status := ""
		if d.Kicked {
			status = "（已踢下线）"
		}
		sb.WriteString(fmt.Sprintf("\n%d. <b>%s</b>%s\n   最近活跃：%s", i+1, deviceLabel(d.DeviceName, d.Client), status, d.LastSeenAt.Format("01-02 15:04")))
		if !d.Kicked {
			rows = append(rows, []telegramInlineButton{{Text: "🚫 踢下线：" + deviceLabel(d.DeviceName, d.Client), Data: "kick:" + d.ID}})
		}
	}
	rows = append(rows, []telegramInlineButton{{Text: "⬅️ 返回菜单", Data: "menu_main"}})
	return telegramCommandReply{Text: sb.String(), Buttons: rows}
}

func (s *TelegramBotService) replyKick(ctx context.Context, msg *TelegramMessage, deviceRowID string) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号。"}
	}
	// Verify the device belongs to this user before kicking.
	var d model.UserDevice
	if err := s.repo.DB.WithContext(ctx).Where("id = ? AND user_id = ?", deviceRowID, user.ID).First(&d).Error; err != nil {
		return telegramCommandReply{Text: "未找到该设备。"}
	}
	if err := s.repo.UserDevice.SetKicked(ctx, d.ID, true); err != nil {
		return telegramCommandReply{Text: "操作失败：" + err.Error()}
	}
	return s.replyDevices(ctx, msg)
}

func (s *TelegramBotService) selfSetName(ctx context.Context, msg *TelegramMessage, input string) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号。"}
	}
	currentPassword, newName := splitCurrentPasswordAndValue(input)
	if currentPassword == "" || newName == "" {
		return telegramCommandReply{Text: "请发送：<code>当前密码 新用户名</code>。"}
	}
	newName = strings.TrimSpace(newName)
	if len(newName) < 2 || strings.ContainsAny(newName, " \t\n") {
		return telegramCommandReply{Text: "用户名至少 2 位且不能含空格，请重试。"}
	}
	if reply, ok := s.verifyTelegramSelfPassword(ctx, msg, user, currentPassword); !ok {
		return reply
	}
	if existing, _ := s.repo.User.FindByUsername(ctx, newName); existing != nil && existing.ID != user.ID {
		return telegramCommandReply{Text: "该用户名已被占用，请换一个。"}
	}
	if err := s.repo.User.UpdateFields(ctx, user.ID, map[string]any{"username": newName}); err != nil {
		return telegramCommandReply{Text: "修改失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("用户名已修改为 <b>%s</b>。请用新用户名登录。", newName)}
}

func (s *TelegramBotService) selfSetPass(ctx context.Context, msg *TelegramMessage, input string) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号。"}
	}
	currentPassword, newPass := splitCurrentPasswordAndValue(input)
	if currentPassword == "" || newPass == "" {
		return telegramCommandReply{Text: "请发送：<code>当前密码 新密码</code>。"}
	}
	newPass = strings.TrimSpace(newPass)
	if s.auth == nil {
		return telegramCommandReply{Text: "服务暂不可用。"}
	}
	if err := s.auth.ChangePassword(ctx, user.ID, currentPassword, newPass); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			_ = s.unbindTelegramUser(ctx, msg.From.ID)
			return telegramCommandReply{Text: "当前密码验证失败，绑定已自动解绑。请用新密码重新绑定账号。"}
		}
		return telegramCommandReply{Text: "修改失败：" + err.Error()}
	}
	if s.device != nil {
		_ = s.device.KickAllDevices(ctx, user.ID)
	}
	return telegramCommandReply{Text: "密码已修改，请用新密码重新登录第三方客户端。"}
}

func splitCurrentPasswordAndValue(input string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) < 2 {
		return "", ""
	}
	return fields[0], strings.TrimSpace(strings.Join(fields[1:], " "))
}

func (s *TelegramBotService) verifyTelegramSelfPassword(ctx context.Context, msg *TelegramMessage, user *model.User, currentPassword string) (telegramCommandReply, bool) {
	if s.auth == nil {
		return telegramCommandReply{Text: "服务暂不可用。"}, false
	}
	if err := s.auth.VerifyPassword(ctx, user.ID, currentPassword); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			_ = s.unbindTelegramUser(ctx, msg.From.ID)
			return telegramCommandReply{Text: "当前密码验证失败，绑定已自动解绑。请用新密码重新绑定账号。"}, false
		}
		return telegramCommandReply{Text: "验证失败：" + err.Error()}, false
	}
	return telegramCommandReply{}, true
}

// ── 兑换码流程 ───────────────────────────────────────────────────────────────

func (s *TelegramBotService) redeemRegisterFlow(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, raw string) telegramCommandReply {
	if channel == nil {
		channel = s.findChannelForMessage(ctx, msg)
	}
	if !s.telegramUserCanBind(ctx, channel, msg.From.ID) {
		return telegramCommandReply{Text: "当前 Telegram 账号不在管理员配置的绑定群组/频道中，无法兑换注册账号。请先加入管理员配置的群组或频道；如果尚未配置，请联系管理员。"}
	}
	rc, errMsg := s.lookupRedeemableCode(ctx, raw, model.RegistrationCodeRegister)
	if rc == nil {
		return telegramCommandReply{Text: errMsg}
	}
	if s.auth == nil {
		return telegramCommandReply{Text: "注册服务暂不可用。"}
	}
	if binding := s.telegramBinding(ctx, msg.From.ID); binding != nil {
		if u, _ := s.repo.User.FindByID(ctx, binding.UserID); u != nil {
			return telegramCommandReply{Text: fmt.Sprintf("当前 Telegram 已绑定账号 <b>%s</b>，无需再用注册码。", u.Username)}
		}
	}
	user, password, claimedCode, err := s.createUserFromRegistrationCode(ctx, rc.Code)
	if err != nil {
		if errors.Is(err, errRegistrationCodeAlreadyUsed) {
			return telegramCommandReply{Text: "兑换码刚刚被使用，请换一个。"}
		}
		if errors.Is(err, errRegistrationCodeExpired) {
			return telegramCommandReply{Text: "兑换码已过期。"}
		}
		if errors.Is(err, ErrUserLimitReached) {
			return telegramCommandReply{Text: "注册失败：用户数量已达授权上限。"}
		}
		return telegramCommandReply{Text: "注册失败：" + err.Error()}
	}
	if claimedCode == nil {
		return telegramCommandReply{Text: "兑换码刚刚被使用，请换一个。"}
	}
	_ = s.upsertTelegramBinding(ctx, msg, user.ID)
	return telegramCommandReply{
		Text: fmt.Sprintf("兑换成功并已创建账号：\n用户名：<b>%s</b>\n密码：<b>%s</b>\n到期：<b>%s</b>\n\n请尽快用「改用户名/改密码」修改为你自己的凭据。",
			user.Username, password, formatExpiry(s.userExpiry(ctx, user.ID))),
		Buttons: [][]telegramInlineButton{{{Text: "⬅️ 返回菜单", Data: "menu_main"}}},
	}
}

func (s *TelegramBotService) createUserFromRegistrationCode(ctx context.Context, rawCode string) (*model.User, string, *model.RegistrationCode, error) {
	code := normalizeRedemptionCode(rawCode)
	if code == "" {
		return nil, "", nil, errRegistrationCodeAlreadyUsed
	}
	password := randomCode(10)
	var created model.User
	var claimed model.RegistrationCode
	err := s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("code = ? AND kind = ? AND used_at IS NULL AND used_count < CASE WHEN max_uses > 0 THEN max_uses ELSE 1 END", code, model.RegistrationCodeRegister).
			First(&claimed).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errRegistrationCodeAlreadyUsed
			}
			return err
		}
		if claimed.IsExpired() {
			return errRegistrationCodeExpired
		}
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count >= LicensedMaxUsers(ctx, s.repo) {
			return ErrUserLimitReached
		}
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		codePrefix := strings.ToLower(claimed.Code)
		if len(codePrefix) > 8 {
			codePrefix = codePrefix[:8]
		}
		created = model.User{
			Username:     "u" + codePrefix,
			PasswordHash: hash,
			Role:         "user",
			Tier:         "free",
			HideAdult:    true,
			ExpiredAt:    renewExpiry(nil, claimed.DurationDays),
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		if err := tx.Create(DefaultPermissions(created.ID)).Error; err != nil {
			return err
		}
		now := time.Now()
		res := tx.Model(&model.RegistrationCode{}).
			Where("id = ? AND used_at IS NULL AND used_count < CASE WHEN max_uses > 0 THEN max_uses ELSE 1 END", claimed.ID).
			Updates(map[string]any{
				"used_by_user_id": created.ID,
				"used_count":      gorm.Expr("used_count + 1"),
				"used_at":         gorm.Expr("CASE WHEN used_count + 1 >= CASE WHEN max_uses > 0 THEN max_uses ELSE 1 END THEN ? ELSE used_at END", now),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errRegistrationCodeAlreadyUsed
		}
		claimed.UsedByUserID = created.ID
		claimed.UsedCount++
		if claimed.UsedCount >= claimed.EffectiveMaxUses() {
			claimed.UsedAt = &now
		}
		return nil
	})
	if err != nil {
		return nil, "", nil, err
	}
	return &created, password, &claimed, nil
}

func (s *TelegramBotService) redeemRenewFlow(ctx context.Context, msg *TelegramMessage, raw string) telegramCommandReply {
	user := s.boundUser(ctx, msg.From.ID)
	if user == nil {
		return telegramCommandReply{Text: "请先绑定账号再续期。"}
	}
	rc, errMsg := s.lookupRedeemableCode(ctx, raw, model.RegistrationCodeRenew)
	if rc == nil {
		return telegramCommandReply{Text: errMsg}
	}
	if err := s.repo.RegCode.MarkUsed(ctx, rc.ID, user.ID); err != nil {
		return telegramCommandReply{Text: "兑换码刚刚被使用，请换一个。"}
	}
	if err := s.applyRenewal(ctx, user.ID, rc.DurationDays); err != nil {
		return telegramCommandReply{Text: "续期失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("续期成功 ✅ 当前到期：<b>%s</b>", formatExpiry(s.userExpiry(ctx, user.ID)))}
}

func (s *TelegramBotService) userExpiry(ctx context.Context, userID string) *time.Time {
	if u, _ := s.repo.User.FindByID(ctx, userID); u != nil {
		return u.ExpiredAt
	}
	return nil
}

// ── 管理员：容量 / 开注 / 兑换码 / 用户管理 / 设备策略 ─────────────────────────

func (s *TelegramBotService) replyCapacity(ctx context.Context) telegramCommandReply {
	c := s.loadCapacity(ctx)
	quota := "未开放"
	if c.OpenRegOn {
		if c.OpenRegLimit > 0 {
			quota = fmt.Sprintf("已开放（%d/%d 名额）", c.OpenRegUsed, c.OpenRegLimit)
		} else {
			quota = "已开放（不限名额，受授权上限约束）"
		}
	}
	text := fmt.Sprintf("<b>容量 / 状态</b>\n\n授权上限：<b>%d</b> 人（随凭证授权实时变化）\n已用：<b>%d</b> 人\n剩余可注册：<b>%d</b> 人\n开注状态：<b>%s</b>",
		c.MaxUsers, c.UsedUsers, c.Remaining(), quota)
	return telegramCommandReply{Text: text, Buttons: [][]telegramInlineButton{{{Text: "⬅️ 返回菜单", Data: "menu_main"}}}}
}

func (s *TelegramBotService) replyOpenRegMenu(ctx context.Context) telegramCommandReply {
	c := s.loadCapacity(ctx)
	state := "未开放"
	if c.OpenRegOn {
		state = fmt.Sprintf("已开放（%d/%d）", c.OpenRegUsed, c.OpenRegLimit)
	}
	return telegramCommandReply{
		Text: "<b>开注设置</b>\n当前：" + state + "\n选择要开放的名额：",
		Buttons: [][]telegramInlineButton{
			{{Text: "5 个", Data: "adm_openreg_set:5"}, {Text: "10 个", Data: "adm_openreg_set:10"}, {Text: "20 个", Data: "adm_openreg_set:20"}},
			{{Text: "不限名额", Data: "adm_openreg_set:0"}, {Text: "关闭注册", Data: "adm_openreg_close"}},
			{{Text: "⬅️ 返回菜单", Data: "menu_main"}},
		},
	}
}

func (s *TelegramBotService) replyGenCodeMenu() telegramCommandReply {
	return telegramCommandReply{
		Text: "<b>生成兑换码</b>\n选择类型与时长：",
		Buttons: [][]telegramInlineButton{
			{{Text: "注册码·30天", Data: "gc:register:30"}, {Text: "注册码·永久", Data: "gc:register:0"}},
			{{Text: "续期码·30天", Data: "gc:renew:30"}, {Text: "续期码·90天", Data: "gc:renew:90"}},
			{{Text: "⬅️ 返回菜单", Data: "menu_main"}},
		},
	}
}

func (s *TelegramBotService) replyGenCode(ctx context.Context, msg *TelegramMessage, data string) telegramCommandReply {
	parts := strings.Split(data, ":") // gc:<kind>:<days>
	if len(parts) != 3 {
		return telegramCommandReply{Text: "参数错误。"}
	}
	kind := parts[1]
	days, _ := strconv.Atoi(parts[2])
	createdBy := ""
	if u := s.boundUser(ctx, msg.From.ID); u != nil {
		createdBy = u.ID
	}
	code, err := s.generateCode(ctx, kind, days, 0, createdBy)
	if err != nil {
		return telegramCommandReply{Text: "生成失败：" + err.Error()}
	}
	kindLabel := map[string]string{model.RegistrationCodeRegister: "注册码", model.RegistrationCodeRenew: "续期码"}[code.Kind]
	dur := "永久"
	if days > 0 {
		dur = fmt.Sprintf("%d 天", days)
	}
	return telegramCommandReply{
		Text:    fmt.Sprintf("已生成%s（%s）：\n\n<code>%s</code>\n\n发给用户在 Bot 中兑换即可。", kindLabel, dur, code.Code),
		Buttons: [][]telegramInlineButton{{{Text: "再生成一个", Data: "adm_gencode"}, {Text: "⬅️ 返回菜单", Data: "menu_main"}}},
	}
}

func (s *TelegramBotService) cmdGenCode(ctx context.Context, msg *TelegramMessage, args []string) telegramCommandReply {
	if len(args) < 2 {
		return telegramCommandReply{Text: "用法：<code>/gencode register|renew 天数 [有效天数] [可用次数]</code>\n示例：<code>/gencode register 30</code>、<code>/gencode renew 90 7 5</code>"}
	}
	kind := strings.ToLower(strings.TrimSpace(args[0]))
	switch kind {
	case "reg", "register", "注册码":
		kind = model.RegistrationCodeRegister
	case "renew", "续期", "续期码":
		kind = model.RegistrationCodeRenew
	default:
		return telegramCommandReply{Text: "类型无效，只支持 register / renew。"}
	}
	days, err := strconv.Atoi(args[1])
	if err != nil || days < 0 {
		return telegramCommandReply{Text: "天数必须是非负整数，0 表示永久。"}
	}
	validDays := 0
	if len(args) > 2 {
		validDays, err = strconv.Atoi(args[2])
		if err != nil || validDays < 0 {
			return telegramCommandReply{Text: "有效天数必须是非负整数。"}
		}
	}
	maxUses := 1
	if len(args) > 3 {
		maxUses, err = strconv.Atoi(args[3])
		if err != nil || maxUses <= 0 {
			return telegramCommandReply{Text: "可用次数必须是正整数。"}
		}
	}
	createdBy := ""
	if u := s.boundUser(ctx, msg.From.ID); u != nil {
		createdBy = u.ID
	}
	code, err := s.generateCodeWithUses(ctx, kind, days, validDays, maxUses, createdBy)
	if err != nil {
		return telegramCommandReply{Text: "生成失败：" + err.Error()}
	}
	kindLabel := map[string]string{model.RegistrationCodeRegister: "注册码", model.RegistrationCodeRenew: "续期码"}[code.Kind]
	dur := "永久"
	if days > 0 {
		dur = fmt.Sprintf("%d 天", days)
	}
	valid := "长期有效"
	if validDays > 0 && code.ExpiresAt != nil {
		valid = "有效至 " + code.ExpiresAt.Format("2006-01-02 15:04")
	}
	uses := "单次使用"
	if code.EffectiveMaxUses() > 1 {
		uses = fmt.Sprintf("最多 %d 次", code.EffectiveMaxUses())
	}
	return telegramCommandReply{Text: fmt.Sprintf("已生成%s（%s，%s，%s）：\n\n<code>%s</code>", kindLabel, dur, valid, uses, code.Code)}
}

func (s *TelegramBotService) replyUserList(ctx context.Context) telegramCommandReply {
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return telegramCommandReply{Text: "读取用户失败：" + err.Error()}
	}
	if len(users) == 0 {
		return telegramCommandReply{Text: "暂无用户。"}
	}
	var rows [][]telegramInlineButton
	limit := len(users)
	if limit > 12 {
		limit = 12
	}
	for i := 0; i < limit; i++ {
		u := users[i]
		flag := ""
		if !u.IsActive {
			flag = "🚫"
		}
		if u.Role == "admin" {
			flag = "👑"
		}
		rows = append(rows, []telegramInlineButton{{Text: flag + " " + u.Username, Data: "usr:" + u.ID}})
	}
	rows = append(rows, []telegramInlineButton{{Text: "⬅️ 返回菜单", Data: "menu_main"}})
	return telegramCommandReply{Text: fmt.Sprintf("<b>用户管理</b>（共 %d 人，显示前 %d）\n点击用户进行操作：", len(users), limit), Buttons: rows}
}

func (s *TelegramBotService) replyUserActions(ctx context.Context, userID string) telegramCommandReply {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil || u == nil {
		return telegramCommandReply{Text: "用户不存在。"}
	}
	protected := UserIsProtectedAccount(ctx, s.repo, u)
	text := fmt.Sprintf("<b>%s</b>\n角色：%s\n状态：%s\n到期：%s\n防共享警告：%d 次",
		u.Username, u.Role, map[bool]string{true: "正常", false: "已禁用"}[u.IsActive], formatExpiry(u.ExpiredAt), u.ShareWarnings)
	if protected {
		return telegramCommandReply{Text: text + "\n\n（受保护账号，不可禁用/删除）", Buttons: [][]telegramInlineButton{{{Text: "⬅️ 返回", Data: "adm_users"}}}}
	}
	banBtn := telegramInlineButton{Text: "🚫 禁用", Data: "uban:" + u.ID}
	if !u.IsActive {
		banBtn = telegramInlineButton{Text: "✅ 解禁", Data: "uunban:" + u.ID}
	}
	return telegramCommandReply{
		Text: text,
		Buttons: [][]telegramInlineButton{
			{banBtn, {Text: "⏳ 续期30天", Data: "urenew:" + u.ID + ":30"}},
			{{Text: "🗑 删除用户", Data: "udel:" + u.ID}},
			{{Text: "⬅️ 返回", Data: "adm_users"}},
		},
	}
}

func (s *TelegramBotService) replyUserBan(ctx context.Context, userID string, unban bool) telegramCommandReply {
	if !unban {
		if reason := s.protectReason(ctx, userID); reason != "" {
			return telegramCommandReply{Text: reason}
		}
	}
	updates := map[string]any{"is_active": unban}
	if unban {
		updates["share_warnings"] = 0
		updates["last_share_warn_at"] = nil
	}
	if err := s.repo.User.UpdateFields(ctx, userID, updates); err != nil {
		return telegramCommandReply{Text: "操作失败：" + err.Error()}
	}
	if unban {
		_ = s.repo.UserDevice.SetKickedByUser(ctx, userID, false)
	}
	return s.replyUserActions(ctx, userID)
}

func (s *TelegramBotService) replyUserDelete(ctx context.Context, userID string) telegramCommandReply {
	if reason := s.protectReason(ctx, userID); reason != "" {
		return telegramCommandReply{Text: reason}
	}
	u, _ := s.repo.User.FindByID(ctx, userID)
	_ = s.repo.UserDevice.DeleteByUser(ctx, userID)
	if err := s.repo.User.Delete(ctx, userID); err != nil {
		return telegramCommandReply{Text: "删除失败：" + err.Error()}
	}
	name := userID
	if u != nil {
		name = u.Username
	}
	return telegramCommandReply{Text: fmt.Sprintf("已删除用户 <b>%s</b>。", name), Buttons: [][]telegramInlineButton{{{Text: "⬅️ 返回", Data: "adm_users"}}}}
}

func (s *TelegramBotService) replyUserRenew(ctx context.Context, payload string) telegramCommandReply {
	parts := strings.Split(payload, ":") // <id>:<days>
	if len(parts) != 2 {
		return telegramCommandReply{Text: "参数错误。"}
	}
	days, _ := strconv.Atoi(parts[1])
	if err := s.applyRenewal(ctx, parts[0], days); err != nil {
		return telegramCommandReply{Text: "续期失败：" + err.Error()}
	}
	return s.replyUserActions(ctx, parts[0])
}

func (s *TelegramBotService) cmdUserRenew(ctx context.Context, args []string) telegramCommandReply {
	if len(args) < 2 {
		return telegramCommandReply{Text: "用法：<code>/renew_user 用户名 天数</code>，天数 0 表示永久。"}
	}
	user, _ := s.repo.User.FindByUsername(ctx, args[0])
	if user == nil {
		user, _ = s.repo.User.FindByID(ctx, args[0])
	}
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	days, err := strconv.Atoi(args[1])
	if err != nil || days < 0 {
		return telegramCommandReply{Text: "天数必须是非负整数。"}
	}
	if err := s.applyRenewal(ctx, user.ID, days); err != nil {
		return telegramCommandReply{Text: "续期失败：" + err.Error()}
	}
	return s.replyUserActions(ctx, user.ID)
}

func (s *TelegramBotService) cmdUserDelete(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "用法：<code>/delete_user 用户名 confirm</code>\n为避免误删，最后一个参数必须是 confirm。"}
	}
	if len(args) < 2 || !strings.EqualFold(args[len(args)-1], "confirm") {
		return telegramCommandReply{Text: "删除用户需要确认：<code>/delete_user 用户名 confirm</code>"}
	}
	user, _ := s.repo.User.FindByUsername(ctx, args[0])
	if user == nil {
		user, _ = s.repo.User.FindByID(ctx, args[0])
	}
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	return s.replyUserDelete(ctx, user.ID)
}

func (s *TelegramBotService) cmdUnbind(ctx context.Context, args []string) telegramCommandReply {
	targets := parseTelegramUnbindTargets(args)
	if len(targets) == 0 {
		return telegramCommandReply{Text: "用法：<code>/unbind 用户名1 用户名2</code>\n也支持逗号分隔，或使用 <code>tg:TelegramID</code> 按 Telegram ID 解绑。此命令只解绑 Bot，不删除媒体账号。"}
	}
	var removed int64
	var done []string
	var skipped []string
	var missing []string
	for _, target := range targets {
		if tgIDRaw, ok := strings.CutPrefix(strings.ToLower(target), "tg:"); ok {
			tgID, err := strconv.ParseInt(tgIDRaw, 10, 64)
			if err != nil || tgID == 0 {
				missing = append(missing, target)
				continue
			}
			n, err := s.deleteTelegramBindings(ctx, "telegram_user_id = ?", tgID)
			if err != nil {
				return telegramCommandReply{Text: "解绑失败：" + err.Error()}
			}
			if n == 0 {
				missing = append(missing, target)
				continue
			}
			removed += n
			done = append(done, target)
			continue
		}

		user, _ := s.repo.User.FindByUsername(ctx, target)
		if user == nil {
			user, _ = s.repo.User.FindByID(ctx, target)
		}
		if user == nil {
			missing = append(missing, target)
			continue
		}
		if user.Role == "admin" {
			skipped = append(skipped, user.Username+"(管理员)")
			continue
		}
		n, err := s.deleteTelegramBindings(ctx, "user_id = ?", user.ID)
		if err != nil {
			return telegramCommandReply{Text: "解绑失败：" + err.Error()}
		}
		if n == 0 {
			missing = append(missing, user.Username+"(未绑定)")
			continue
		}
		removed += n
		done = append(done, user.Username)
	}
	return formatUnbindResult("批量解绑完成", removed, done, skipped, missing)
}

func (s *TelegramBotService) cmdUnbindDuplicates(ctx context.Context) telegramCommandReply {
	if s == nil || s.repo == nil || s.repo.DB == nil {
		return telegramCommandReply{Text: "仓库不可用。"}
	}
	var bindings []model.TelegramBinding
	if err := s.repo.DB.WithContext(ctx).Order("updated_at desc, created_at desc").Find(&bindings).Error; err != nil {
		return telegramCommandReply{Text: "读取绑定失败：" + err.Error()}
	}
	seenTelegram := make(map[int64]string)
	seenUser := make(map[string]string)
	var removeIDs []string
	var removedLabels []string
	for _, binding := range bindings {
		remove := false
		if binding.UserID == "" || binding.TelegramUserID == 0 {
			remove = true
		} else if user, _ := s.repo.User.FindByID(ctx, binding.UserID); user == nil {
			remove = true
		} else if _, ok := seenTelegram[binding.TelegramUserID]; ok {
			remove = true
		} else if _, ok := seenUser[binding.UserID]; ok {
			remove = true
		}
		if remove {
			removeIDs = append(removeIDs, binding.ID)
			removedLabels = append(removedLabels, fmt.Sprintf("tg:%d", binding.TelegramUserID))
			continue
		}
		seenTelegram[binding.TelegramUserID] = binding.ID
		seenUser[binding.UserID] = binding.ID
	}
	if len(removeIDs) == 0 {
		return telegramCommandReply{Text: "未发现重复或无效绑定。"}
	}
	n, err := s.deleteTelegramBindings(ctx, "id IN ?", removeIDs)
	if err != nil {
		return telegramCommandReply{Text: "清理失败：" + err.Error()}
	}
	return formatUnbindResult("重复/无效绑定清理完成", n, removedLabels, nil, nil)
}

func (s *TelegramBotService) cmdUnbindInactive(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "用法：<code>/unbind_inactive 天数</code>\n例如 <code>/unbind_inactive 30</code> 会解绑 30 天未登录的普通用户 Bot 绑定，不删除账号。"}
	}
	days, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || days < 1 {
		return telegramCommandReply{Text: "天数必须是大于 0 的整数。"}
	}
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return telegramCommandReply{Text: "读取用户失败：" + err.Error()}
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var userIDs []string
	var done []string
	for _, user := range users {
		if user.Role == "admin" {
			continue
		}
		lastActive := user.CreatedAt
		if user.LastLoginAt != nil {
			lastActive = *user.LastLoginAt
		}
		if lastActive.IsZero() || lastActive.After(cutoff) {
			continue
		}
		var count int64
		_ = s.repo.DB.WithContext(ctx).Model(&model.TelegramBinding{}).Where("user_id = ?", user.ID).Count(&count).Error
		if count == 0 {
			continue
		}
		userIDs = append(userIDs, user.ID)
		done = append(done, user.Username)
	}
	if len(userIDs) == 0 {
		return telegramCommandReply{Text: fmt.Sprintf("未发现 %d 天未登录且已绑定 Bot 的普通用户。", days)}
	}
	n, err := s.deleteTelegramBindings(ctx, "user_id IN ?", userIDs)
	if err != nil {
		return telegramCommandReply{Text: "解绑失败：" + err.Error()}
	}
	return formatUnbindResult(fmt.Sprintf("已解绑 %d 天未登录用户", days), n, done, nil, nil)
}

func parseTelegramUnbindTargets(args []string) []string {
	seen := make(map[string]struct{})
	var targets []string
	for _, arg := range args {
		for _, part := range strings.FieldsFunc(arg, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' || r == '\t'
		}) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key := strings.ToLower(part)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			targets = append(targets, part)
		}
	}
	return targets
}

func (s *TelegramBotService) deleteTelegramBindings(ctx context.Context, query string, args ...interface{}) (int64, error) {
	if s == nil || s.repo == nil || s.repo.DB == nil {
		return 0, nil
	}
	tx := s.repo.DB.WithContext(ctx).Unscoped().Where(query, args...).Delete(&model.TelegramBinding{})
	return tx.RowsAffected, tx.Error
}

func formatUnbindResult(title string, removed int64, done, skipped, missing []string) telegramCommandReply {
	var sb strings.Builder
	sb.WriteString("<b>")
	sb.WriteString(title)
	sb.WriteString("</b>\n\n")
	sb.WriteString(fmt.Sprintf("已解绑：<b>%d</b> 条绑定", removed))
	if len(done) > 0 {
		sb.WriteString("\n目标：")
		sb.WriteString(formatShortList(done, 12))
	}
	if len(skipped) > 0 {
		sb.WriteString("\n跳过：")
		sb.WriteString(formatShortList(skipped, 8))
	}
	if len(missing) > 0 {
		sb.WriteString("\n未找到/未绑定：")
		sb.WriteString(formatShortList(missing, 8))
	}
	return telegramCommandReply{Text: sb.String()}
}

func formatShortList(items []string, limit int) string {
	if len(items) == 0 {
		return ""
	}
	if limit < 1 {
		limit = 1
	}
	out := items
	if len(out) > limit {
		out = out[:limit]
	}
	text := "<code>" + strings.Join(out, "</code>、<code>") + "</code>"
	if len(items) > limit {
		text += fmt.Sprintf(" 等 %d 项", len(items))
	}
	return text
}

// protectReason returns a non-empty message when a user must not be
// disabled/deleted (admins, default admin and protected-list users).
func (s *TelegramBotService) protectReason(ctx context.Context, userID string) string {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil || u == nil {
		return "用户不存在。"
	}
	if u.Role == "admin" {
		return "管理员账号受保护，不可禁用/删除。"
	}
	if first, _ := s.repo.User.FirstAdmin(ctx); first != nil && first.ID == u.ID {
		return "默认管理员账号受保护，不可禁用/删除。"
	}
	if _, ok := ProtectedUserIDSet(ctx, s.repo)[u.ID]; ok {
		return "该账号在 Bot 保护名单中，不可禁用/删除。"
	}
	return ""
}

func (s *TelegramBotService) replyDevicePolicy(ctx context.Context) telegramCommandReply {
	cfg := loadBotConfig(ctx, s.repo)
	text := fmt.Sprintf(
		"<b>设备策略</b>\n\n① 防共享：<b>%s</b>\n   并发播放终端上限 %d / 登录终端上限 %d；同一终端多个 App 只算 1 台，App 作为登录渠道记录。\n   设备指纹异常警告 %d 次后禁用账号。\n\n② Mgo 保号规则：<b>%s</b>\n   保号模式：%s；启用规则 %d 条。\n\n<b>命令：</b>\n<code>/antishare on play=3 login=3 warn=2</code>\n<code>/cleanup run</code> 预览候选\n<code>/cleanup run confirm</code> 确认清理\n<code>/cleanup on|off</code>\n<code>/cleanup_rule list|add|edit|修改|del|enable|disable</code>\n\n策略默认关闭；清理前会先预览候选；满足任意一条保号规则即保留；管理员/受保护账号永不自动处理。",
		onOff(cfg.AntiShareEnabled), cfg.MaxConcurrentPlay, cfg.MaxLoggedClients, cfg.WarnThreshold,
		onOff(cfg.AccountCleanupEnabled), cleanupModeLabel(cfg.AccountCleanupKeepMode), countEnabledCleanupRules(cfg.AccountCleanupRules))
	return telegramCommandReply{
		Text: text,
		Buttons: [][]telegramInlineButton{
			{{Text: toggleLabel("防共享", cfg.AntiShareEnabled), Data: "dp_toggle:antishare"}},
			{{Text: toggleLabel("保号规则", cfg.AccountCleanupEnabled), Data: "dp_toggle:cleanup"}},
			{{Text: "⬅️ 返回菜单", Data: "menu_main"}},
		},
	}
}

func (s *TelegramBotService) cmdDevicePolicy(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		return s.replyDevicePolicy(ctx)
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "run", "sweep":
		return s.cmdCleanup(ctx, []string{"run"})
	default:
		return telegramCommandReply{Text: "用法：<code>/devicepolicy</code> 查看策略，或使用 <code>/antishare</code>、<code>/cleanup</code>、<code>/cleanup_rule</code> 管理。"}
	}
}

func (s *TelegramBotService) cmdAntiShare(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		return s.replyDevicePolicy(ctx)
	}
	enabled, ok := parseCommandBool(args[0])
	if !ok {
		return telegramCommandReply{Text: "用法：<code>/antishare on|off [play=3] [login=3] [warn=2]</code>，login 表示登录终端设备上限，同一终端多个 App 不重复计数。"}
	}
	if err := s.repo.Setting.Set(ctx, SettingAntiShareEnabled, strconv.FormatBool(enabled)); err != nil {
		return telegramCommandReply{Text: "更新失败：" + err.Error()}
	}
	for _, arg := range args[1:] {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "play", "maxplay", "播放":
			_ = s.repo.Setting.Set(ctx, SettingMaxConcurrentPlay, strconv.Itoa(n))
		case "login", "client", "clients", "登录":
			_ = s.repo.Setting.Set(ctx, SettingMaxLoggedClients, strconv.Itoa(n))
		case "warn", "warnings", "警告":
			_ = s.repo.Setting.Set(ctx, SettingWarnThreshold, strconv.Itoa(n))
		}
	}
	return s.replyDevicePolicy(ctx)
}

func (s *TelegramBotService) cmdCleanup(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		return s.replyDevicePolicy(ctx)
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on", "true", "1", "开启", "enable":
		if err := s.repo.Setting.Set(ctx, SettingAccountCleanupEnabled, "true"); err != nil {
			return telegramCommandReply{Text: "开启失败：" + err.Error()}
		}
		return s.replyDevicePolicy(ctx)
	case "off", "false", "0", "关闭", "disable":
		if err := s.repo.Setting.Set(ctx, SettingAccountCleanupEnabled, "false"); err != nil {
			return telegramCommandReply{Text: "关闭失败：" + err.Error()}
		}
		return s.replyDevicePolicy(ctx)
	case "run", "sweep", "巡检", "preview", "预览":
		device := s.device
		if device == nil {
			device = NewDeviceService(s.log, s.repo)
		}
		if len(args) > 1 && isCleanupConfirmArg(args[1]) {
			cfg := loadBotConfig(ctx, s.repo)
			if !cfg.AccountCleanupEnabled {
				return telegramCommandReply{Text: "保号规则未开启，不会清理账号。"}
			}
			if countEnabledCleanupRules(cfg.AccountCleanupRules) == 0 {
				return telegramCommandReply{Text: "没有启用的保号规则，不会清理账号。"}
			}
			removed, err := device.SweepAccountCleanup(ctx)
			if err != nil {
				return telegramCommandReply{Text: "确认清理失败：" + err.Error()}
			}
			return telegramCommandReply{Text: fmt.Sprintf("保号规则确认清理完成，已清理 <b>%d</b> 个账号。", removed)}
		}
		candidates, err := device.PreviewAccountCleanup(ctx)
		if err != nil {
			return telegramCommandReply{Text: "巡检预览失败：" + err.Error()}
		}
		return telegramCommandReply{Text: s.formatCleanupPreview(ctx, candidates)}
	default:
		return telegramCommandReply{Text: "用法：<code>/cleanup on|off</code>、<code>/cleanup run</code> 预览、<code>/cleanup run confirm</code> 确认清理"}
	}
}

func isCleanupConfirmArg(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "confirm", "yes", "delete", "确认", "清理", "删除":
		return true
	default:
		return false
	}
}

func (s *TelegramBotService) formatCleanupPreview(ctx context.Context, candidates []accountCleanupCandidate) string {
	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AccountCleanupEnabled {
		return "保号规则未开启，不会清理账号。"
	}
	if countEnabledCleanupRules(cfg.AccountCleanupRules) == 0 {
		return "没有启用的保号规则，不会清理账号。"
	}
	if len(candidates) == 0 {
		return "保号规则预览完成：没有需要清理的账号。"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>保号规则预览</b>\n\n将清理候选：<b>%d</b> 个账号。\n当前只是预览，未删除任何账号。\n\n", len(candidates)))
	limit := len(candidates)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		candidate := candidates[i]
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n%s\n", i+1, escapeHTML(candidate.Username), escapeHTML(candidate.Details)))
	}
	if len(candidates) > limit {
		sb.WriteString(fmt.Sprintf("……另有 %d 个候选未展示。\n", len(candidates)-limit))
	}
	sb.WriteString("\n确认无误后再执行：<code>/cleanup run confirm</code>")
	return sb.String()
}

func (s *TelegramBotService) cmdCleanupMode(ctx context.Context, args []string) telegramCommandReply {
	if err := s.repo.Setting.Set(ctx, SettingAccountCleanupKeepMode, "any"); err != nil {
		return telegramCommandReply{Text: "更新失败：" + err.Error()}
	}
	if err := s.repo.Setting.Set(ctx, SettingAccountCleanupRequiredCount, "1"); err != nil {
		return telegramCommandReply{Text: "更新失败：" + err.Error()}
	}
	reply := s.replyDevicePolicy(ctx)
	reply.Text = "Mgo 保号模式固定为：满足任意一条启用规则即保留；只有全部规则都不满足才进入清理候选。\n\n" + reply.Text
	return reply
}

func (s *TelegramBotService) cmdCleanupRule(ctx context.Context, args []string) telegramCommandReply {
	rules := s.currentCleanupRules(ctx)
	if len(args) == 0 {
		return telegramCommandReply{Text: formatCleanupRules(rules)}
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	switch action {
	case "list", "ls", "status":
		return telegramCommandReply{Text: formatCleanupRules(rules)}
	case "help", "?", "帮助":
		return telegramCommandReply{Text: cleanupRuleHelp()}
	case "del", "delete", "rm":
		if len(args) < 2 {
			return telegramCommandReply{Text: "用法：<code>/cleanup_rule del 规则ID</code>"}
		}
		next := make([]accountCleanupRule, 0, len(rules))
		removed := false
		for _, r := range rules {
			if r.ID == args[1] {
				removed = true
				continue
			}
			next = append(next, r)
		}
		if !removed {
			return telegramCommandReply{Text: "未找到该规则。"}
		}
		if err := s.saveCleanupRules(ctx, next); err != nil {
			return telegramCommandReply{Text: "保存失败：" + err.Error()}
		}
		return telegramCommandReply{Text: "已删除规则。\n\n" + formatCleanupRules(next)}
	case "enable", "on", "disable", "off":
		if len(args) < 2 {
			return telegramCommandReply{Text: "用法：<code>/cleanup_rule enable|disable 规则ID</code>"}
		}
		enable := action == "enable" || action == "on"
		changed := false
		for i := range rules {
			if rules[i].ID == args[1] {
				rules[i].Enabled = enable
				changed = true
			}
		}
		if !changed {
			return telegramCommandReply{Text: "未找到该规则。"}
		}
		if err := s.saveCleanupRules(ctx, rules); err != nil {
			return telegramCommandReply{Text: "保存失败：" + err.Error()}
		}
		return telegramCommandReply{Text: "已更新规则状态。\n\n" + formatCleanupRules(rules)}
	case "add", "set", "edit", "update", "修改", "更新", "改":
		rule, err := parseCleanupRuleCommand(args[1:])
		if err != nil {
			return telegramCommandReply{Text: err.Error() + "\n\n" + cleanupRuleHelp()}
		}
		updated := false
		for i := range rules {
			if rules[i].ID == rule.ID {
				rules[i] = rule
				updated = true
				break
			}
		}
		if !updated {
			rules = append(rules, rule)
		}
		rules = normalizeCleanupRules(rules)
		if err := s.saveCleanupRules(ctx, rules); err != nil {
			return telegramCommandReply{Text: "保存失败：" + err.Error()}
		}
		actionText := "已新增规则。"
		if updated {
			actionText = "已更新规则。"
		}
		return telegramCommandReply{Text: actionText + "\n\n" + formatCleanupRules(rules)}
	default:
		return telegramCommandReply{Text: cleanupRuleHelp()}
	}
}

func (s *TelegramBotService) replyDevicePolicyToggle(ctx context.Context, which string) telegramCommandReply {
	cfg := loadBotConfig(ctx, s.repo)
	switch which {
	case "antishare":
		_ = s.repo.Setting.Set(ctx, SettingAntiShareEnabled, strconv.FormatBool(!cfg.AntiShareEnabled))
	case "cleanup":
		_ = s.repo.Setting.Set(ctx, SettingAccountCleanupEnabled, strconv.FormatBool(!cfg.AccountCleanupEnabled))
	}
	return s.replyDevicePolicy(ctx)
}

func (s *TelegramBotService) cmdUserBan(ctx context.Context, args []string, unban bool) telegramCommandReply {
	if len(args) == 0 {
		if unban {
			return telegramCommandReply{Text: "用法：<code>/unban 用户名</code>"}
		}
		return telegramCommandReply{Text: "用法：<code>/ban 用户名</code>"}
	}
	user, _ := s.repo.User.FindByUsername(ctx, args[0])
	if user == nil {
		user, _ = s.repo.User.FindByID(ctx, args[0])
	}
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	return s.replyUserBan(ctx, user.ID, unban)
}

func (s *TelegramBotService) currentCleanupRules(ctx context.Context) []accountCleanupRule {
	cfg := loadBotConfig(ctx, s.repo)
	return cfg.AccountCleanupRules
}

func (s *TelegramBotService) saveCleanupRules(ctx context.Context, rules []accountCleanupRule) error {
	raw, err := json.Marshal(normalizeCleanupRules(rules))
	if err != nil {
		return err
	}
	return s.repo.Setting.Set(ctx, SettingAccountCleanupRules, string(raw))
}

func parseCommandBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1", "yes", "enable", "enabled", "开启", "开":
		return true, true
	case "off", "false", "0", "no", "disable", "disabled", "关闭", "关":
		return false, true
	default:
		return false, false
	}
}

func parseCleanupRuleCommand(args []string) (accountCleanupRule, error) {
	if len(args) < 2 {
		return accountCleanupRule{}, fmt.Errorf("新增规则参数不足")
	}
	rule := accountCleanupRule{
		Type:          strings.ToLower(strings.TrimSpace(args[0])),
		ID:            strings.TrimSpace(args[1]),
		Enabled:       true,
		WindowDaysMin: 3,
		WindowDaysMax: 5,
		MinHours:      6,
		MinCount:      1,
	}
	switch rule.Type {
	case "watch_hours":
		name, values := cleanupRuleNameAndValues(args[2:], 3)
		rule.Name = name
		if len(values) >= 3 {
			rule.WindowDaysMin, _ = strconv.Atoi(values[0])
			rule.WindowDaysMax, _ = strconv.Atoi(values[1])
			rule.MinHours, _ = strconv.ParseFloat(values[2], 64)
			if rule.Name == "" {
				rule.Name = fmt.Sprintf("%d~%d 天观看满 %s 小时", rule.WindowDaysMin, rule.WindowDaysMax, formatRuleHours(rule.MinHours))
			}
		}
	case "recent_login":
		name, values := cleanupRuleNameAndValues(args[2:], 1)
		rule.Name = name
		if len(values) >= 1 {
			rule.WindowDaysMax, _ = strconv.Atoi(values[0])
			if rule.Name == "" {
				rule.Name = fmt.Sprintf("%d 天内登录", rule.WindowDaysMax)
			}
		}
	case "signin_streak", "account_age_grace":
		name, values := cleanupRuleNameAndValues(args[2:], 1)
		rule.Name = name
		if len(values) >= 1 {
			rule.MinCount, _ = strconv.Atoi(values[0])
			if rule.Name == "" {
				if rule.Type == "signin_streak" {
					rule.Name = fmt.Sprintf("连续签到 %d 天", rule.MinCount)
				} else {
					rule.Name = fmt.Sprintf("新号宽限 %d 天", rule.MinCount)
				}
			}
		}
	default:
		return accountCleanupRule{}, fmt.Errorf("不支持的规则类型：%s", rule.Type)
	}
	normalized := normalizeCleanupRules([]accountCleanupRule{rule})
	if len(normalized) == 0 {
		return accountCleanupRule{}, fmt.Errorf("规则无效")
	}
	return normalized[0], nil
}

func cleanupRuleNameAndValues(args []string, numericCount int) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) >= numericCount && cleanupRuleValuesAreNumeric(args[:numericCount]) {
		return "", args
	}
	return strings.TrimSpace(args[0]), args[1:]
}

func cleanupRuleValuesAreNumeric(values []string) bool {
	for _, value := range values {
		if _, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err != nil {
			return false
		}
	}
	return true
}

func formatCleanupRules(rules []accountCleanupRule) string {
	if len(rules) == 0 {
		return "<b>保号规则</b>\n\n暂无规则。"
	}
	var sb strings.Builder
	sb.WriteString("<b>保号规则</b>\n")
	for i, r := range rules {
		state := map[bool]string{true: "启用", false: "停用"}[r.Enabled]
		detail := cleanupRuleDetail(r)
		parts := []string{
			fmt.Sprintf("\n%d. <code>%s</code>", i+1, r.ID),
		}
		if shouldShowCleanupRuleName(r, detail) {
			parts = append(parts, r.Name)
		}
		parts = append(parts, cleanupRuleTypeLabel(r.Type), state)
		if detail != "" {
			parts = append(parts, detail)
		}
		sb.WriteString(strings.Join(parts, " · "))
	}
	return sb.String()
}

func shouldShowCleanupRuleName(r accountCleanupRule, detail string) bool {
	name := strings.TrimSpace(r.Name)
	if name == "" || strings.EqualFold(name, r.ID) {
		return false
	}
	if detail != "" && strings.EqualFold(name, detail) {
		return false
	}
	return true
}

func cleanupRuleDetail(r accountCleanupRule) string {
	switch r.Type {
	case "watch_hours":
		return fmt.Sprintf("%d~%d 天 %s 小时", r.WindowDaysMin, r.WindowDaysMax, formatRuleHours(r.MinHours))
	case "recent_login":
		return fmt.Sprintf("%d 天内登录", r.WindowDaysMax)
	case "signin_streak":
		return fmt.Sprintf("连续签到 %d 天", r.MinCount)
	case "account_age_grace":
		return fmt.Sprintf("新号宽限 %d 天", r.MinCount)
	default:
		return ""
	}
}

func formatRuleHours(hours float64) string {
	if hours == float64(int(hours)) {
		return strconv.Itoa(int(hours))
	}
	return fmt.Sprintf("%.1f", hours)
}

func cleanupRuleTypeLabel(t string) string {
	switch t {
	case "watch_hours":
		return "观看时长"
	case "recent_login":
		return "最近登录"
	case "signin_streak":
		return "连续签到"
	case "account_age_grace":
		return "新号宽限"
	default:
		return t
	}
}

func cleanupRuleHelp() string {
	return "<b>Mgo 保号规则命令</b>\n\n" +
		"<code>/cleanup_rule list</code> — 查看规则\n" +
		"<code>/cleanup_rule add watch_hours watch_3_5d_6h 观看3到5天满6小时 3 5 6</code>\n" +
		"<code>/cleanup_rule add recent_login login_7d 七天内登录 7</code>\n" +
		"<code>/cleanup_rule add signin_streak sign_3 连续签到3天 3</code>\n" +
		"<code>/cleanup_rule add account_age_grace new_7d 新号宽限7天 7</code>\n" +
		"<code>/cleanup_rule edit 规则类型 规则ID 名称 参数...</code> — 修改同 ID 规则\n" +
		"<code>/cleanup_rule 修改 规则类型 规则ID 名称 参数...</code> — 中文修改入口\n" +
		"<code>/cleanup_rule enable 规则ID</code> / <code>disable 规则ID</code>\n" +
		"<code>/cleanup_rule del 规则ID</code>\n\n" +
		"保号模式固定为：满足任意一条启用规则即保留；全部不满足才会清理。"
}

func onOff(b bool) string {
	return map[bool]string{true: "已开启", false: "已关闭"}[b]
}

func toggleLabel(name string, enabled bool) string {
	if enabled {
		return "关闭" + name
	}
	return "开启" + name
}

func cleanupModeLabel(mode string) string {
	return "满足任意一条"
}

func countEnabledCleanupRules(rules []accountCleanupRule) int {
	n := 0
	for _, r := range rules {
		if r.Enabled {
			n++
		}
	}
	return n
}

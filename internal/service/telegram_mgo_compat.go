package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *TelegramBotService) cmdMgoCreateUser(ctx context.Context, args []string) telegramCommandReply {
	if len(args) < 2 {
		return telegramCommandReply{Text: "用法：<code>/ucr 用户名 密码 [天数]</code>，天数 0 表示永久。"}
	}
	if s.auth == nil {
		return telegramCommandReply{Text: "注册服务暂不可用。"}
	}
	user, _, err := s.auth.Register(ctx, args[0], args[1])
	if err != nil {
		return telegramCommandReply{Text: "创建失败：" + err.Error()}
	}
	days := 0
	if len(args) >= 3 {
		parsed, err := strconv.Atoi(args[2])
		if err != nil || parsed < 0 {
			return telegramCommandReply{Text: "账号已创建，但天数无效。请用 <code>/renew 用户名 天数</code> 调整。"}
		}
		days = parsed
		if err := s.applyRenewal(ctx, user.ID, days); err != nil {
			return telegramCommandReply{Text: "账号已创建，但续期失败：" + err.Error()}
		}
	}
	return telegramCommandReply{Text: fmt.Sprintf("已创建用户：<b>%s</b>\n到期：<b>%s</b>", user.Username, formatExpiry(s.userExpiry(ctx, user.ID)))}
}

func (s *TelegramBotService) cmdMgoUserInfo(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "用法：<code>/uinfo 用户名</code>"}
	}
	user := s.findMgoBotUser(ctx, args[0])
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	devices, _ := s.repo.UserDevice.ListByUser(ctx, user.ID)
	var historyCount int64
	_ = s.repo.DB.WithContext(ctx).Model(&model.PlaybackHistory{}).Where("user_id = ?", user.ID).Count(&historyCount).Error
	var binding model.TelegramBinding
	tg := "未绑定"
	if err := s.repo.DB.WithContext(ctx).Where("user_id = ?", user.ID).First(&binding).Error; err == nil {
		tg = fmt.Sprintf("tg:%d", binding.TelegramUserID)
		if binding.TelegramName != "" {
			tg += " " + binding.TelegramName
		}
	}
	return telegramCommandReply{Text: fmt.Sprintf(
		"<b>用户信息</b>\n\n用户名：<b>%s</b>\n角色：<b>%s</b>\n状态：<b>%s</b>\n到期：<b>%s</b>\nTelegram：<b>%s</b>\n设备：<b>%d</b>\n播放记录：<b>%d</b>\n最后登录：<b>%s</b>",
		user.Username, user.Role, activeLabel(user), formatExpiry(user.ExpiredAt), tg, len(devices), historyCount, formatOptionalTime(user.LastLoginAt),
	)}
}

func (s *TelegramBotService) cmdMgoDeleteUser(ctx context.Context, args []string) telegramCommandReply {
	if len(args) < 2 || !strings.EqualFold(args[len(args)-1], "confirm") {
		return telegramCommandReply{Text: "删除用户需要确认：<code>/rmemby 用户名 confirm</code> 或 <code>/urm 用户名 confirm</code>"}
	}
	user := s.findMgoBotUser(ctx, args[0])
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	if reason := s.protectReason(ctx, user.ID); reason != "" {
		return telegramCommandReply{Text: reason}
	}
	_ = s.repo.UserDevice.DeleteByUser(ctx, user.ID)
	if err := s.repo.User.Delete(ctx, user.ID); err != nil {
		return telegramCommandReply{Text: "删除失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("已删除用户 <b>%s</b>。", user.Username)}
}

func (s *TelegramBotService) cmdMgoOnlyRemoveRecord(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "用法：<code>/only_rm_record tg:123456</code> 或 <code>/only_rm_record 用户名</code>，只删除 Telegram 绑定记录。"}
	}
	target := strings.TrimSpace(args[0])
	var removed int64
	if raw, ok := strings.CutPrefix(strings.ToLower(target), "tg:"); ok {
		tgID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || tgID == 0 {
			return telegramCommandReply{Text: "Telegram ID 无效。"}
		}
		removed, err = s.deleteTelegramBindings(ctx, "telegram_user_id = ?", tgID)
		if err != nil {
			return telegramCommandReply{Text: "删除绑定失败：" + err.Error()}
		}
	} else {
		user := s.findMgoBotUser(ctx, target)
		if user == nil {
			return telegramCommandReply{Text: "未找到用户。"}
		}
		n, err := s.deleteTelegramBindings(ctx, "user_id = ?", user.ID)
		if err != nil {
			return telegramCommandReply{Text: "删除绑定失败：" + err.Error()}
		}
		removed = n
	}
	return telegramCommandReply{Text: fmt.Sprintf("已删除 Telegram 绑定记录：<b>%d</b> 条。", removed)}
}

func (s *TelegramBotService) cmdMgoUserIP(ctx context.Context, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: "用法：<code>/userip 用户名</code>"}
	}
	user := s.findMgoBotUser(ctx, args[0])
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	devices, err := s.repo.UserDevice.ListByUser(ctx, user.ID)
	if err != nil {
		return telegramCommandReply{Text: "查询失败：" + err.Error()}
	}
	if len(devices) == 0 {
		return telegramCommandReply{Text: "该用户暂无设备/IP记录。"}
	}
	var out []string
	for i, d := range devices {
		if i >= 20 {
			break
		}
		out = append(out, fmt.Sprintf("%d. %s / %s / %s / %s", i+1, blankDash(d.LastIP), blankDash(d.DeviceName), blankDash(d.Client), d.LastSeenAt.Format("2006-01-02 15:04")))
	}
	return telegramCommandReply{Text: "<b>" + user.Username + " 的设备/IP</b>\n\n<code>" + strings.Join(out, "\n") + "</code>"}
}

func (s *TelegramBotService) cmdMgoAuditDevices(ctx context.Context, mode string, args []string) telegramCommandReply {
	if len(args) == 0 {
		return telegramCommandReply{Text: fmt.Sprintf("用法：<code>/%s 关键词</code>", mode)}
	}
	keyword := strings.TrimSpace(strings.Join(args, " "))
	var rows []struct {
		Username   string
		DeviceID   string
		DeviceName string
		Client     string
		LastIP     string
		LastSeenAt time.Time
	}
	q := s.repo.DB.WithContext(ctx).Table("user_devices").
		Select("users.username, user_devices.device_id, user_devices.device_name, user_devices.client, user_devices.last_ip, user_devices.last_seen_at").
		Joins("JOIN users ON users.id = user_devices.user_id").
		Order("user_devices.last_seen_at desc").
		Limit(20)
	switch mode {
	case "auditip":
		q = q.Where("user_devices.last_ip LIKE ?", "%"+keyword+"%")
	case "auditdevice":
		q = q.Where("user_devices.device_name LIKE ? OR user_devices.device_id LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	case "auditclient":
		q = q.Where("user_devices.client LIKE ?", "%"+keyword+"%")
	case "udeviceid":
		q = q.Where("user_devices.device_id LIKE ?", "%"+keyword+"%")
	}
	if err := q.Scan(&rows).Error; err != nil {
		return telegramCommandReply{Text: "查询失败：" + err.Error()}
	}
	if len(rows) == 0 {
		return telegramCommandReply{Text: "没有匹配记录。"}
	}
	var out []string
	for i, r := range rows {
		out = append(out, fmt.Sprintf("%d. %s / %s / %s / %s / %s", i+1, r.Username, blankDash(r.LastIP), blankDash(r.DeviceName), blankDash(r.Client), r.LastSeenAt.Format("2006-01-02 15:04")))
	}
	return telegramCommandReply{Text: "<b>审计结果</b>\n\n<code>" + strings.Join(out, "\n") + "</code>"}
}

func (s *TelegramBotService) cmdMgoRenewAll(ctx context.Context, args []string) telegramCommandReply {
	if len(args) < 2 || !strings.EqualFold(args[len(args)-1], "confirm") {
		return telegramCommandReply{Text: "批量续期需要确认：<code>/renewall 天数 confirm</code>"}
	}
	days, err := strconv.Atoi(args[0])
	if err != nil || days < 0 {
		return telegramCommandReply{Text: "天数必须是非负整数，0 表示永久。"}
	}
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return telegramCommandReply{Text: "读取用户失败：" + err.Error()}
	}
	var count int
	for _, user := range users {
		if user.Role == "admin" {
			continue
		}
		if err := s.applyRenewal(ctx, user.ID, days); err == nil {
			count++
		}
	}
	return telegramCommandReply{Text: fmt.Sprintf("批量续期完成：<b>%d</b> 个普通用户。", count)}
}

func (s *TelegramBotService) cmdMgoBanAll(ctx context.Context, active bool, args []string) telegramCommandReply {
	if len(args) == 0 || !strings.EqualFold(args[len(args)-1], "confirm") {
		action := "banall"
		if active {
			action = "unbanall"
		}
		return telegramCommandReply{Text: fmt.Sprintf("批量操作需要确认：<code>/%s confirm</code>", action)}
	}
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return telegramCommandReply{Text: "读取用户失败：" + err.Error()}
	}
	var count int
	for _, user := range users {
		if !active && UserIsProtectedAccount(ctx, s.repo, &user) {
			continue
		}
		if active && user.Role == "admin" {
			continue
		}
		updates := map[string]any{"is_active": active}
		if active {
			updates["share_warnings"] = 0
			updates["last_share_warn_at"] = nil
		}
		if err := s.repo.User.UpdateFields(ctx, user.ID, updates); err == nil {
			_ = s.repo.UserDevice.SetKickedByUser(ctx, user.ID, !active)
			count++
		}
	}
	if active {
		return telegramCommandReply{Text: fmt.Sprintf("已解禁普通用户：<b>%d</b> 个。", count)}
	}
	return telegramCommandReply{Text: fmt.Sprintf("已禁用普通用户：<b>%d</b> 个。", count)}
}

func (s *TelegramBotService) cmdMgoCallAll(ctx context.Context, channel *model.NotifyChannel, args []string) telegramCommandReply {
	message := strings.TrimSpace(strings.Join(args, " "))
	if message == "" {
		return telegramCommandReply{Text: "用法：<code>/callall 消息内容</code>"}
	}
	if strings.TrimSpace(s.telegramChannelConfig(channel)["bot_token"]) == "" {
		return telegramCommandReply{Text: "当前 Telegram 渠道未配置 bot_token，无法群发。"}
	}
	var bindings []model.TelegramBinding
	if err := s.repo.DB.WithContext(ctx).Find(&bindings).Error; err != nil {
		return telegramCommandReply{Text: "读取绑定失败：" + err.Error()}
	}
	sent := 0
	for _, binding := range bindings {
		if binding.ChatID == 0 {
			continue
		}
		if err := s.reply(ctx, channel, int(binding.ChatID), telegramCommandReply{Text: message}); err == nil {
			sent++
		}
	}
	return telegramCommandReply{Text: fmt.Sprintf("群发完成：成功发送 <b>%d</b> 个绑定用户。", sent)}
}

func (s *TelegramBotService) cmdMgoSyncUnbound(ctx context.Context, args []string) telegramCommandReply {
	var users []model.User
	if err := s.repo.DB.WithContext(ctx).
		Where("role <> ?", "admin").
		Where("NOT EXISTS (SELECT 1 FROM telegram_bindings WHERE telegram_bindings.user_id = users.id AND telegram_bindings.deleted_at IS NULL)").
		Order("created_at asc").Find(&users).Error; err != nil {
		return telegramCommandReply{Text: "查询失败：" + err.Error()}
	}
	if len(args) >= 2 && strings.EqualFold(args[0], "delete") && strings.EqualFold(args[1], "confirm") {
		deleted := 0
		for _, user := range users {
			if UserIsProtectedAccount(ctx, s.repo, &user) {
				continue
			}
			_ = s.repo.UserDevice.DeleteByUser(ctx, user.ID)
			if err := s.repo.User.Delete(ctx, user.ID); err == nil {
				deleted++
			}
		}
		return telegramCommandReply{Text: fmt.Sprintf("已删除未绑定 Bot 的普通用户：<b>%d</b> 个。", deleted)}
	}
	if len(users) == 0 {
		return telegramCommandReply{Text: "没有未绑定 Bot 的普通用户。"}
	}
	names := make([]string, 0, minInt(len(users), 20))
	for i, user := range users {
		if i >= 20 {
			break
		}
		names = append(names, user.Username)
	}
	return telegramCommandReply{Text: fmt.Sprintf("未绑定 Bot 的普通用户：<b>%d</b> 个。\n%s\n\n如需删除：<code>/syncunbound delete confirm</code>", len(users), telegramInlineCodeList(names))}
}

func (s *TelegramBotService) cmdMgoCheckExpired(ctx context.Context, args []string) telegramCommandReply {
	now := time.Now()
	var users []model.User
	if err := s.repo.DB.WithContext(ctx).Where("expired_at IS NOT NULL AND expired_at < ?", now).Order("expired_at asc").Find(&users).Error; err != nil {
		return telegramCommandReply{Text: "查询失败：" + err.Error()}
	}
	if len(args) >= 2 && strings.EqualFold(args[0], "disable") && strings.EqualFold(args[1], "confirm") {
		disabled := 0
		for _, user := range users {
			if UserIsProtectedAccount(ctx, s.repo, &user) {
				continue
			}
			if err := s.repo.User.UpdateFields(ctx, user.ID, map[string]any{"is_active": false}); err == nil {
				disabled++
			}
		}
		return telegramCommandReply{Text: fmt.Sprintf("已禁用过期普通用户：<b>%d</b> 个。", disabled)}
	}
	if len(users) == 0 {
		return telegramCommandReply{Text: "没有过期用户。"}
	}
	lines := make([]string, 0, minInt(len(users), 20))
	for i, user := range users {
		if i >= 20 {
			break
		}
		lines = append(lines, fmt.Sprintf("%s（%s）", user.Username, formatExpiry(user.ExpiredAt)))
	}
	return telegramCommandReply{Text: fmt.Sprintf("过期用户：<b>%d</b> 个。\n%s\n\n如需禁用：<code>/check_ex disable confirm</code>", len(users), telegramInlineCodeList(lines))}
}

func (s *TelegramBotService) cmdMgoScanNames(ctx context.Context) telegramCommandReply {
	var rows []struct {
		Username string
		Count    int64
	}
	if err := s.repo.DB.WithContext(ctx).Table("users").
		Select("LOWER(username) AS username, COUNT(*) AS count").
		Group("LOWER(username)").Having("COUNT(*) > 1").Scan(&rows).Error; err != nil {
		return telegramCommandReply{Text: "扫描失败：" + err.Error()}
	}
	if len(rows) == 0 {
		return telegramCommandReply{Text: "未发现同名用户记录。"}
	}
	var out []string
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s x%d", row.Username, row.Count))
	}
	return telegramCommandReply{Text: "<b>同名用户记录</b>\n" + telegramInlineCodeList(out)}
}

func (s *TelegramBotService) cmdMgoRanks(ctx context.Context, window time.Duration, byDuration bool) telegramCommandReply {
	since := time.Now().Add(-window)
	title := "播放次数排行"
	selectExpr := "COUNT(*) AS score"
	if byDuration {
		title = "观影时长排行"
		selectExpr = "COALESCE(SUM(position_ms), 0) AS score"
	}
	q := s.repo.DB.WithContext(ctx).Table("playback_histories").
		Select("users.username, " + selectExpr).
		Joins("JOIN users ON users.id = playback_histories.user_id").
		Group("users.username").
		Order("score DESC").
		Limit(10)
	if window > 0 {
		q = q.Where("playback_histories.watched_at >= ?", since)
	}
	var rows []struct {
		Username string
		Score    int64
	}
	if err := q.Scan(&rows).Error; err != nil {
		return telegramCommandReply{Text: "排行查询失败：" + err.Error()}
	}
	if len(rows) == 0 {
		return telegramCommandReply{Text: "暂无排行数据。"}
	}
	var out []string
	for i, row := range rows {
		score := fmt.Sprintf("%d 次", row.Score)
		if byDuration {
			score = humanDurationFromMillis(row.Score)
		}
		out = append(out, fmt.Sprintf("%d. %s — %s", i+1, row.Username, score))
	}
	return telegramCommandReply{Text: "<b>" + title + "</b>\n\n<code>" + strings.Join(out, "\n") + "</code>"}
}

func (s *TelegramBotService) cmdMgoAdminRole(ctx context.Context, args []string) telegramCommandReply {
	if len(args) < 2 {
		return telegramCommandReply{Text: "用法：<code>/embyadmin 用户名 on|off</code>"}
	}
	user := s.findMgoBotUser(ctx, args[0])
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	enable := parseOnOff(args[1])
	if enable == nil {
		return telegramCommandReply{Text: "第二个参数请使用 on/off。"}
	}
	if !*enable {
		if first, _ := s.repo.User.FirstAdmin(ctx); first != nil && first.ID == user.ID {
			return telegramCommandReply{Text: "默认管理员不可降级。"}
		}
	}
	role := "user"
	if *enable {
		role = "admin"
	}
	if err := s.repo.User.UpdateFields(ctx, user.ID, map[string]any{"role": role}); err != nil {
		return telegramCommandReply{Text: "更新失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("已将 <b>%s</b> 角色设置为 <b>%s</b>。", user.Username, role)}
}

func (s *TelegramBotService) cmdMgoMediaAccessAll(ctx context.Context, allow bool) telegramCommandReply {
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return telegramCommandReply{Text: "读取用户失败：" + err.Error()}
	}
	updated := 0
	for _, user := range users {
		if user.Role == "admin" {
			continue
		}
		perm, err := s.repo.Permission.FindByUserID(ctx, user.ID)
		if err != nil {
			continue
		}
		if perm == nil {
			perm = DefaultPermissions(user.ID)
			perm.CanPlayMedia = allow
			if err := s.repo.Permission.Create(ctx, perm); err != nil {
				continue
			}
		}
		if err := s.repo.DB.WithContext(ctx).Model(&model.UserPermission{}).
			Where("user_id = ?", user.ID).
			Update("can_play_media", allow).Error; err == nil {
			updated++
		}
	}
	state := "关闭"
	if allow {
		state = "开启"
	}
	return telegramCommandReply{Text: fmt.Sprintf("已为普通用户%s媒体播放权限：<b>%d</b> 个。", state, updated)}
}

func (s *TelegramBotService) cmdMgoBotAdmin(ctx context.Context, channel *model.NotifyChannel, args []string, add bool) telegramCommandReply {
	if channel == nil {
		return telegramCommandReply{Text: "Telegram 渠道不存在。"}
	}
	if len(args) == 0 {
		return telegramCommandReply{Text: "用法：<code>/proadmin TelegramID</code> 或 <code>/revadmin TelegramID</code>"}
	}
	tgID := strings.TrimPrefix(strings.TrimSpace(args[0]), "tg:")
	if _, err := strconv.ParseInt(tgID, 10, 64); err != nil {
		return telegramCommandReply{Text: "TelegramID 必须是数字。"}
	}
	cfg := s.telegramChannelConfig(channel)
	ids := telegramConfiguredUserIDs(cfg["admin_user_ids"])
	seen := make(map[string]bool, len(ids)+1)
	var next []string
	for _, id := range ids {
		if id == tgID {
			seen[id] = true
			if add {
				next = append(next, id)
			}
			continue
		}
		if id != "" {
			next = append(next, id)
		}
	}
	if add && !seen[tgID] {
		next = append(next, tgID)
	}
	cfg["admin_user_ids"] = strings.Join(next, ",")
	raw, _ := json.Marshal(cfg)
	updated := *channel
	updated.Config = string(raw)
	if s.crypto != nil {
		updated.Config = s.crypto.Encrypt(updated.Config)
	}
	if err := s.repo.NotifyChannel.Update(ctx, &updated); err != nil {
		return telegramCommandReply{Text: "更新管理员列表失败：" + err.Error()}
	}
	if add {
		return telegramCommandReply{Text: "已添加 Bot 管理员：<code>" + tgID + "</code>"}
	}
	return telegramCommandReply{Text: "已移除 Bot 管理员：<code>" + tgID + "</code>"}
}

func (s *TelegramBotService) cmdMgoProtectedUser(ctx context.Context, args []string, protect bool) telegramCommandReply {
	if len(args) == 0 || strings.EqualFold(args[0], "list") {
		return s.cmdMgoProtectedUserList(ctx)
	}
	user := s.findMgoBotUser(ctx, args[0])
	if user == nil {
		return telegramCommandReply{Text: "未找到用户。"}
	}
	ids := ProtectedUserIDSet(ctx, s.repo)
	if protect {
		ids[user.ID] = struct{}{}
		if err := SaveProtectedUserIDSet(ctx, s.repo, ids); err != nil {
			return telegramCommandReply{Text: "保存保护名单失败：" + err.Error()}
		}
		return telegramCommandReply{Text: fmt.Sprintf("已加入保护名单：<b>%s</b>。\n该用户不会被 Bot 自动清理、批量禁用或删除。", user.Username)}
	}
	delete(ids, user.ID)
	if err := SaveProtectedUserIDSet(ctx, s.repo, ids); err != nil {
		return telegramCommandReply{Text: "保存保护名单失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("已移出保护名单：<b>%s</b>。", user.Username)}
}

func (s *TelegramBotService) cmdMgoProtectedUserList(ctx context.Context) telegramCommandReply {
	ids := ProtectedUserIDSet(ctx, s.repo)
	if len(ids) == 0 {
		return telegramCommandReply{Text: "保护名单为空。管理员和默认管理员始终自动保护。"}
	}
	names := make([]string, 0, len(ids))
	for id := range ids {
		if user, _ := s.repo.User.FindByID(ctx, id); user != nil {
			names = append(names, user.Username)
		} else {
			names = append(names, id+"(用户不存在)")
		}
	}
	sort.Strings(names)
	return telegramCommandReply{Text: fmt.Sprintf("保护名单：<b>%d</b> 个。\n%s", len(names), telegramInlineCodeList(names))}
}

func (s *TelegramBotService) cmdMgoBackupDB(ctx context.Context) telegramCommandReply {
	if s.backup == nil {
		return telegramCommandReply{Text: "备份服务暂不可用。"}
	}
	info, err := s.backup.Create(ctx)
	if err != nil {
		return telegramCommandReply{Text: "数据库备份失败：" + err.Error()}
	}
	return telegramCommandReply{Text: fmt.Sprintf("数据库备份完成：<code>%s</code>\n大小：<b>%d</b> bytes", info.Filename, info.Size)}
}

func (s *TelegramBotService) cmdMgoRestoreDB(ctx context.Context, args []string) telegramCommandReply {
	if s.backup == nil {
		return telegramCommandReply{Text: "备份服务暂不可用。"}
	}
	if len(args) == 0 || strings.EqualFold(args[0], "list") {
		items, err := s.backup.List()
		if err != nil {
			return telegramCommandReply{Text: "读取备份列表失败：" + err.Error()}
		}
		if len(items) == 0 {
			return telegramCommandReply{Text: "暂无数据库备份。可先使用 <code>/backup_db</code> 创建。"}
		}
		lines := make([]string, 0, minInt(len(items), 10))
		for i, item := range items {
			if i >= 10 {
				break
			}
			lines = append(lines, fmt.Sprintf("%s（%d bytes）", item.Filename, item.Size))
		}
		return telegramCommandReply{Text: "可恢复备份：\n" + telegramInlineCodeList(lines) + "\n\n恢复需要确认：<code>/restore_from_db 文件名 confirm</code>"}
	}
	if len(args) < 2 || !strings.EqualFold(args[len(args)-1], "confirm") {
		return telegramCommandReply{Text: "恢复数据库会覆盖当前数据，需要确认：<code>/restore_from_db 文件名 confirm</code>"}
	}
	filename := strings.TrimSpace(args[0])
	if err := s.backup.Restore(ctx, filename); err != nil {
		return telegramCommandReply{Text: "恢复失败：" + err.Error()}
	}
	return telegramCommandReply{Text: "数据库已从备份恢复，请重启 MediaStationGo 后生效。"}
}

func (s *TelegramBotService) cmdMgoSyncGroup(ctx context.Context, channel *model.NotifyChannel, args []string) telegramCommandReply {
	chatIDs := s.telegramMembershipChatIDs(channel)
	if len(chatIDs) == 0 {
		return telegramCommandReply{Text: "未配置可校验成员的群组/频道 ID。请在 Telegram 通知渠道设置 group_chat_id 或 channel_chat_id。"}
	}
	if strings.TrimSpace(s.telegramChannelConfig(channel)["bot_token"]) == "" {
		return telegramCommandReply{Text: "当前 Telegram 渠道未配置 bot_token，无法校验群成员。"}
	}
	var bindings []model.TelegramBinding
	if err := s.repo.DB.WithContext(ctx).Find(&bindings).Error; err != nil {
		return telegramCommandReply{Text: "读取绑定失败：" + err.Error()}
	}
	type staleBinding struct {
		User    model.User
		Binding model.TelegramBinding
	}
	var stale []staleBinding
	for _, binding := range bindings {
		if binding.TelegramUserID == 0 || binding.UserID == "" {
			continue
		}
		user, _ := s.repo.User.FindByID(ctx, binding.UserID)
		if user == nil || UserIsProtectedAccount(ctx, s.repo, user) {
			continue
		}
		member := false
		for _, chatID := range chatIDs {
			if s.telegramUserIsChatMember(ctx, channel, chatID, int(binding.TelegramUserID)) {
				member = true
				break
			}
		}
		if !member {
			stale = append(stale, staleBinding{User: *user, Binding: binding})
		}
	}
	if len(stale) == 0 {
		return telegramCommandReply{Text: "所有已绑定账号都仍在配置的群组/频道中。"}
	}
	if len(args) >= 2 && strings.EqualFold(args[0], "delete") && strings.EqualFold(args[1], "confirm") {
		deleted := 0
		for _, item := range stale {
			_ = s.repo.UserDevice.DeleteByUser(ctx, item.User.ID)
			if err := s.repo.User.Delete(ctx, item.User.ID); err == nil {
				deleted++
			}
		}
		return telegramCommandReply{Text: fmt.Sprintf("已删除不在群组/频道中的普通账号：<b>%d</b> 个。", deleted)}
	}
	names := make([]string, 0, minInt(len(stale), 20))
	for i, item := range stale {
		if i >= 20 {
			break
		}
		names = append(names, fmt.Sprintf("%s(tg:%d)", item.User.Username, item.Binding.TelegramUserID))
	}
	return telegramCommandReply{Text: fmt.Sprintf("不在配置群组/频道中的绑定账号：<b>%d</b> 个。\n%s\n\n删除需确认：<code>/syncgroupm delete confirm</code>", len(stale), telegramInlineCodeList(names))}
}

func (s *TelegramBotService) telegramMembershipChatIDs(channel *model.NotifyChannel) []string {
	cfg := s.telegramChannelConfig(channel)
	seen := map[string]struct{}{}
	var out []string
	for _, key := range []string{"group_chat_id", "channel_chat_id"} {
		value := strings.TrimSpace(cfg[key])
		if value == "" {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		if value := strings.TrimSpace(cfg["chat_id"]); strings.HasPrefix(value, "-") {
			out = append(out, value)
		}
	}
	return out
}

func (s *TelegramBotService) cmdMgoUnsupported(name, replacement string) telegramCommandReply {
	text := fmt.Sprintf("<b>%s</b> 已识别，但当前 Telegram Bot API 无法完整复刻该行为。", name)
	if replacement != "" {
		text += "\n请使用：" + replacement
	}
	return telegramCommandReply{Text: text}
}

func (s *TelegramBotService) findMgoBotUser(ctx context.Context, target string) *model.User {
	target = strings.TrimSpace(strings.TrimPrefix(target, "@"))
	if target == "" {
		return nil
	}
	if user, _ := s.repo.User.FindByUsername(ctx, target); user != nil {
		return user
	}
	if user, _ := s.repo.User.FindByID(ctx, target); user != nil {
		return user
	}
	if tgRaw, ok := strings.CutPrefix(strings.ToLower(target), "tg:"); ok {
		if tgID, err := strconv.ParseInt(tgRaw, 10, 64); err == nil {
			var binding model.TelegramBinding
			if err := s.repo.DB.WithContext(ctx).Where("telegram_user_id = ?", tgID).First(&binding).Error; err == nil {
				user, _ := s.repo.User.FindByID(ctx, binding.UserID)
				return user
			}
		}
	}
	return nil
}

func activeLabel(user *model.User) string {
	if user == nil {
		return "未知"
	}
	if !user.IsActive {
		return "已禁用"
	}
	if user.ExpiredAt != nil && time.Now().After(*user.ExpiredAt) {
		return "已过期"
	}
	return "正常"
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func blankDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func telegramInlineCodeList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "<code>" + strings.Join(items, "</code>、<code>") + "</code>"
}

func parseOnOff(raw string) *bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "on", "true", "1", "yes", "enable", "enabled", "开启", "开":
		v := true
		return &v
	case "off", "false", "0", "no", "disable", "disabled", "关闭", "关":
		v := false
		return &v
	default:
		return nil
	}
}

func humanDurationFromMillis(ms int64) string {
	if ms <= 0 {
		return "0 分钟"
	}
	totalMinutes := ms / 1000 / 60
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours == 0 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", hours, minutes)
}

// Package service — Telegram command registry and dispatch.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type telegramCommandHandler func(args []string) (telegramCommandReply, error)

type telegramCommandDefinition struct {
	Aliases       []string
	AdminOnly     bool
	AdminOnlyText string
	GroupAllowed  bool
	Handle        telegramCommandHandler
}

func (s *TelegramBotService) telegramCommandDefinitions(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage) []telegramCommandDefinition {
	adminOnly := "此命令仅管理员可用。"
	return []telegramCommandDefinition{
		{Aliases: []string{"/start"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			if len(args) == 0 {
				return s.mainMenu(ctx, channel, telegramPrivateMessageForUser(msg)), nil
			}
			return s.cmdStart(ctx, msg, args), nil
		}},
		{Aliases: []string{"/menu"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			return s.mainMenu(ctx, channel, telegramPrivateMessageForUser(msg)), nil
		}},
		{Aliases: []string{"/cancel"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			s.takePending(int64(msg.From.ID))
			return telegramCommandReply{Text: "已取消当前操作。"}, nil
		}},
		{Aliases: []string{"/help"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) {
			return telegramCommandReply{Text: s.cmdHelp(ctx, msg)}, nil
		}},
		{Aliases: []string{"/hideadult", "/hide_adult", "/adult"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdHideAdult(ctx, msg, args), nil }},
		{Aliases: []string{"/account", "/me", "/myinfo"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.replyAccount(ctx, msg), nil }},
		{Aliases: []string{"/count"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdStats(ctx) }},
		{Aliases: []string{"/signin", "/checkin"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.replySignIn(ctx, msg), nil }},
		{Aliases: []string{"/devices"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.replyDevices(ctx, msg), nil }},
		{Aliases: []string{"/kick"}, GroupAllowed: true, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdKick(ctx, msg, args), nil }},
		{Aliases: []string{"/setname", "/rename"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSetName(ctx, msg, args), nil }},
		{Aliases: []string{"/setpass", "/passwd", "/password"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSetPass(ctx, msg, args), nil }},
		{Aliases: []string{"/redeem"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRedeem(ctx, channel, msg, args), nil }},
		{Aliases: []string{"/redeem_register"}, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdRedeemRegister(ctx, channel, msg, args), nil
		}},
		{Aliases: []string{"/redeem_renew"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRedeemRenew(ctx, msg, args), nil }},
		{Aliases: []string{"/register", "/reg", "/signup"}, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRegister(ctx, channel, msg, args), nil }},

		{Aliases: []string{"/registration", "/reg_switch", "/openreg"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdRegistrationToggle(ctx, args), nil }},
		{Aliases: []string{"/capacity"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.replyCapacity(ctx), nil }},
		{Aliases: []string{"/users", "/kk"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.replyUserList(ctx), nil }},
		{Aliases: []string{"/gencode"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdGenCode(ctx, msg, args), nil }},
		{Aliases: []string{"/renew_user"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserRenew(ctx, args), nil }},
		{Aliases: []string{"/delete_user"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserDelete(ctx, args), nil }},
		{Aliases: []string{"/unbind"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUnbind(ctx, args), nil }},
		{Aliases: []string{"/unbind_duplicates"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUnbindDuplicates(ctx), nil }},
		{Aliases: []string{"/unbind_inactive"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUnbindInactive(ctx, args), nil }},
		{Aliases: []string{"/devicepolicy", "/policy"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdDevicePolicy(ctx, args), nil }},
		{Aliases: []string{"/antishare"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdAntiShare(ctx, args), nil }},
		{Aliases: []string{"/cleanup"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanup(ctx, args), nil }},
		{Aliases: []string{"/cleanup_mode"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanupMode(ctx, args), nil }},
		{Aliases: []string{"/cleanup_rule"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanupRule(ctx, args), nil }},
		{Aliases: []string{"/ban"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserBan(ctx, args, false), nil }},
		{Aliases: []string{"/unban"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserBan(ctx, args, true), nil }},
		{Aliases: []string{"/status"}, AdminOnly: true, AdminOnlyText: "此命令仅管理员可用。普通用户只能使用 /start 绑定账号，并通过按钮隐藏成人目录。", Handle: func(args []string) (telegramCommandReply, error) { return s.cmdStatus(ctx) }},
		{Aliases: []string{"/search"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSearch(ctx, args) }},
		{Aliases: []string{"/downloads"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdDownloads(ctx) }},
		{Aliases: []string{"/stats"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdStats(ctx) }},
		{Aliases: []string{"/renew"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdUserRenew(ctx, args), nil }},
		{Aliases: []string{"/ucr"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraCreateUser(ctx, args), nil }},
		{Aliases: []string{"/uinfo"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraUserInfo(ctx, args), nil }},
		{Aliases: []string{"/rmemby", "/urm", "/only_rm_emby"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraDeleteUser(ctx, args), nil }},
		{Aliases: []string{"/only_rm_record"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraOnlyRemoveRecord(ctx, args), nil }},
		{Aliases: []string{"/userip"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraUserIP(ctx, args), nil }},
		{Aliases: []string{"/udeviceid"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraAuditDevices(ctx, "udeviceid", args), nil
		}},
		{Aliases: []string{"/auditip"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraAuditDevices(ctx, "auditip", args), nil
		}},
		{Aliases: []string{"/auditdevice"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraAuditDevices(ctx, "auditdevice", args), nil
		}},
		{Aliases: []string{"/auditclient"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraAuditDevices(ctx, "auditclient", args), nil
		}},
		{Aliases: []string{"/renewall"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraRenewAll(ctx, args), nil }},
		{Aliases: []string{"/callall"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraCallAll(ctx, channel, args), nil }},
		{Aliases: []string{"/syncunbound"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraSyncUnbound(ctx, args), nil }},
		{Aliases: []string{"/syncgroupm"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraSyncGroup(ctx, channel, args), nil
		}},
		{Aliases: []string{"/kick_not_emby"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraUnsupported("群内无号用户清理", "<code>/syncgroupm</code> 可检查已绑定账号是否仍在群内；Telegram Bot API 无法枚举全部群成员，因此不能可靠找出“在群但无号”的用户。"), nil
		}},
		{Aliases: []string{"/scan_embyname"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraScanNames(ctx), nil }},
		{Aliases: []string{"/check_ex"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraCheckExpired(ctx, args), nil }},
		{Aliases: []string{"/deleted", "/low_activity"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdCleanup(ctx, []string{"run"}), nil }},
		{Aliases: []string{"/uranks"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraRanks(ctx, 0, true), nil }},
		{Aliases: []string{"/days_ranks"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraRanks(ctx, 24*time.Hour, false), nil
		}},
		{Aliases: []string{"/week_ranks"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraRanks(ctx, 7*24*time.Hour, false), nil
		}},
		{Aliases: []string{"/embyadmin"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraAdminRole(ctx, args), nil }},
		{Aliases: []string{"/unbanall"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraBanAll(ctx, true, args), nil }},
		{Aliases: []string{"/banall"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraBanAll(ctx, false, args), nil }},
		{Aliases: []string{"/embylibs_unblockall", "/extraembylibs_unblockall"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraMediaAccessAll(ctx, true), nil
		}},
		{Aliases: []string{"/embylibs_blockall", "/extraembylibs_blockall"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraMediaAccessAll(ctx, false), nil
		}},
		{Aliases: []string{"/proadmin"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraBotAdmin(ctx, channel, args, true), nil
		}},
		{Aliases: []string{"/revadmin"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraBotAdmin(ctx, channel, args, false), nil
		}},
		{Aliases: []string{"/backup_db"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraBackupDB(ctx), nil }},
		{Aliases: []string{"/restore_from_db"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) { return s.cmdSakuraRestoreDB(ctx, args), nil }},
		{Aliases: []string{"/prouser"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraProtectedUser(ctx, args, true), nil
		}},
		{Aliases: []string{"/revuser"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraProtectedUser(ctx, args, false), nil
		}},
		{Aliases: []string{"/restart", "/update_bot", "/paolu", "/bindall_id", "/sync_favorites", "/coins", "/score", "/coinsall", "/coinsclear", "/red", "/srank", "/white_channel", "/rev_white_channel", "/unban_channel", "/config"}, AdminOnly: true, AdminOnlyText: adminOnly, Handle: func(args []string) (telegramCommandReply, error) {
			return s.cmdSakuraUnsupported("Sakura 专属命令", "这些命令涉及外部 Bot 自身运维/积分红包/皮套人管理，已在 MediaStationGo 中由权限、通知渠道、设备策略替代。"), nil
		}},
	}
}

func (s *TelegramBotService) telegramCommandRegistry(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage) map[string]telegramCommandDefinition {
	defs := s.telegramCommandDefinitions(ctx, channel, msg)
	registry := make(map[string]telegramCommandDefinition, len(defs)*2)
	for _, def := range defs {
		for _, alias := range def.Aliases {
			registry[alias] = def
		}
	}
	return registry
}

// executeCommand parses and dispatches Telegram commands through a registry so
// adding a command does not grow a monolithic switch.
func (s *TelegramBotService) executeCommand(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, text string) (telegramCommandReply, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return telegramCommandReply{}, nil
	}

	cmd := telegramCommandName(parts[0])
	args := parts[1:]
	if msg.Chat.Type != "" && msg.Chat.Type != "private" && !s.telegramChatAllowed(channel, msg.Chat.ID) {
		return telegramCommandReply{Text: "此群组/频道未绑定到 Bot 管理入口，请在通知渠道里填写「绑定群组 ID」或「绑定频道 ID」。"}, nil
	}

	def, ok := s.telegramCommandRegistry(ctx, channel, msg)[cmd]
	if !ok {
		return telegramCommandReply{Text: fmt.Sprintf("未知命令: %s\n\n输入 /help 查看可用命令列表。", cmd)}, nil
	}
	if telegramIsGroupChat(msg.Chat.Type) && !def.GroupAllowed {
		if def.AdminOnly && s.telegramUserIsAdmin(ctx, channel, msg.From.ID) {
			return telegramCommandReply{Text: telegramGroupPrivateAdminHint()}, nil
		}
		return telegramCommandReply{}, nil
	}
	if def.AdminOnly && !s.telegramUserIsAdmin(ctx, channel, msg.From.ID) {
		return telegramCommandReply{Text: def.AdminOnlyText}, nil
	}
	return def.Handle(args)
}

func telegramSupportedCommand(cmd string) bool {
	cmd = telegramCommandName(cmd)
	if cmd == "" {
		return false
	}
	_, ok := telegramSupportedCommandSet[cmd]
	return ok
}

var telegramSupportedCommandSet = map[string]struct{}{
	"/start": {}, "/menu": {}, "/cancel": {}, "/help": {}, "/hideadult": {}, "/hide_adult": {}, "/adult": {},
	"/account": {}, "/me": {}, "/myinfo": {}, "/count": {}, "/signin": {}, "/checkin": {}, "/devices": {}, "/kick": {}, "/setname": {}, "/rename": {}, "/setpass": {}, "/passwd": {}, "/password": {},
	"/redeem": {}, "/redeem_register": {}, "/redeem_renew": {},
	"/register": {}, "/reg": {}, "/signup": {}, "/registration": {}, "/reg_switch": {}, "/openreg": {},
	"/capacity": {}, "/users": {}, "/kk": {}, "/gencode": {}, "/renew_user": {}, "/delete_user": {}, "/unbind": {}, "/unbind_duplicates": {}, "/unbind_inactive": {},
	"/devicepolicy": {}, "/policy": {}, "/antishare": {}, "/cleanup": {}, "/cleanup_mode": {}, "/cleanup_rule": {},
	"/ban": {}, "/unban": {}, "/status": {}, "/search": {}, "/downloads": {}, "/stats": {},
	"/renew": {}, "/ucr": {}, "/uinfo": {}, "/rmemby": {}, "/urm": {}, "/only_rm_emby": {}, "/only_rm_record": {},
	"/userip": {}, "/udeviceid": {}, "/auditip": {}, "/auditdevice": {}, "/auditclient": {},
	"/renewall": {}, "/callall": {}, "/syncunbound": {}, "/syncgroupm": {}, "/kick_not_emby": {}, "/scan_embyname": {},
	"/check_ex": {}, "/deleted": {}, "/low_activity": {}, "/uranks": {}, "/days_ranks": {}, "/week_ranks": {},
	"/embyadmin": {}, "/unbanall": {}, "/banall": {}, "/embylibs_unblockall": {}, "/embylibs_blockall": {},
	"/extraembylibs_unblockall": {}, "/extraembylibs_blockall": {}, "/proadmin": {}, "/revadmin": {},
	"/backup_db": {}, "/restore_from_db": {}, "/restart": {}, "/update_bot": {}, "/paolu": {}, "/bindall_id": {}, "/sync_favorites": {}, "/config": {},
	"/coins": {}, "/score": {}, "/coinsall": {}, "/coinsclear": {}, "/red": {}, "/srank": {}, "/prouser": {}, "/revuser": {},
	"/white_channel": {}, "/rev_white_channel": {}, "/unban_channel": {},
}

type telegramBotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func telegramBotCommandMenu() []telegramBotCommand {
	return telegramPrivateBotCommandMenu()
}

func telegramPrivateBotCommandMenu() []telegramBotCommand {
	return []telegramBotCommand{
		{Command: "start", Description: "绑定账号或打开主菜单"},
		{Command: "menu", Description: "打开功能菜单"},
		{Command: "help", Description: "查看命令帮助"},
		{Command: "account", Description: "查看账号状态"},
		{Command: "signin", Description: "签到"},
		{Command: "devices", Description: "查看登录设备"},
		{Command: "kick", Description: "踢下线设备"},
		{Command: "hideadult", Description: "隐藏/显示成人媒体库"},
		{Command: "redeem", Description: "兑换注册码或续期码"},
		{Command: "register", Description: "注册新账号"},
	}
}

func telegramGroupBotCommandMenu() []telegramBotCommand {
	return []telegramBotCommand{
		{Command: "start", Description: "打开群组自助菜单"},
		{Command: "menu", Description: "打开群组自助菜单"},
		{Command: "help", Description: "查看群组可用命令"},
		{Command: "account", Description: "查看账号状态"},
		{Command: "signin", Description: "签到"},
		{Command: "devices", Description: "查看登录设备"},
		{Command: "kick", Description: "踢下线设备"},
		{Command: "hideadult", Description: "隐藏/显示成人媒体库"},
	}
}

func telegramAdminBotCommandMenu() []telegramBotCommand {
	commands := append([]telegramBotCommand{}, telegramPrivateBotCommandMenu()...)
	commands = append(commands,
		telegramBotCommand{Command: "status", Description: "系统运行状态(管理员)"},
		telegramBotCommand{Command: "search", Description: "搜索媒体库(管理员)"},
		telegramBotCommand{Command: "downloads", Description: "下载列表(管理员)"},
		telegramBotCommand{Command: "stats", Description: "媒体库统计(管理员)"},
		telegramBotCommand{Command: "users", Description: "用户管理(管理员)"},
		telegramBotCommand{Command: "cleanup", Description: "保号规则开关/巡检(管理员)"},
		telegramBotCommand{Command: "cleanup_rule", Description: "Sakura保号规则管理(管理员)"},
	)
	return commands
}

func registerTelegramBotCommands(ctx context.Context, cfg map[string]string) error {
	if strings.TrimSpace(cfg["bot_token"]) == "" {
		return nil
	}
	normalizeTelegramConfig(cfg)
	if err := telegramSetBotCommands(ctx, cfg, telegramPrivateBotCommandMenu(), nil); err != nil {
		return err
	}
	if err := telegramSetBotCommands(ctx, cfg, telegramPrivateBotCommandMenu(), map[string]interface{}{"type": "all_private_chats"}); err != nil {
		return err
	}
	if err := telegramSetBotCommands(ctx, cfg, telegramGroupBotCommandMenu(), map[string]interface{}{"type": "all_group_chats"}); err != nil {
		return err
	}

	adminCommands := telegramAdminBotCommandMenu()
	for _, adminID := range telegramConfiguredUserIDs(cfg["admin_user_ids"]) {
		_ = telegramSetBotCommands(ctx, cfg, adminCommands, map[string]interface{}{"type": "chat", "chat_id": adminID})
	}
	return nil
}

func telegramSetBotCommands(ctx context.Context, cfg map[string]string, commands []telegramBotCommand, scope map[string]interface{}) error {
	payload := map[string]interface{}{"commands": commands}
	if scope != nil {
		payload["scope"] = scope
	}
	return telegramPostJSON(ctx, cfg, "setMyCommands", payload, 15*time.Second)
}

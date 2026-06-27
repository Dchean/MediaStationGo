package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const settingDownloadClientsManaged = "download_clients.managed"

// ReloadConfig rebuilds the qBittorrent client from the configured
// download clients (preferred) or the legacy Setting table (fallback).
//
// 配置来源优先级：
//
//  1. download_clients 表中 type=qbittorrent 且 is_default=true 且 enabled=true
//     的行（侧边栏「下载器」页面写入的数据）。
//  2. system Setting 表中的 qbittorrent.url / username / password
//     （旧版「系统设置」表单写入的数据；保留作向后兼容）。
//
// 这避免了两套配置各跑各的：之前操作员明明已经在「下载器」页面填好
// 默认 qb，但实际下载链路读的还是 Setting 表，导致一直连不上。
func (d *DownloadService) ReloadConfig(ctx context.Context) error {
	cfg := QBitConfig{}
	hasConfiguredClients := false
	managedByDownloadClients := false

	// Path 1: download_clients 表
	if d.repo.DownloadClient != nil {
		hasConfiguredClients, _ = d.repo.DownloadClient.HasAnyIncludingDeleted(ctx)
		if c, err := d.repo.DownloadClient.FindDefault(ctx); err == nil && c != nil && c.Type == "qbittorrent" {
			cfg.BaseURL = strings.TrimRight(c.Host, "/")
			cfg.Username = c.Username
			cfg.Password = c.Password
		} else if c, err := d.soleEnabledQBitClient(ctx); err == nil && c != nil {
			cfg.BaseURL = strings.TrimRight(c.Host, "/")
			cfg.Username = c.Username
			cfg.Password = c.Password
		}
	}
	if d.repo.Setting != nil {
		managedRaw, _ := d.repo.Setting.Get(ctx, settingDownloadClientsManaged)
		managedByDownloadClients = strings.EqualFold(strings.TrimSpace(managedRaw), "true")
	}

	// Path 2: legacy Setting 表。
	// 仅在旧部署“从未使用过 download_clients 表”时回退。只要操作员曾经
	// 配置过下载器，删除/禁用全部下载器就表示应停止投递，不能再偷偷用
	// qbittorrent.* 旧设置继续往下载器添加任务。
	if cfg.BaseURL == "" && !hasConfiguredClients && !managedByDownloadClients {
		get := func(k string) string {
			v, _ := d.repo.Setting.Get(ctx, k)
			return v
		}
		cfg.BaseURL = get("qbittorrent.url")
		cfg.Username = get("qbittorrent.username")
		cfg.Password = get("qbittorrent.password")
	}

	d.qb.Configure(cfg)
	return nil
}

func (d *DownloadService) soleEnabledQBitClient(ctx context.Context) (*model.DownloadClient, error) {
	if d == nil || d.repo == nil || d.repo.DownloadClient == nil {
		return nil, nil
	}
	rows, err := d.repo.DownloadClient.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	var selected *model.DownloadClient
	for i := range rows {
		if rows[i].Type != "qbittorrent" {
			continue
		}
		if selected != nil {
			return nil, nil
		}
		row := rows[i]
		selected = &row
	}
	return selected, nil
}

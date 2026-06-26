package service

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// DownloadTaskMeta carries public display metadata for a download. It is
// deliberately separate from the private torrent URL so API responses never
// need to expose tracker tokens.
type DownloadTaskMeta struct {
	SubscriptionID       string
	Title                string
	PosterURL            string
	BackdropURL          string
	Overview             string
	MediaType            string
	MediaCategory        string
	SourceCategory       string
	OriginalName         string
	OriginalLanguage     string
	Year                 int
	Rating               float32
	Genres               string
	AllowExistingLibrary bool
}

type downloadAddRequest struct {
	title        string
	savePath     string
	qbitCategory string
	meta         DownloadTaskMeta
}

// AddDownload accepts a magnet URL / HTTP URL and persists a tracking row.
func (d *DownloadService) AddDownload(ctx context.Context, userID, urlStr, savePath string) (*model.DownloadTask, error) {
	return d.AddDownloadWithMeta(ctx, userID, urlStr, savePath, DownloadTaskMeta{})
}

func (d *DownloadService) AddDownloadWithMeta(ctx context.Context, userID, urlStr, savePath string, meta DownloadTaskMeta) (*model.DownloadTask, error) {
	req, err := d.prepareDownloadAdd(ctx, urlStr, savePath, meta)
	if err != nil {
		return nil, err
	}
	if !req.meta.AllowExistingLibrary && d.localMediaAlreadyExists(ctx, req.title) {
		return nil, ErrMediaAlreadyInLibrary
	}
	if existing, ok := d.findExistingDownloadTask(ctx, req); ok {
		return existing, ErrDownloadAlreadyExists
	}
	_ = d.ReloadConfig(ctx)
	if !d.qb.IsConfigured() {
		return nil, errors.New("no default downloader configured")
	}
	if d.torrentExistsByIdentity(ctx, req) {
		task, err := d.createTask(ctx, userID, urlStr, req.savePath, req.meta)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.meta.SubscriptionID) != "" {
			return task, nil
		}
		return task, ErrDownloadAlreadyExists
	}
	if err := d.addPreparedDownloadToClient(ctx, urlStr, &req); err != nil {
		if errors.Is(err, ErrDownloadAlreadyExists) && strings.TrimSpace(req.meta.SubscriptionID) != "" {
			return d.createTask(ctx, userID, urlStr, req.savePath, req.meta)
		}
		return nil, err
	}
	return d.createTask(ctx, userID, urlStr, req.savePath, req.meta)
}

func (d *DownloadService) prepareDownloadAdd(ctx context.Context, urlStr, savePath string, meta DownloadTaskMeta) (downloadAddRequest, error) {
	if urlStr == "" {
		return downloadAddRequest{}, errors.New("empty url")
	}
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = publicDownloadTitle(urlStr)
		meta.Title = title
	}
	autoClassify := downloadSmartClassifyEnabled(ctx, d.repo, d.organizer)
	savePath, resolvedCategory := d.resolveDownloadSavePath(ctx, savePath, meta, autoClassify)
	if !autoClassify {
		meta.MediaCategory = ""
	} else if strings.TrimSpace(meta.MediaCategory) == "" {
		meta.MediaCategory = resolvedCategory
	}
	return downloadAddRequest{
		title:        title,
		savePath:     savePath,
		qbitCategory: strings.TrimSpace(meta.MediaCategory),
		meta:         meta,
	}, nil
}

func (d *DownloadService) addPreparedDownloadToClient(ctx context.Context, urlStr string, req *downloadAddRequest) error {
	var siteFetchErr error
	if d.site != nil {
		if data, name, err := d.site.FetchTorrentFile(ctx, urlStr); err == nil {
			if err := d.qb.AddTorrentFileWithCategory(ctx, data, name, req.savePath, req.qbitCategory); err != nil {
				return err
			}
			if strings.TrimSpace(req.meta.Title) == "" {
				req.meta.Title = strings.TrimSuffix(name, path.Ext(name))
			}
			return nil
		} else {
			siteFetchErr = err
		}
	}
	if err := d.qb.AddTorrentWithCategory(ctx, urlStr, req.savePath, req.qbitCategory); err != nil {
		if siteFetchErr != nil && !strings.Contains(siteFetchErr.Error(), "no matching PT site") {
			return errors.Join(err, siteFetchErr)
		}
		return err
	}
	return nil
}

func (d *DownloadService) resolveDownloadSavePath(ctx context.Context, explicitSavePath string, meta DownloadTaskMeta, autoClassify bool) (string, string) {
	if strings.TrimSpace(explicitSavePath) != "" {
		if !autoClassify {
			return explicitSavePath, ""
		}
		return explicitSavePath, strings.TrimSpace(meta.MediaCategory)
	}
	base := downloadDefaultSaveRoot(ctx, d.repo)
	if strings.TrimSpace(base) == "" {
		return "", strings.TrimSpace(meta.MediaCategory)
	}
	mediaType := normalizeMediaType(meta.MediaType, meta.Title, meta.SourceCategory)
	category := strings.TrimSpace(meta.MediaCategory)
	if category == "" {
		category = classifyMediaCategory(mediaClassifyInput{
			MediaType: mediaType,
			Title:     meta.Title,
			Category:  meta.SourceCategory,
		}, downloadCategoryMap(d.organizer))
	}
	if !autoClassify || category == "" {
		return base, ""
	}
	return downloadSavePathCategoryRoot(base, sanitizeFilename(category)), category
}

func (d *DownloadService) localMediaAlreadyExists(ctx context.Context, title string) bool {
	rows, ok := d.localMediaAvailabilityRows(ctx, title)
	if !ok {
		return false
	}
	return localMediaRowsMatchDownloadTitle(title, rows)
}

func (d *DownloadService) localMediaAvailabilityRows(ctx context.Context, title string) ([]model.Media, bool) {
	if d == nil || d.repo == nil || d.repo.DB == nil {
		return nil, false
	}
	if !d.repo.DB.Migrator().HasTable(&model.Media{}) {
		return nil, false
	}
	queries := localAvailabilityTitleCandidates(title)
	if len(queries) == 0 {
		return nil, false
	}
	var rows []model.Media
	db := d.repo.DB.WithContext(ctx).Model(&model.Media{})
	for i, query := range queries {
		like := "%" + query + "%"
		clause := "title LIKE ? OR original_name LIKE ? OR path LIKE ?"
		if i == 0 {
			db = db.Where(clause, like, like, like)
		} else {
			db = db.Or(clause, like, like, like)
		}
	}
	if err := db.
		Order("season_num asc, episode_num asc, created_at desc").
		Limit(200).
		Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func localMediaRowsMatchDownloadTitle(title string, rows []model.Media) bool {
	wanted := episodeRefsFromTitle(title)
	if len(wanted) == 0 {
		return true
	}
	existing := map[string]struct{}{}
	hasSeriesPack := false
	for _, row := range rows {
		rowSeason, rowEpisode := localMediaRowSeasonEpisode(row)
		if rowEpisode > 0 {
			existing[episodeKey(rowSeason, rowEpisode)] = struct{}{}
			continue
		}
		if rowEpisode <= 0 && isSeriesPackTitle(row.Title+" "+row.OriginalName+" "+row.Path) {
			hasSeriesPack = true
		}
	}
	if hasSeriesPack {
		return len(wanted) == 0
	}
	for _, ref := range wanted {
		if _, ok := existing[episodeKey(ref.Season, ref.Episode)]; !ok {
			return false
		}
	}
	return true
}

func localMediaRowSeasonEpisode(row model.Media) (int, int) {
	rowSeason := row.SeasonNum
	rowEpisode := row.EpisodeNum
	if rowSeason <= 0 || rowEpisode <= 0 {
		parsedSeason, parsedEpisode := ParseEpisode(row.Path)
		if rowSeason <= 0 {
			rowSeason = parsedSeason
		}
		if rowEpisode <= 0 {
			rowEpisode = parsedEpisode
		}
	}
	if rowSeason <= 0 {
		rowSeason = 1
	}
	return rowSeason, rowEpisode
}

func (d *DownloadService) findExistingDownloadTask(ctx context.Context, req downloadAddRequest) (*model.DownloadTask, bool) {
	key := downloadTaskIdentityKey(req.title)
	if key == "" || d == nil || d.repo == nil || d.repo.Download == nil {
		return nil, false
	}
	rows, err := d.repo.Download.List(ctx)
	if err != nil {
		return nil, false
	}
	subscriptionID := strings.TrimSpace(req.meta.SubscriptionID)
	for i := range rows {
		if subscriptionID != "" {
			if !downloadTaskBlocksReadd(rows[i].Status) {
				continue
			}
			if !downloadTaskInSubscriptionScope(rows[i], req) {
				continue
			}
			if !d.subscriptionDownloadTaskStillLive(ctx, rows[i]) {
				continue
			}
		} else if !downloadTaskBlocksDuplicate(rows[i].Status) {
			continue
		}
		current := downloadTaskIdentityKey(rows[i].Title)
		if downloadTaskCoversAddRequest(rows[i].Title, req) || current == key {
			return &rows[i], true
		}
	}
	return nil, false
}

func (d *DownloadService) subscriptionDownloadTaskStillLive(ctx context.Context, row model.DownloadTask) bool {
	live, ok := d.liveTorrentSnapshot(30 * time.Second)
	if !ok && d != nil && d.qb != nil && d.qb.IsConfigured() {
		var err error
		live, err = d.qb.List(ctx, "")
		if err != nil {
			return true
		}
		ok = true
	}
	if !ok {
		return true
	}
	for _, torrent := range live {
		if downloadTaskMatchesLiveTorrent(row, torrent) {
			return true
		}
	}
	return false
}

func downloadTaskMatchesLiveTorrent(row model.DownloadTask, torrent QBitTorrent) bool {
	torrentName := strings.TrimSpace(torrent.Name)
	if torrentName == "" {
		return false
	}
	req := downloadAddRequest{
		title:    row.Title,
		savePath: row.SavePath,
		meta: DownloadTaskMeta{
			SubscriptionID: row.SubscriptionID,
		},
	}
	if downloadTaskCoversAddRequest(torrentName, req) {
		return true
	}
	rowKey := downloadTaskIdentityKey(row.Title)
	torrentKey := downloadTaskIdentityKey(torrentName)
	if rowKey != "" && torrentKey != "" {
		return rowKey == torrentKey
	}
	if len(episodeRefsFromTitle(row.Title)) > 0 || len(episodeRefsFromTitle(torrentName)) > 0 {
		return false
	}
	rowTorrentKey := normalizeTorrentName(row.Title)
	liveTorrentKey := normalizeTorrentName(torrentName)
	return rowTorrentKey != "" && rowTorrentKey == liveTorrentKey
}

func downloadTaskCoversAddRequest(existing string, req downloadAddRequest) bool {
	if subscriptionRequestHasExplicitEpisodes(req) {
		return downloadExplicitEpisodesCoverRequest(existing, req.title)
	}
	return downloadTitleCoversRequest(existing, req.title)
}

func subscriptionRequestHasExplicitEpisodes(req downloadAddRequest) bool {
	return strings.TrimSpace(req.meta.SubscriptionID) != "" && len(episodeRefsFromTitle(req.title)) > 0
}

func downloadExplicitEpisodesCoverRequest(existing, requested string) bool {
	current := parseDownloadMediaIdentity(existing)
	want := parseDownloadMediaIdentity(requested)
	if current.TitleKey == "" || want.TitleKey == "" {
		return false
	}
	if current.TitleKey != want.TitleKey {
		return false
	}
	if current.Year > 0 && want.Year > 0 && current.Year != want.Year {
		return false
	}
	if len(current.Episodes) == 0 || len(want.Episodes) == 0 {
		return false
	}
	currentEpisodes := map[string]struct{}{}
	for _, ref := range current.Episodes {
		currentEpisodes[episodeKey(ref.Season, ref.Episode)] = struct{}{}
	}
	for _, ref := range want.Episodes {
		if _, ok := currentEpisodes[episodeKey(ref.Season, ref.Episode)]; !ok {
			return false
		}
	}
	return true
}

func downloadTaskInSubscriptionScope(row model.DownloadTask, req downloadAddRequest) bool {
	subscriptionID := strings.TrimSpace(req.meta.SubscriptionID)
	if subscriptionID == "" {
		return true
	}
	rowSubscriptionID := strings.TrimSpace(row.SubscriptionID)
	if rowSubscriptionID != "" {
		return rowSubscriptionID == subscriptionID
	}
	rowSavePath := strings.TrimSpace(row.SavePath)
	requestSavePath := strings.TrimSpace(req.savePath)
	if rowSavePath == "" || requestSavePath == "" {
		return false
	}
	return sameOrChildPath(rowSavePath, requestSavePath) || sameOrChildPath(requestSavePath, rowSavePath)
}

func (d *DownloadService) torrentExistsByIdentity(ctx context.Context, req downloadAddRequest) bool {
	query := downloadTaskIdentityKey(req.title)
	if query == "" {
		return false
	}
	live, err := d.qb.List(ctx, "")
	if err != nil {
		return false
	}
	for _, torrent := range live {
		if !torrentInDownloadRequestScope(torrent, req) {
			continue
		}
		if downloadTaskCoversAddRequest(torrent.Name, req) {
			return true
		}
		current := downloadTaskIdentityKey(torrent.Name)
		if current == "" {
			continue
		}
		if current == query {
			return true
		}
	}
	return false
}

func torrentInDownloadRequestScope(torrent QBitTorrent, req downloadAddRequest) bool {
	if strings.TrimSpace(req.meta.SubscriptionID) == "" {
		return true
	}
	requestSavePath := strings.TrimSpace(req.savePath)
	torrentSavePath := strings.TrimSpace(torrent.SavePath)
	if requestSavePath == "" || torrentSavePath == "" {
		return false
	}
	return sameOrChildPath(torrentSavePath, requestSavePath) || sameOrChildPath(requestSavePath, torrentSavePath)
}

func (d *DownloadService) createTask(ctx context.Context, userID, urlStr, savePath string, meta DownloadTaskMeta) (*model.DownloadTask, error) {
	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = publicDownloadTitle(urlStr)
	}
	t := &model.DownloadTask{
		UserID:               userID,
		SubscriptionID:       strings.TrimSpace(meta.SubscriptionID),
		Source:               "qbittorrent",
		URL:                  urlStr,
		Title:                title,
		PosterURL:            meta.PosterURL,
		BackdropURL:          meta.BackdropURL,
		Overview:             meta.Overview,
		SavePath:             savePath,
		MediaType:            meta.MediaType,
		MediaCategory:        meta.MediaCategory,
		OriginalName:         meta.OriginalName,
		OriginalLanguage:     meta.OriginalLanguage,
		Year:                 meta.Year,
		Rating:               meta.Rating,
		Genres:               meta.Genres,
		Status:               "queued",
		AllowExistingLibrary: meta.AllowExistingLibrary,
	}
	if err := d.repo.Download.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

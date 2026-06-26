package database

import (
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// AutoMigrate creates tables for every model registered in the model package.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		return err
	}
	if err := ensurePostgresColumnCompatibility(db); err != nil {
		return err
	}
	if err := enforceTelegramBindingOneToOne(db); err != nil {
		return err
	}
	if err := ensurePerformanceIndexes(db); err != nil {
		return err
	}
	if err := ensureLibraryRootsCompatibility(db); err != nil {
		return err
	}
	if isSQLite(db) {
		return ensureMediaSearchIndex(db)
	}
	return nil
}

func ensurePostgresColumnCompatibility(db *gorm.DB) error {
	if !isPostgres(db) {
		return nil
	}
	statements := []string{
		`ALTER TABLE media ALTER COLUMN container TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN genres TYPE text`,
		`ALTER TABLE media ALTER COLUMN series_id TYPE varchar(128)`,
		`ALTER TABLE media ALTER COLUMN duplicate_of TYPE varchar(128)`,
		`ALTER TABLE playback_histories ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE favorites ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE playlist_items ALTER COLUMN media_id TYPE varchar(128)`,
		`ALTER TABLE strm_records ALTER COLUMN media_id TYPE varchar(128)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensurePerformanceIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_media_library_created_active ON media(library_id, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_episode_active ON media(library_id, season_num, episode_num, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_library_root_active ON media(library_id, library_root_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_media_series_active ON media(series_id, season_num, episode_num) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_user_media_active ON favorites(user_id, media_id) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_user_media_active ON playback_histories(user_id, media_id, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_playback_histories_resume_active ON playback_histories(user_id, completed, watched_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_play_profiles_user_created_active ON play_profiles(user_id, created_at DESC) WHERE deleted_at IS NULL`,
	}
	if isSQLite(db) {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_media_title_active ON media(title COLLATE NOCASE) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_media_original_name_active ON media(original_name COLLATE NOCASE) WHERE deleted_at IS NULL`,
		)
	} else {
		statements = append(statements,
			`CREATE INDEX IF NOT EXISTS idx_media_title_active ON media(title) WHERE deleted_at IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_media_original_name_active ON media(original_name) WHERE deleted_at IS NULL`,
		)
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureLibraryRootsCompatibility(db *gorm.DB) error {
	var libraries []model.Library
	if err := db.Find(&libraries).Error; err != nil {
		return err
	}
	for _, lib := range libraries {
		if strings.TrimSpace(lib.Path) == "" {
			continue
		}
		var count int64
		if err := db.Model(&model.LibraryRoot{}).Where("library_id = ?", lib.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		root := model.LibraryRoot{
			LibraryID: lib.ID,
			Name:      firstLibraryRootLabel(lib.Path),
			Path:      lib.Path,
			Enabled:   lib.Enabled,
			SortOrder: 0,
		}
		if err := db.Create(&root).Error; err != nil {
			return err
		}
		if err := backfillLibraryRootMedia(db, lib, root); err != nil {
			return err
		}
	}
	return nil
}

func backfillLibraryRootMedia(db *gorm.DB, lib model.Library, root model.LibraryRoot) error {
	var rows []model.Media
	if err := db.Unscoped().
		Model(&model.Media{}).
		Select("id", "path").
		Where("library_id = ? AND (library_root_id = '' OR library_root_id IS NULL)", lib.ID).
		Find(&rows).Error; err != nil {
		return err
	}
	rootPath := strings.TrimSpace(root.Path)
	for _, row := range rows {
		rel, ok := relativePathWithinRoot(row.Path, rootPath)
		if !ok {
			continue
		}
		if err := db.Unscoped().Model(&model.Media{}).Where("id = ?", row.ID).Updates(map[string]any{
			"library_root_id": root.ID,
			"relative_path":   rel,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func relativePathWithinRoot(pathValue, root string) (string, bool) {
	pathValue = strings.TrimSpace(pathValue)
	root = strings.TrimSpace(root)
	if pathValue == "" || root == "" {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(root), "cloud://") || strings.HasPrefix(strings.ToLower(pathValue), "cloud://") {
		prefix := strings.TrimRight(root, "/") + "/"
		if strings.EqualFold(pathValue, root) {
			return "", true
		}
		if strings.HasPrefix(strings.ToLower(pathValue), strings.ToLower(prefix)) {
			return strings.TrimPrefix(pathValue, prefix), true
		}
		return "", false
	}
	cleanPath := filepath.Clean(pathValue)
	cleanRoot := filepath.Clean(root)
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func firstLibraryRootLabel(pathValue string) string {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(pathValue), "cloud://") {
		parts := strings.Split(strings.Trim(pathValue, "/"), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	base := filepath.Base(filepath.Clean(pathValue))
	if base == "." || base == string(filepath.Separator) {
		return pathValue
	}
	return base
}

// mediaSearchIndexSchemaVersion identifies the physical FTS index layout.
// v2 aligns FTS rowids with media rowids and keeps the index current with
// triggers.
const mediaSearchIndexSchemaVersion = 2

func ensureMediaSearchIndex(db *gorm.DB) error {
	if err := ensureMediaSearchMetaTable(db); err != nil {
		return nil
	}
	version := currentMediaSearchIndexVersion(db)
	if version != mediaSearchIndexSchemaVersion {
		resetMediaSearchIndex(db)
	}
	if !createMediaSearchFTSTable(db) {
		return nil
	}
	if err := createMediaSearchTriggers(db); err != nil {
		return err
	}
	if version != mediaSearchIndexSchemaVersion {
		return markMediaSearchIndexVersion(db)
	}
	return nil
}

func ensureMediaSearchMetaTable(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS media_search_meta (id INTEGER PRIMARY KEY CHECK (id = 1), version INTEGER NOT NULL)`).Error
}

func currentMediaSearchIndexVersion(db *gorm.DB) int {
	var version int
	_ = db.Raw(`SELECT version FROM media_search_meta WHERE id = 1`).Scan(&version).Error
	return version
}

func resetMediaSearchIndex(db *gorm.DB) {
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS media_search_fts_ai`,
		`DROP TRIGGER IF EXISTS media_search_fts_au`,
		`DROP TRIGGER IF EXISTS media_search_fts_ad`,
		`DROP TABLE IF EXISTS media_search_fts`,
	} {
		_ = db.Exec(stmt).Error
	}
}

func createMediaSearchFTSTable(db *gorm.DB) bool {
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(media_id UNINDEXED, title, original_name, path, genres, tokenize='trigram')`).Error; err == nil {
		return true
	}
	if err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS media_search_fts USING fts5(media_id UNINDEXED, title, original_name, path, genres, tokenize='unicode61')`).Error; err == nil {
		return true
	}
	// FTS is an acceleration path. Some embedded SQLite builds may omit FTS5;
	// keep startup working and let repository queries fall back to LIKE search.
	return false
}

func createMediaSearchTriggers(db *gorm.DB) error {
	for _, stmt := range mediaSearchTriggerStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

var mediaSearchTriggerStatements = []string{
	`CREATE TRIGGER IF NOT EXISTS media_search_fts_ai AFTER INSERT ON media WHEN new.deleted_at IS NULL BEGIN
		DELETE FROM media_search_fts WHERE rowid = new.rowid;
		INSERT INTO media_search_fts(rowid, media_id, title, original_name, path, genres)
		VALUES (new.rowid, new.id, COALESCE(new.title, ''), COALESCE(new.original_name, ''), COALESCE(new.path, ''), COALESCE(new.genres, ''));
	END`,
	`CREATE TRIGGER IF NOT EXISTS media_search_fts_au AFTER UPDATE OF title, original_name, path, genres, deleted_at ON media BEGIN
		DELETE FROM media_search_fts WHERE rowid = old.rowid;
		INSERT INTO media_search_fts(rowid, media_id, title, original_name, path, genres)
		SELECT new.rowid, new.id, COALESCE(new.title, ''), COALESCE(new.original_name, ''), COALESCE(new.path, ''), COALESCE(new.genres, '')
		WHERE new.deleted_at IS NULL;
	END`,
	`CREATE TRIGGER IF NOT EXISTS media_search_fts_ad AFTER DELETE ON media BEGIN
		DELETE FROM media_search_fts WHERE rowid = old.rowid;
	END`,
}

func markMediaSearchIndexVersion(db *gorm.DB) error {
	return db.Exec(`INSERT INTO media_search_meta(id, version) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET version = excluded.version`, mediaSearchIndexSchemaVersion).Error
}

func enforceTelegramBindingOneToOne(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.TelegramBinding{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
DELETE FROM telegram_bindings
WHERE deleted_at IS NULL
  AND user_id IN (
    SELECT user_id
    FROM telegram_bindings
    WHERE deleted_at IS NULL
    GROUP BY user_id
    HAVING COUNT(*) > 1
  )
  AND id NOT IN (
    SELECT id
    FROM (
      SELECT id,
             ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY updated_at DESC, created_at DESC, id DESC) AS rn
      FROM telegram_bindings
      WHERE deleted_at IS NULL
    ) AS ranked_bindings
    WHERE rn = 1
  )
`).Error; err != nil {
			return err
		}
		return tx.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_bindings_user_id_active
ON telegram_bindings(user_id)
WHERE deleted_at IS NULL
`).Error
	})
}

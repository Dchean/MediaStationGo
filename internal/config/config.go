// Package config 加载分层配置：默认值、配置文件和环境变量。
//
// 优先级（低 -> 高）:
//  1. 内置默认值
//  2. 工作目录中的 config.yaml（嵌套格式）
//  3. config/*.yaml 分片文件（按模块）
//  4. 以 MEDIASTATION_ 为前缀的环境变量
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// EnvPrefix 是所有环境变量驱动的覆盖使用的前缀。
const EnvPrefix = "MEDIASTATION"

// Config 是根配置聚合。
type Config struct {
	App          AppConfig          `mapstructure:"app"`
	Database     DatabaseConfig     `mapstructure:"database"`
	Secrets      SecretsConfig      `mapstructure:"secrets"`
	Logging      LoggingConfig      `mapstructure:"logging"`
	Cache        CacheConfig        `mapstructure:"cache"`
	Media        MediaConfig        `mapstructure:"media"`
	Transcoder   TranscoderConfig   `mapstructure:"transcoder"`
	AI           AIConfig           `mapstructure:"ai"`
	FlareSolverr FlareSolverrConfig `mapstructure:"flaresolverr"`
	ApiConfig    ApiConfigConfig    `mapstructure:"api_config"`
	Organizer    OrganizerConfig    `mapstructure:"organizer"`
	License      LicenseConfig      `mapstructure:"license"`
}

// ApiConfigConfig API 配置相关设置。
type ApiConfigConfig struct {
	// AutoEncrypt 是否自动加密敏感字段
	AutoEncrypt bool `mapstructure:"auto_encrypt"`
	// DefaultTimeout 默认请求超时（秒）
	DefaultTimeout int `mapstructure:"default_timeout"`
}

// TranscoderConfig 控制 HLS / ffmpeg 后端。
type TranscoderConfig struct {
	Encoder            string `mapstructure:"encoder"` // "" / nvenc / qsv / vaapi
	Enabled            bool   `mapstructure:"enabled"`
	HardwareAccel      bool   `mapstructure:"hardware_accel"`
	Preset             string `mapstructure:"preset"`
	VideoBitrate       string `mapstructure:"video_bitrate"`
	MaxRate            string `mapstructure:"max_rate"`
	BufSize            string `mapstructure:"buf_size"`
	MaxHeight          int    `mapstructure:"max_height"`
	SegmentSeconds     int    `mapstructure:"segment_seconds"`
	Realtime           bool   `mapstructure:"realtime"`
	Threads            int    `mapstructure:"threads"`
	MaxConcurrent      int    `mapstructure:"max_concurrent"`
	IdleTimeoutSeconds int    `mapstructure:"idle_timeout_seconds"`
}

// AppConfig 保存运行时应用参数。
type AppConfig struct {
	Port        int    `mapstructure:"port"`
	Debug       bool   `mapstructure:"debug"`
	Env         string `mapstructure:"env"`
	DataDir     string `mapstructure:"data_dir"`
	WebDir      string `mapstructure:"web_dir"`
	FFmpegPath  string `mapstructure:"ffmpeg_path"`
	FFprobePath string `mapstructure:"ffprobe_path"`
	// FFprobeMaxConcurrent limits concurrent ffprobe/ffmpeg metadata probes.
	// NAS devices can become unresponsive when a scan starts many probe
	// processes at once, so the default is deliberately conservative.
	FFprobeMaxConcurrent int      `mapstructure:"ffprobe_max_concurrent"`
	VAAPIDevice          string   `mapstructure:"vaapi_device"`
	CORSOrigins          []string `mapstructure:"cors_origins"`
	ServerURL            string   `mapstructure:"server_url"`
}

// DatabaseConfig 配置 GORM + SQLite。
type DatabaseConfig struct {
	DBPath       string `mapstructure:"db_path"`
	WALMode      bool   `mapstructure:"wal_mode"`
	BusyTimeout  int    `mapstructure:"busy_timeout"`
	CacheSize    int    `mapstructure:"cache_size"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// SecretsConfig 保存 JWT / 第三方 API 密钥（不要提交值）。
type SecretsConfig struct {
	JWTSecret      string `mapstructure:"jwt_secret"`
	TMDbAPIKey     string `mapstructure:"tmdb_api_key"`
	TMDbAPIProxy   string `mapstructure:"tmdb_api_proxy"`
	TMDbImageProxy string `mapstructure:"tmdb_image_proxy"`
	BangumiToken   string `mapstructure:"bangumi_access_token"`
	TheTVDBAPIKey  string `mapstructure:"thetvdb_api_key"`
	FanartAPIKey   string `mapstructure:"fanart_tv_api_key"`
	DoubanCookie   string `mapstructure:"douban_cookie"`
	// 用于加密的密钥，如果为空则使用 JWTSecret
	EncryptionKey string `mapstructure:"encryption_key"`
}

// LoggingConfig 配置 Zap。
type LoggingConfig struct {
	Level          string `mapstructure:"level"`
	Format         string `mapstructure:"format"`
	OutputPath     string `mapstructure:"output_path"`
	EnableRotation bool   `mapstructure:"enable_rotation"`
	MaxSizeMB      int    `mapstructure:"max_size_mb"`
	MaxAgeDays     int    `mapstructure:"max_age_days"`
	MaxBackups     int    `mapstructure:"max_backups"`
}

// CacheConfig 控制磁盘转码/刮削缓存。
type CacheConfig struct {
	CacheDir           string `mapstructure:"cache_dir"`
	MaxDiskUsageMB     int    `mapstructure:"max_disk_usage_mb"`
	TTLHours           int    `mapstructure:"ttl_hours"`
	AutoCleanup        bool   `mapstructure:"auto_cleanup"`
	CleanupIntervalMin int    `mapstructure:"cleanup_interval_min"`
}

// MediaConfig 保存默认库位置（用于引导库）。
type MediaConfig struct {
	MoviesDir string `mapstructure:"movies_dir"`
	TVDir     string `mapstructure:"tv_dir"`
	AnimeDir  string `mapstructure:"anime_dir"`
}

// AIConfig 配置可选的 LLM 提供者。
type AIConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Provider      string `mapstructure:"provider"`
	APIBase       string `mapstructure:"api_base"`
	APIKey        string `mapstructure:"api_key"`
	Model         string `mapstructure:"model"`
	Timeout       int    `mapstructure:"timeout"`
	MaxConcurrent int    `mapstructure:"max_concurrent"`
}

// LicenseConfig configures the optional MediaStationLicenseServer bridge.
type LicenseConfig struct {
	ServerURL  string `mapstructure:"server_url"`
	HMACSecret string `mapstructure:"hmac_secret"`
}

// OrganizerConfig 配置媒体文件智能分类整理。
type OrganizerConfig struct {
	SmartClassify     bool              `mapstructure:"smart_classify"`
	AutoAfterDownload bool              `mapstructure:"auto_after_download"`
	Categories        map[string]string `mapstructure:"categories"`
}

// FlareSolverrConfig 配置 FlareSolverr 服务（用于绕过 Cloudflare/WAF）。
type FlareSolverrConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
	Session string `mapstructure:"session"`
	Timeout int    `mapstructure:"timeout"`
}

// Load 从默认值 / 文件 / 环境读取配置。
//
// 即使没有文件也始终返回可用的 Config。
func Load() (*Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !asConfigFileNotFound(err, &notFound) {
			return nil, fmt.Errorf("read main config: %w", err)
		}
	}

	// 合并 ./config/*.yaml 下的分片文件。
	if entries, err := os.ReadDir("config"); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			s := viper.New()
			s.SetConfigFile(filepath.Join("config", e.Name()))
			if err := s.ReadInConfig(); err == nil {
				_ = v.MergeConfigMap(s.AllSettings())
			}
		}
	}

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.port", 8080)
	v.SetDefault("app.debug", false)
	v.SetDefault("app.env", "production")
	v.SetDefault("app.data_dir", "./data")
	v.SetDefault("app.web_dir", "./web/dist")
	v.SetDefault("app.ffmpeg_path", "ffmpeg")
	v.SetDefault("app.ffprobe_path", "ffprobe")
	v.SetDefault("app.ffprobe_max_concurrent", 1)
	v.SetDefault("app.vaapi_device", "/dev/dri/renderD128")
	v.SetDefault("app.cors_origins", []string{})
	v.SetDefault("app.server_url", "")

	v.SetDefault("database.db_path", "./data/mediastation.db")
	v.SetDefault("database.wal_mode", true)
	v.SetDefault("database.busy_timeout", 5000)
	v.SetDefault("database.cache_size", -20000)
	v.SetDefault("database.max_open_conns", 1)
	v.SetDefault("database.max_idle_conns", 1)

	v.SetDefault("secrets.jwt_secret", "")

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")
	v.SetDefault("logging.max_size_mb", 100)
	v.SetDefault("logging.max_age_days", 30)
	v.SetDefault("logging.max_backups", 10)

	v.SetDefault("cache.cache_dir", "./cache")
	v.SetDefault("cache.cleanup_interval_min", 60)

	v.SetDefault("ai.enabled", false)
	v.SetDefault("ai.provider", "openai")
	v.SetDefault("ai.api_base", "https://api.openai.com/v1")
	v.SetDefault("ai.model", "gpt-4o-mini")
	v.SetDefault("ai.timeout", 30)
	v.SetDefault("ai.max_concurrent", 3)

	v.SetDefault("flaresolverr.enabled", false)
	v.SetDefault("flaresolverr.url", "http://localhost:8191")
	v.SetDefault("flaresolverr.session", "mediastation")
	v.SetDefault("flaresolverr.timeout", 60)

	v.SetDefault("downloads.smart_classify", true)
	v.SetDefault("organizer.smart_classify", false)
	v.SetDefault("organizer.auto_after_download", false)
	v.SetDefault("organize.scrape_after", false)
	v.SetDefault("scrape.delay_min_ms", 250)
	v.SetDefault("scrape.delay_max_ms", 500)
	v.SetDefault("organizer.categories.chinese_movie", "华语电影")
	v.SetDefault("organizer.categories.animation_movie", "动画电影")
	v.SetDefault("organizer.categories.foreign_movie", "外语电影")
	v.SetDefault("organizer.categories.euus_movie", "欧美电影")
	v.SetDefault("organizer.categories.jk_movie", "日韩电影")
	v.SetDefault("organizer.categories.domestic_tv", "国产剧")
	v.SetDefault("organizer.categories.euus_tv", "欧美剧")
	v.SetDefault("organizer.categories.jk_tv", "日韩剧")
	v.SetDefault("organizer.categories.jp_anime", "日番")
	v.SetDefault("organizer.categories.cn_anime", "国漫")
	v.SetDefault("organizer.categories.variety", "综艺")
	v.SetDefault("organizer.categories.documentary", "纪录片")
	v.SetDefault("organizer.categories.children", "儿童")
	v.SetDefault("organizer.categories.uncategorized_tv", "未分类")

	v.SetDefault("transcoder.encoder", "")
	v.SetDefault("transcoder.enabled", true)
	v.SetDefault("transcoder.hardware_accel", false)
	v.SetDefault("transcoder.preset", "veryfast")
	v.SetDefault("transcoder.video_bitrate", "1500k")
	v.SetDefault("transcoder.max_rate", "1800k")
	v.SetDefault("transcoder.buf_size", "3000k")
	v.SetDefault("transcoder.max_height", 720)
	v.SetDefault("transcoder.segment_seconds", 4)
	v.SetDefault("transcoder.realtime", true)
	v.SetDefault("transcoder.threads", 2)
	v.SetDefault("transcoder.max_concurrent", 1)
	v.SetDefault("transcoder.idle_timeout_seconds", 120)

	// API Config 默认设置
	v.SetDefault("api_config.auto_encrypt", true)
	v.SetDefault("api_config.default_timeout", 30)

	v.SetDefault("license.server_url", "")
	v.SetDefault("license.hmac_secret", "")
}

// normalize 填充派生默认值并自愈空的关键字段。
func (c *Config) normalize() error {
	if c.App.DataDir == "" {
		c.App.DataDir = "./data"
	}
	if c.Database.DBPath == "" {
		c.Database.DBPath = filepath.Join(c.App.DataDir, "mediastation.db")
	}
	if c.Cache.CacheDir == "" {
		c.Cache.CacheDir = filepath.Join(c.App.DataDir, "cache")
	}
	if c.Secrets.JWTSecret == "" {
		// 持久化自动生成的密钥以在操作员忘记配置时保持会话稳定。
		path := filepath.Join(c.App.DataDir, ".jwt_secret")
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 { // #nosec G304 -- path is fixed to .jwt_secret under configured DataDir.
			c.Secrets.JWTSecret = strings.TrimSpace(string(data))
		} else {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return fmt.Errorf("generate jwt secret: %w", err)
			}
			c.Secrets.JWTSecret = hex.EncodeToString(buf)
			_ = os.MkdirAll(c.App.DataDir, 0o750)
			_ = os.WriteFile(path, []byte(c.Secrets.JWTSecret), 0o600)
		}
	}
	return nil
}

// asConfigFileNotFound 是 errors.As 的小辅助函数，避免在这个短文件中导入 errors。
func asConfigFileNotFound(err error, target *viper.ConfigFileNotFoundError) bool {
	if err == nil {
		return false
	}
	if v, ok := err.(viper.ConfigFileNotFoundError); ok {
		*target = v
		return true
	}
	return false
}





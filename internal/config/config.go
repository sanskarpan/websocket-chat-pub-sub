package config

import (
	"errors"
	"os"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	App           AppConfig           `mapstructure:"app"`
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Redis         RedisConfig         `mapstructure:"redis"`
	Auth          AuthConfig          `mapstructure:"auth"`
	RateLimit     RateLimitConfig     `mapstructure:"rate_limit"`
	Features      FeaturesConfig      `mapstructure:"features"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Storage       StorageConfig       `mapstructure:"storage"`
}

type AppConfig struct {
	Name        string `mapstructure:"name"`
	Version     string `mapstructure:"version"`
	Environment string `mapstructure:"environment"`
}

type ServerConfig struct {
	Host      string          `mapstructure:"host"`
	Port      int             `mapstructure:"port"`
	Websocket WebsocketConfig `mapstructure:"websocket"`
	HTTP      HTTPConfig      `mapstructure:"http"`
	TLS       TLSConfig       `mapstructure:"tls"`
}

type WebsocketConfig struct {
	Path                string        `mapstructure:"path"`
	ReadBufferSize      int           `mapstructure:"read_buffer_size"`
	WriteBufferSize     int           `mapstructure:"write_buffer_size"`
	MaxMessageSize      int64         `mapstructure:"max_message_size"`
	PingInterval        time.Duration `mapstructure:"ping_interval"`
	PongTimeout         time.Duration `mapstructure:"pong_timeout"`
	WriteTimeout        time.Duration `mapstructure:"write_timeout"`
	MaxConnectionsPerIP int           `mapstructure:"max_connections_per_ip"`
}

type HTTPConfig struct {
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	IdleTimeout    time.Duration `mapstructure:"idle_timeout"`
	MaxHeaderBytes int           `mapstructure:"max_header_bytes"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

type DatabaseConfig struct {
	Postgresql PostgresqlConfig `mapstructure:"postgresql"`
}

type PostgresqlConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Database        string        `mapstructure:"database"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	SSLMode         string        `mapstructure:"ssl_mode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type RedisConfig struct {
	Mode         string   `mapstructure:"mode"`
	Addrs        []string `mapstructure:"addrs"`
	Password     string   `mapstructure:"password"`
	DB           int      `mapstructure:"db"`
	PoolSize     int      `mapstructure:"pool_size"`
	MinIdleConns int      `mapstructure:"min_idle_conns"`
}

type AuthConfig struct {
	JWT    JWTConfig    `mapstructure:"jwt"`
	BCrypt BCryptConfig `mapstructure:"bcrypt"`
}

type JWTConfig struct {
	Algorithm       string        `mapstructure:"algorithm"`
	PrivateKey      string        `mapstructure:"private_key"`
	PublicKey       string        `mapstructure:"public_key"`
	AccessTokenTTL  time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
	Issuer          string        `mapstructure:"issuer"`
	Audience        []string      `mapstructure:"audience"`
}

type BCryptConfig struct {
	Cost int `mapstructure:"cost"`
}

type RateLimitConfig struct {
	Enabled bool            `mapstructure:"enabled"`
	Rules   []RateLimitRule `mapstructure:"rules"`
}

type RateLimitRule struct {
	Key    string        `mapstructure:"key"`
	Limit  int           `mapstructure:"limit"`
	Window time.Duration `mapstructure:"window"`
}

type FeaturesConfig struct {
	MessageRetentionDays   int      `mapstructure:"message_retention_days"`
	MaxFileSize            int64    `mapstructure:"max_file_size"`
	AllowedFileTypes       []string `mapstructure:"allowed_file_types"`
	EnableReadReceipts     bool     `mapstructure:"enable_read_receipts"`
	EnableTypingIndicators bool     `mapstructure:"enable_typing_indicators"`
	EnableReactions        bool     `mapstructure:"enable_reactions"`
	EnableThreads          bool     `mapstructure:"enable_threads"`
}

type ObservabilityConfig struct {
	Logging LoggingConfig `mapstructure:"logging"`
	Metrics MetricsConfig `mapstructure:"metrics"`
	Tracing TracingConfig `mapstructure:"tracing"`
}

type LoggingConfig struct {
	Level    string `mapstructure:"level"`
	Format   string `mapstructure:"format"`
	Output   string `mapstructure:"output"`
	FilePath string `mapstructure:"file_path"`
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

type TracingConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	Exporter     string  `mapstructure:"exporter"`
	Endpoint     string  `mapstructure:"endpoint"`
	SamplingRate float64 `mapstructure:"sampling_rate"`
}

type StorageConfig struct {
	Type  string      `mapstructure:"type"`
	MinIO MinIOConfig `mapstructure:"minio"`
	S3    S3Config    `mapstructure:"s3"`
}

type MinIOConfig struct {
	Endpoint  string `mapstructure:"endpoint"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	Bucket    string `mapstructure:"bucket"`
	UseSSL    bool   `mapstructure:"use_ssl"`
}

type S3Config struct {
	Region string `mapstructure:"region"`
	Bucket string `mapstructure:"bucket"`
}

func (c *Config) Validate() error {
	if c.App.Environment == "production" {
		if c.Auth.JWT.PrivateKey == "" || c.Auth.JWT.PrivateKey == "default-secret-change-in-production" {
			return errors.New("production environment requires a secure JWT_SECRET environment variable")
		}
		if len(c.Auth.JWT.PrivateKey) < 32 {
			return errors.New("JWT_SECRET must be at least 32 characters long in production")
		}
		if c.Database.Postgresql.Password == "" {
			return errors.New("production environment requires DB_PASSWORD environment variable")
		}
	}
	return nil
}

func Load() *Config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	cfg := defaultConfig()
	if err := viper.ReadInConfig(); err == nil {
		_ = viper.Unmarshal(cfg)
	}

	if envSecret := os.Getenv("JWT_SECRET"); envSecret != "" {
		cfg.Auth.JWT.PrivateKey = envSecret
		cfg.Auth.JWT.PublicKey = envSecret
	}
	if cfg.Auth.JWT.PrivateKey == "" {
		cfg.Auth.JWT.PrivateKey = "development-secret-key-change-in-production-32bytes!"
		cfg.Auth.JWT.PublicKey = cfg.Auth.JWT.PrivateKey
	}

	if envDbPass := os.Getenv("DB_PASSWORD"); envDbPass != "" {
		cfg.Database.Postgresql.Password = envDbPass
	}
	if envRedisPass := os.Getenv("REDIS_PASSWORD"); envRedisPass != "" {
		cfg.Redis.Password = envRedisPass
	}

	return cfg
}

func defaultConfig() *Config {
	return &Config{
		App: AppConfig{
			Name:        "websocket-chat",
			Version:     "1.0.0",
			Environment: "development",
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8085,
			Websocket: WebsocketConfig{
				Path:                "/ws",
				ReadBufferSize:      1024,
				WriteBufferSize:     1024,
				MaxMessageSize:      65536,
				PingInterval:        30 * time.Second,
				PongTimeout:         60 * time.Second,
				WriteTimeout:        10 * time.Second,
				MaxConnectionsPerIP: 10,
			},
			HTTP: HTTPConfig{
				ReadTimeout:    30 * time.Second,
				WriteTimeout:   30 * time.Second,
				IdleTimeout:    120 * time.Second,
				MaxHeaderBytes: 1048576,
			},
			TLS: TLSConfig{
				Enabled: false,
			},
		},
		Database: DatabaseConfig{
			Postgresql: PostgresqlConfig{
				Host:            "localhost",
				Port:            5432,
				Database:        "chat",
				User:            "chat",
				Password:        os.Getenv("DB_PASSWORD"),
				SSLMode:         "disable",
				MaxOpenConns:    25,
				MaxIdleConns:    10,
				ConnMaxLifetime: 30 * time.Minute,
			},
		},
		Redis: RedisConfig{
			Mode:         "single",
			Addrs:        []string{"localhost:6379"},
			Password:     os.Getenv("REDIS_PASSWORD"),
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 5,
		},
		Auth: AuthConfig{
			JWT: JWTConfig{
				Algorithm:       "HS256",
				PrivateKey:      os.Getenv("JWT_SECRET"),
				PublicKey:       os.Getenv("JWT_SECRET"),
				AccessTokenTTL:  15 * time.Minute,
				RefreshTokenTTL: 168 * time.Hour,
				Issuer:          "chat-app",
				Audience:        []string{"chat-api"},
			},
			BCrypt: BCryptConfig{
				Cost: 12,
			},
		},
		RateLimit: RateLimitConfig{
			Enabled: true,
			Rules: []RateLimitRule{
				{Key: "message", Limit: 100, Window: time.Minute},
				{Key: "connection", Limit: 5, Window: time.Minute},
				{Key: "room_create", Limit: 10, Window: time.Hour},
			},
		},
		Features: FeaturesConfig{
			MessageRetentionDays:   365,
			MaxFileSize:            10485760,
			AllowedFileTypes:       []string{"image/*", "application/pdf"},
			EnableReadReceipts:     true,
			EnableTypingIndicators: true,
			EnableReactions:        true,
			EnableThreads:          true,
		},
		Observability: ObservabilityConfig{
			Logging: LoggingConfig{
				Level:    "debug",
				Format:   "json",
				Output:   "stdout",
				FilePath: "logs/app.log",
			},
			Metrics: MetricsConfig{
				Enabled: true,
				Port:    9090,
				Path:    "/metrics",
			},
			Tracing: TracingConfig{
				Enabled:      false,
				Exporter:     "jaeger",
				Endpoint:     "http://localhost:14268/api/traces",
				SamplingRate: 0.01,
			},
		},
	}
}

package main

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"testlink/internal/ratelimit"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	ClickHouse ClickHouseConfig `yaml:"clickhouse"`
	Redis      RedisConfig      `yaml:"redis"`
	Auth       AuthConfig       `yaml:"auth"`
	GeoIP      GeoIPConfig      `yaml:"geoip"`
	RateLimit  ratelimit.Config `yaml:"ratelimit"`
}

type ServerConfig struct {
	Port              int      `yaml:"port"`
	TrustedProxies    int      `yaml:"trusted_proxies"`
	TrustedProxyCIDRs []string `yaml:"trusted_proxy_cidrs"`
}

type ClickHouseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
}

type AuthConfig struct {
	JWTSecret  string `yaml:"jwt_secret"`
	AdminToken string `yaml:"admin_token"`
}

type GeoIPConfig struct {
	IP2RegionV4    string `yaml:"ip2region_v4"`
	IP2RegionV6    string `yaml:"ip2region_v6"`
	MaxmindCountry string `yaml:"maxmind_country"`
	MaxmindASN     string `yaml:"maxmind_asn"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Config{
		Server: ServerConfig{Port: 8080, TrustedProxies: 1},
		ClickHouse: ClickHouseConfig{
			Host: "localhost", Port: 9000, Database: "testlink", Username: "default",
		},
		Redis: RedisConfig{Host: "localhost", Port: 6379},
		GeoIP: GeoIPConfig{
			IP2RegionV4:    "ip2region_v4.xdb",
			IP2RegionV6:    "ip2region_v6.xdb",
			MaxmindCountry: "GeoLite2-Country.mmdb",
			MaxmindASN:     "GeoLite2-ASN.mmdb",
		},
		RateLimit: ratelimit.Config{
			SessionPerIPPerMin:  5,
			ReportPerIPPerMin:   60,
			GlobalSessionPerSec: 100,
		},
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	applyEnvOverrides(&cfg)
	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	setInt(&cfg.Server.Port, "TESTLINK_PORT")
	setStringSlice(&cfg.Server.TrustedProxyCIDRs, "TESTLINK_TRUSTED_PROXY_CIDRS")

	setString(&cfg.ClickHouse.Host, "TESTLINK_CLICKHOUSE_HOST")
	setInt(&cfg.ClickHouse.Port, "TESTLINK_CLICKHOUSE_PORT")
	setString(&cfg.ClickHouse.Database, "TESTLINK_CLICKHOUSE_DATABASE")
	setString(&cfg.ClickHouse.Username, "TESTLINK_CLICKHOUSE_USERNAME")
	setString(&cfg.ClickHouse.Password, "TESTLINK_CLICKHOUSE_PASSWORD")

	setString(&cfg.Redis.Host, "TESTLINK_REDIS_HOST")
	setInt(&cfg.Redis.Port, "TESTLINK_REDIS_PORT")
	setString(&cfg.Redis.Password, "TESTLINK_REDIS_PASSWORD")

	setString(&cfg.Auth.JWTSecret, "TESTLINK_JWT_SECRET")
	setString(&cfg.Auth.AdminToken, "TESTLINK_ADMIN_TOKEN")
}

func setString(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func setInt(dst *int, key string) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func setStringSlice(dst *[]string, key string) {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		*dst = out
	}
}

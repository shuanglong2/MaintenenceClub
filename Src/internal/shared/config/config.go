// Package config 从环境变量读取进程配置.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config 是进程启动后不再改变的配置.
type Config struct {
	Env           string
	Addr          string
	DB            string
	Data          string
	UploadQuotaMB int
	CookieSecure  bool
	DevAssets     string
	TrustedProxy  string
}

// Load 读取环境变量. 空值使用默认, 非法值返回错误.
func Load() (Config, error) {
	var cfg Config
	var err error
	cfg.Env = envStr("MCLUB_ENV", "prod")
	if cfg.Env != "prod" && cfg.Env != "dev" {
		return Config{}, fmt.Errorf("MCLUB_ENV 只能是 prod 或 dev")
	}
	cfg.Addr = envStr("MCLUB_ADDR", "127.0.0.1:8080")
	if err = loopback(cfg.Addr); err != nil {
		return Config{}, err
	}
	cfg.DB = envStr("MCLUB_DB", "host=/var/run/postgresql dbname=mclub user=mclub")
	cfg.Data = envStr("MCLUB_DATA", "/srv/maintclub/uploads")
	cfg.UploadQuotaMB, err = envInt("MCLUB_UPLOAD_QUOTA_MB", 6144)
	if err != nil {
		return Config{}, err
	}
	if cfg.UploadQuotaMB < 1 {
		return Config{}, fmt.Errorf("MCLUB_UPLOAD_QUOTA_MB 必须大于 0")
	}
	cfg.CookieSecure, err = envBool("MCLUB_COOKIE_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	cfg.DevAssets = envStr("MCLUB_DEV_ASSETS", "")
	if cfg.Env == "prod" && cfg.DevAssets != "" {
		return Config{}, fmt.Errorf("生产环境不能设置 MCLUB_DEV_ASSETS")
	}
	cfg.TrustedProxy = envStr("MCLUB_TRUSTED_PROXY", "127.0.0.1")
	if ip := net.ParseIP(cfg.TrustedProxy); ip == nil {
		return Config{}, fmt.Errorf("MCLUB_TRUSTED_PROXY 必须是 IP 地址")
	}
	return cfg, nil
}

func envStr(key, fallback string) string {
	v, ok := os.LookupEnv(key)
	v = strings.TrimSpace(v)
	if !ok || v == "" {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	v = strings.TrimSpace(v)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s 不是整数", key)
	}
	return n, nil
}

func envBool(key string, fallback bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	v = strings.TrimSpace(strings.ToLower(v))
	if !ok || v == "" {
		return fallback, nil
	}
	switch v {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s 不是布尔值", key)
	}
}

func loopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("MCLUB_ADDR 无效: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("MCLUB_ADDR 只能监听回环地址")
	}
	return nil
}

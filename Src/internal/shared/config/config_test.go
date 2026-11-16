package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"MCLUB_ENV",
		"MCLUB_ADDR",
		"MCLUB_DB",
		"MCLUB_DATA",
		"MCLUB_UPLOAD_QUOTA_MB",
		"MCLUB_COOKIE_SECURE",
		"MCLUB_DEV_ASSETS",
		"MCLUB_TRUSTED_PROXY",
	} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != "prod" || cfg.Addr != "127.0.0.1:8080" || cfg.UploadQuotaMB != 6144 {
		t.Fatalf("%+v", cfg)
	}
	if cfg.CookieSecure || cfg.DevAssets != "" || cfg.TrustedProxy != "127.0.0.1" {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadRejectsPublicAddr(t *testing.T) {
	t.Setenv("MCLUB_ADDR", "0.0.0.0:8080")
	if _, err := Load(); err == nil {
		t.Fatal("应拒绝非回环地址")
	}
}

func TestLoadProdRejectsDevAssets(t *testing.T) {
	t.Setenv("MCLUB_ENV", "prod")
	t.Setenv("MCLUB_DEV_ASSETS", "Src/internal/web")
	if _, err := Load(); err == nil {
		t.Fatal("生产环境应拒绝开发资源目录")
	}
}

func TestLoadDevAllowsAssets(t *testing.T) {
	t.Setenv("MCLUB_ENV", "dev")
	t.Setenv("MCLUB_DEV_ASSETS", "Src/internal/web")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DevAssets != "Src/internal/web" {
		t.Fatal(cfg.DevAssets)
	}
}

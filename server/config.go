package main

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Listen       string
	MusicRoot    string
	DataDir      string
	NavidromeURL string
	ScanUser     string
	ScanPassword string
	SessionKey   []byte
	MaxUpload    int64
	TrustedNets  []netip.Prefix
	SecureCookie bool
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func LoadConfig() (*Config, error) {
	c := &Config{
		Listen:       env("LISTEN", ":4534"),
		MusicRoot:    env("MUSIC_ROOT", "/srv/music"),
		DataDir:      env("DATA_DIR", "/var/lib/buddymusic"),
		NavidromeURL: strings.TrimRight(env("NAVIDROME_URL", "http://127.0.0.1:4533"), "/"),
		ScanUser:     env("SCAN_USER", "buddymusic-scan"),
		ScanPassword: os.Getenv("SCAN_PASSWORD"),
		SecureCookie: env("SECURE_COOKIE", "false") == "true",
	}
	mb, err := strconv.Atoi(env("MAX_UPLOAD_MB", "200"))
	if err != nil || mb <= 0 {
		return nil, fmt.Errorf("MAX_UPLOAD_MB: invalid value")
	}
	c.MaxUpload = int64(mb) << 20
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SESSION_KEY"))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("SESSION_KEY must be 32 bytes, base64 encoded (openssl rand -base64 32)")
	}
	c.SessionKey = key
	for _, s := range strings.Split(env("TRUSTED_NETS", "127.0.0.0/8,::1/128,100.64.0.0/10,fd7a:115c:a1e0::/48"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_NETS: %w", err)
		}
		c.TrustedNets = append(c.TrustedNets, p)
	}
	return c, nil
}

func (c *Config) IncomingDir() string { return filepath.Join(c.MusicRoot, "_incoming") }
func (c *Config) LibraryDir() string  { return filepath.Join(c.MusicRoot, "library") }

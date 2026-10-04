// Package config loads runtime configuration from the environment.
package config

import (
    "fmt"
    "net"
    "net/url"
    "os"
    "strconv"
    "strings"
    "time"
)

// Config holds all runtime settings.
type Config struct {
    Listen           string
    BaseURL          string
    DataDir          string
    OIDCIssuer       string
    OIDCClientID     string
    OIDCClientSecret string
    OIDCScopes       []string
    OIDCGroupsClaim  string
    BootstrapAdmins  []string
    DefaultCurrency  string
    MaxImageBytes    int64
    MaxDocumentBytes int64
    SessionIdle      time.Duration
    SessionMax       time.Duration
    TrustedProxies   []*net.IPNet
    InsecureCookies  bool
    LogLevel         string
    DevAuth          string
}

func env(key, def string) string {
    if v, ok := os.LookupEnv(key); ok && v != "" {
        return v
    }
    return def
}

func splitList(s string, sep string) []string {
    var out []string
    for _, p := range strings.Split(s, sep) {
        if p = strings.TrimSpace(p); p != "" {
            out = append(out, p)
        }
    }
    return out
}

// Load reads configuration from DRAWERED_* environment variables.
func Load() (*Config, error) {
    c := &Config{
        Listen:           env("DRAWERED_LISTEN", ":8080"),
        BaseURL:          strings.TrimRight(env("DRAWERED_BASE_URL", ""), "/"),
        DataDir:          env("DRAWERED_DATA_DIR", "./data"),
        OIDCIssuer:       env("DRAWERED_OIDC_ISSUER", ""),
        OIDCClientID:     env("DRAWERED_OIDC_CLIENT_ID", ""),
        OIDCClientSecret: env("DRAWERED_OIDC_CLIENT_SECRET", ""),
        OIDCGroupsClaim:  env("DRAWERED_OIDC_GROUPS_CLAIM", "groups"),
        BootstrapAdmins:  splitList(env("DRAWERED_BOOTSTRAP_ADMINS", ""), ","),
        DefaultCurrency:  strings.ToUpper(env("DRAWERED_DEFAULT_CURRENCY", "GBP")),
        LogLevel:         env("DRAWERED_LOG_LEVEL", "info"),
        DevAuth:          env("DRAWERED_DEV_AUTH", ""),
    }

    scopes := []string{"openid", "profile", "email"}
    for _, s := range splitList(env("DRAWERED_OIDC_SCOPES", ""), " ") {
        dup := false
        for _, e := range scopes {
            dup = dup || e == s
        }
        if !dup {
            scopes = append(scopes, s)
        }
    }
    c.OIDCScopes = scopes

    var err error
    if c.MaxImageBytes, err = megabytes("DRAWERED_MAX_IMAGE_MB", 25); err != nil {
        return nil, err
    }
    if c.MaxDocumentBytes, err = megabytes("DRAWERED_MAX_DOCUMENT_MB", 50); err != nil {
        return nil, err
    }
    if c.SessionIdle, err = duration("DRAWERED_SESSION_IDLE", 168*time.Hour); err != nil {
        return nil, err
    }
    if c.SessionMax, err = duration("DRAWERED_SESSION_MAX", 720*time.Hour); err != nil {
        return nil, err
    }
    if c.InsecureCookies, err = strconv.ParseBool(env("DRAWERED_INSECURE_COOKIES", "false")); err != nil {
        return nil, fmt.Errorf("DRAWERED_INSECURE_COOKIES: %w", err)
    }
    for _, cidr := range splitList(env("DRAWERED_TRUSTED_PROXIES", ""), ",") {
        if !strings.Contains(cidr, "/") {
            if strings.Contains(cidr, ":") {
                cidr += "/128"
            } else {
                cidr += "/32"
            }
        }
        _, n, err := net.ParseCIDR(cidr)
        if err != nil {
            return nil, fmt.Errorf("DRAWERED_TRUSTED_PROXIES: %w", err)
        }
        c.TrustedProxies = append(c.TrustedProxies, n)
    }
    if len(c.DefaultCurrency) != 3 {
        return nil, fmt.Errorf("DRAWERED_DEFAULT_CURRENCY must be an ISO 4217 code")
    }
    return c, nil
}

// ValidateServe checks the settings needed to run the web server.
func (c *Config) ValidateServe(devAuthAllowed bool) error {
    if c.BaseURL == "" {
        return fmt.Errorf("DRAWERED_BASE_URL is required")
    }
    if _, err := url.Parse(c.BaseURL); err != nil {
        return fmt.Errorf("DRAWERED_BASE_URL: %w", err)
    }
    if c.DevAuth != "" && devAuthAllowed {
        return nil
    }
    if c.OIDCIssuer == "" || c.OIDCClientID == "" {
        return fmt.Errorf("DRAWERED_OIDC_ISSUER and DRAWERED_OIDC_CLIENT_ID are required")
    }
    return nil
}

func megabytes(key string, def int64) (int64, error) {
    v := env(key, strconv.FormatInt(def, 10))
    n, err := strconv.ParseInt(v, 10, 64)
    if err != nil || n <= 0 {
        return 0, fmt.Errorf("%s: invalid size %q", key, v)
    }
    return n << 20, nil
}

func duration(key string, def time.Duration) (time.Duration, error) {
    v := env(key, def.String())
    d, err := time.ParseDuration(v)
    if err != nil || d <= 0 {
        return 0, fmt.Errorf("%s: invalid duration %q", key, v)
    }
    return d, nil
}

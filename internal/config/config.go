package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	MeiliURL, MeiliMasterKey                                                   string
	Env, HTTPAddr, BaseURL, DatabaseURL, UploadDir, AuthSecret                 string
	AllowedDomains, AllowedEmails, AdminEmails                                 []string
	TrustedProxies                                                             []*net.IPNet
	SMTPHost, SMTPPort, SMTPUser, SMTPPassword, SMTPFrom                       string
	SMTPStartTLS, SecureCookies                                                bool
	UploadQuotaBytes                                                           int64
	UploadPhotoLimit                                                           int
	ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout, ShutdownTimeout time.Duration
}

func Load() (Config, error) { return load(os.LookupEnv) }
func load(lookup func(string) (string, bool)) (Config, error) {
	get := func(k, d string) string {
		if v, ok := lookup(k); ok {
			return v
		}
		return d
	}
	c := Config{Env: get("APP_ENV", "development"), HTTPAddr: get("HTTP_ADDR", "127.0.0.1:8080"), BaseURL: strings.TrimRight(strings.TrimSpace(get("BASE_URL", "http://localhost:8080")), "/"),
		DatabaseURL: get("DATABASE_URL", "postgres://dormhub:dormhub_dev@127.0.0.1:5432/dormhub?sslmode=disable"), UploadDir: get("UPLOAD_DIR", "./data/uploads"), AuthSecret: get("AUTH_SECRET", ""),
		SMTPHost: get("SMTP_HOST", "localhost"), SMTPPort: get("SMTP_PORT", "1025"), SMTPUser: get("SMTP_USER", ""), SMTPPassword: get("SMTP_PASSWORD", ""), SMTPFrom: get("SMTP_FROM", "Dorm Hub <noreply@students.example>")}
	if c.Env != "development" && c.Env != "production" {
		return c, fmt.Errorf("APP_ENV: expected development or production")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return c, fmt.Errorf("BASE_URL: expected an HTTP(S) origin without a path")
	}
	c.SecureCookies = u.Scheme == "https"
	c.MeiliURL = strings.TrimRight(strings.TrimSpace(get("MEILI_URL", "")), "/")
	c.MeiliMasterKey = get("MEILI_MASTER_KEY", "")
	if c.MeiliURL != "" {
		endpoint, e := url.Parse(c.MeiliURL)
		if e != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil {
			return c, fmt.Errorf("MEILI_URL: expected an HTTP(S) origin without credentials or a path")
		}
		if len(c.MeiliMasterKey) < 32 {
			return c, fmt.Errorf("MEILI_MASTER_KEY: expected at least 32 bytes when MEILI_URL is set")
		}
	}
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return c, fmt.Errorf("HTTP_ADDR: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return c, fmt.Errorf("HTTP_ADDR: invalid port")
	}
	c.AllowedDomains = split(get("ALLOWED_EMAIL_DOMAINS", "students.example"))
	c.AllowedEmails = split(get("ALLOWED_EMAIL_ADDRESSES", ""))
	c.AdminEmails = split(get("ADMIN_EMAILS", "admin@students.example"))
	if len(c.AllowedDomains) == 0 {
		return c, fmt.Errorf("ALLOWED_EMAIL_DOMAINS is required")
	}
	for _, d := range c.AllowedDomains {
		if strings.ContainsAny(d, "@ /:\\") || !strings.Contains(d, ".") {
			return c, fmt.Errorf("ALLOWED_EMAIL_DOMAINS: invalid domain")
		}
	}
	for _, email := range c.AllowedEmails {
		address, e := mail.ParseAddress(email)
		if e != nil || address.Address != email || len(email) > 254 || len(strings.Split(email, "@")) != 2 {
			return c, fmt.Errorf("ALLOWED_EMAIL_ADDRESSES: invalid email")
		}
	}
	for _, email := range c.AdminEmails {
		address, e := mail.ParseAddress(email)
		if e != nil || address.Address != email || !c.AllowsEmail(email) {
			return c, fmt.Errorf("ADMIN_EMAILS: invalid or disallowed email")
		}
	}
	for _, value := range split(get("TRUSTED_PROXY_CIDRS", "")) {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return c, fmt.Errorf("TRUSTED_PROXY_CIDRS: invalid CIDR")
		}
		c.TrustedProxies = append(c.TrustedProxies, network)
	}
	if c.DatabaseURL == "" || c.UploadDir == "" || c.SMTPHost == "" {
		return c, fmt.Errorf("DATABASE_URL, UPLOAD_DIR and SMTP_HOST must not be empty")
	}
	if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
		return c, fmt.Errorf("SMTP_FROM: %w", err)
	}
	smtpPort, err := strconv.Atoi(c.SMTPPort)
	if err != nil || smtpPort < 1 || smtpPort > 65535 {
		return c, fmt.Errorf("SMTP_PORT: invalid port")
	}
	tlsValue := get("SMTP_STARTTLS", strconv.FormatBool(c.Env == "production"))
	c.SMTPStartTLS, err = strconv.ParseBool(tlsValue)
	if err != nil {
		return c, fmt.Errorf("SMTP_STARTTLS: %w", err)
	}
	if (c.SMTPUser == "") != (c.SMTPPassword == "") {
		return c, fmt.Errorf("SMTP_USER and SMTP_PASSWORD must be set together")
	}
	if c.Env == "production" && (!c.SecureCookies || !c.SMTPStartTLS || len(c.AuthSecret) < 32 || c.AuthSecret == "dorm-hub-local-development-secret-change-before-production" || strings.Contains(strings.ToLower(c.AuthSecret), "change-me")) {
		return c, fmt.Errorf("production requires HTTPS BASE_URL, SMTP_STARTTLS=true and a non-default AUTH_SECRET of at least 32 characters")
	}
	if c.AuthSecret == "" {
		buf := make([]byte, 32)
		if _, err = rand.Read(buf); err != nil {
			return c, err
		}
		c.AuthSecret = hex.EncodeToString(buf)
	}
	if len(c.AuthSecret) < 32 {
		return c, fmt.Errorf("AUTH_SECRET must have at least 32 characters")
	}
	quota, err := strconv.ParseInt(get("UPLOAD_QUOTA_MB", "200"), 10, 64)
	if err != nil || quota < 1 || quota > 10000 {
		return c, fmt.Errorf("UPLOAD_QUOTA_MB: expected 1..10000")
	}
	c.UploadQuotaBytes = quota * 1024 * 1024
	c.UploadPhotoLimit, err = strconv.Atoi(get("UPLOAD_PHOTO_LIMIT", "200"))
	if err != nil || c.UploadPhotoLimit < 1 || c.UploadPhotoLimit > 10000 {
		return c, fmt.Errorf("UPLOAD_PHOTO_LIMIT: expected 1..10000")
	}
	settings := []struct {
		name     string
		fallback time.Duration
		target   *time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", 5 * time.Second, &c.ReadHeaderTimeout}, {"HTTP_READ_TIMEOUT", 30 * time.Second, &c.ReadTimeout}, {"HTTP_WRITE_TIMEOUT", 30 * time.Second, &c.WriteTimeout}, {"HTTP_IDLE_TIMEOUT", 60 * time.Second, &c.IdleTimeout}, {"HTTP_SHUTDOWN_TIMEOUT", 10 * time.Second, &c.ShutdownTimeout}}
	for _, s := range settings {
		value, ok := lookup(s.name)
		if !ok {
			*s.target = s.fallback
			continue
		}
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return c, fmt.Errorf("%s: expected a positive duration", s.name)
		}
		*s.target = duration
	}
	return c, nil
}
func split(value string) []string {
	var result []string
	for _, v := range strings.Split(value, ",") {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}
func (c Config) AllowsEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, allowed := range c.AllowedEmails {
		if email == allowed {
			return true
		}
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	for _, d := range c.AllowedDomains {
		if parts[1] == d {
			return true
		}
	}
	return false
}
func (c Config) IsAdmin(email string) bool {
	for _, e := range c.AdminEmails {
		if email == e {
			return true
		}
	}
	return false
}
func (c Config) SessionCookie() string {
	if c.SecureCookies {
		return "__Host-dormhub_session"
	}
	return "dormhub_session"
}

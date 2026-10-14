package config

import (
	"strings"
	"testing"
	"time"
)

func settings(values map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := values[k]
		return v, ok
	}
}
func TestDefaults(t *testing.T) {
	c, err := load(settings(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != "127.0.0.1:8080" || c.ShutdownTimeout != 10*time.Second || len(c.AuthSecret) < 32 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}
func TestInvalidConfig(t *testing.T) {
	for _, test := range []struct{ key, value string }{{"HTTP_ADDR", "localhost"}, {"HTTP_ADDR", "localhost:0"}, {"HTTP_READ_TIMEOUT", "0s"}, {"BASE_URL", "http://localhost:8080/path"}, {"SMTP_PORT", "-1"}, {"ALLOWED_EMAIL_DOMAINS", ""}, {"APP_ENV", "other"}, {"AUTH_SECRET", "short"}, {"SMTP_STARTTLS", "nope"}} {
		t.Run(test.key, func(t *testing.T) {
			_, err := load(settings(map[string]string{test.key: test.value}))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("expected error for %s, got %v", test.key, err)
			}
		})
	}
}
func TestProductionRequiresSecurity(t *testing.T) {
	_, err := load(settings(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("insecure production config accepted")
	}
}
func TestDomainIsExact(t *testing.T) {
	c := Config{AllowedDomains: []string{"student.example"}}
	if !c.AllowsEmail("a@student.example") || c.AllowsEmail("a@sub.student.example") || c.AllowsEmail("a@student.example.evil.test") {
		t.Fatal("domain restriction failed")
	}
}

func TestSingleExternalAdminAddress(t *testing.T) {
	c, err := load(settings(map[string]string{
		"ALLOWED_EMAIL_DOMAINS":   "student.spbu.ru",
		"ALLOWED_EMAIL_ADDRESSES": "DormHub.Connection@gmail.com",
		"ADMIN_EMAILS":            "dormhub.connection@gmail.com",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		email          string
		allowed, admin bool
	}{
		{"st123456@student.spbu.ru", true, false},
		{"dormhub.connection@gmail.com", true, true},
		{"other@gmail.com", false, false},
		{"dormhub.connection+other@gmail.com", false, false},
		{"dormhubconnection@gmail.com", false, false},
		{"dormhub.connection@gmail.com.evil.test", false, false},
		{"st123456@sub.student.spbu.ru", false, false},
	} {
		if c.AllowsEmail(test.email) != test.allowed {
			t.Errorf("access for %q", test.email)
		}
		if c.IsAdmin(test.email) != test.admin {
			t.Errorf("admin for %q", test.email)
		}
	}
}

func TestExternalEmailExceptionDoesNotGrantAdmin(t *testing.T) {
	c, err := load(settings(map[string]string{
		"ALLOWED_EMAIL_ADDRESSES": "guest@gmail.com",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.AllowsEmail("guest@gmail.com") || c.IsAdmin("guest@gmail.com") {
		t.Fatal("exception must only grant access")
	}
}

func TestInvalidEmailException(t *testing.T) {
	for _, value := range []string{"gmail.com", "@gmail.com", "Owner <owner@gmail.com>", "owner@gmail.com@evil.test"} {
		_, err := load(settings(map[string]string{"ALLOWED_EMAIL_ADDRESSES": value}))
		if err == nil || !strings.Contains(err.Error(), "ALLOWED_EMAIL_ADDRESSES") {
			t.Fatalf("invalid exception %q accepted: %v", value, err)
		}
	}
	_, err := load(settings(map[string]string{"ADMIN_EMAILS": "owner@gmail.com"}))
	if err == nil {
		t.Fatal("admin outside allowed domains needs an explicit exception")
	}
}

func TestProductionRejectsDevelopmentSecret(t *testing.T) {
	_, err := load(settings(map[string]string{
		"APP_ENV": "production", "BASE_URL": "https://dormhub.example",
		"SMTP_STARTTLS": "true", "AUTH_SECRET": "dorm-hub-local-development-secret-change-before-production",
	}))
	if err == nil {
		t.Fatal("development secret accepted in production")
	}
}
func TestUploadQuotas(t *testing.T) {
	c, err := load(settings(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.UploadQuotaBytes != 200*1024*1024 || c.UploadPhotoLimit != 200 {
		t.Fatal("unexpected upload defaults")
	}
	for _, entry := range []struct{ key, value string }{{"UPLOAD_QUOTA_MB", "0"}, {"UPLOAD_QUOTA_MB", "10001"}, {"UPLOAD_PHOTO_LIMIT", "-1"}, {"UPLOAD_PHOTO_LIMIT", "bad"}} {
		if _, err := load(settings(map[string]string{entry.key: entry.value})); err == nil {
			t.Fatalf("invalid %s accepted", entry.key)
		}
	}
}

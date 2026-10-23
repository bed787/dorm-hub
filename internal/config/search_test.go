package config

import "testing"

func TestMeilisearchConfiguration(t *testing.T) {
	for _, tc := range []struct {
		url, key string
		valid    bool
	}{
		{"", "", true}, {"http://meilisearch:7700", "01234567890123456789012345678901", true},
		{"http://meilisearch:7700", "short", false}, {"http://user:password@meilisearch:7700", "01234567890123456789012345678901", false},
		{"http://meilisearch:7700/path", "01234567890123456789012345678901", false},
	} {
		_, err := load(func(k string) (string, bool) {
			switch k {
			case "MEILI_URL":
				return tc.url, true
			case "MEILI_MASTER_KEY":
				return tc.key, true
			}
			return "", false
		})
		if (err == nil) != tc.valid {
			t.Errorf("URL %q: %v", tc.url, err)
		}
	}
}

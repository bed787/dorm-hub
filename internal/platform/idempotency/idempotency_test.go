package idempotency

import (
	"strings"
	"testing"
)

func TestKeysAndHashes(t *testing.T) {
	for _, key := range []string{"", "short", strings.Repeat("a", 129), "invalid key with spaces"} {
		if Validate(key) == nil {
			t.Fatalf("invalid key accepted: %q", key)
		}
	}
	if err := Validate(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if Hash([]byte("photo")) != Hash([]byte("photo")) || Hash([]byte("photo")) == Hash([]byte("other")) {
		t.Fatal("request hashes do not identify their input")
	}
}

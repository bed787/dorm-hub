package users

import "testing"

func TestContactValidation(t *testing.T) {
	for _, test := range []struct {
		telegram, vk string
		valid        bool
	}{{"https://t.me/student", "", true}, {"", "https://vk.com/student", true}, {"", "", false}, {"javascript:alert(1)", "", false}, {"https://t.me.evil.test/student", "", false}, {"https://t.me@evil.test/student", "", false}, {"http://t.me/student", "", false}, {"https://t.me/", "", false}, {"https://t.me:443/student", "", false}} {
		err := ValidateContacts(test.telegram, test.vk, true)
		if (err == nil) != test.valid {
			t.Errorf("contacts %q %q: %v", test.telegram, test.vk, err)
		}
	}
}

func TestReservedNickname(t *testing.T) {
	for _, test := range []struct {
		nickname string
		reserved bool
	}{
		{"Студент_2", true},
		{"сТуДеНт_002", true},
		{"СТУДЕНТ_9223372036854775807", true},
		{"Студент_", false},
		{"Студент_Ваня", false},
		{"Студент_2_вещи", false},
		{"Алиса", false},
	} {
		if got := reservedNickname(test.nickname); got != test.reserved {
			t.Errorf("reservedNickname(%q) = %v, want %v", test.nickname, got, test.reserved)
		}
	}
}

func TestContactNormalization(t *testing.T) {
	for _, tt := range []struct{ input, host, want string }{
		{"@student", "t.me", "https://t.me/student"}, {"t.me/student", "t.me", "https://t.me/student"}, {"student", "vk.com", "https://vk.com/student"}, {"https://vk.ru/id123", "vk.com", "https://vk.ru/id123"}, {"javascript:alert(1)", "t.me", "javascript:alert(1)"},
	} {
		if got := NormalizeContact(tt.input, tt.host); got != tt.want {
			t.Errorf("%q: got %q", tt.input, got)
		}
	}
}

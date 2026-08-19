package apikey

import "testing"

func TestKeyInfo_HasScope(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		scope  string
		want   bool
	}{
		{"exact match", []string{"read", "write"}, "write", true},
		{"no match", []string{"read"}, "write", false},
		{"wildcard grants everything", []string{"*"}, "anything", true},
		{"empty scopes deny", []string{}, "read", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := &KeyInfo{Scopes: tc.scopes}
			if got := k.HasScope(tc.scope); got != tc.want {
				t.Errorf("HasScope(%q) = %v, want %v", tc.scope, got, tc.want)
			}
		})
	}
}

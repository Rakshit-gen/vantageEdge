package models

import (
	"reflect"
	"testing"
)

// TestStringArray_RoundTrip is the regression test for the previous
// hand-rolled Postgres array encoder/decoder, which corrupted any element
// containing a comma, brace, or quote. A scope like "read,write" (a
// single value someone might reasonably enter) would come back as two
// separate scopes after a Value()/Scan() round trip.
func TestStringArray_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   StringArray
	}{
		{"empty", StringArray{}},
		{"simple values", StringArray{"read", "write"}},
		{"value containing a comma", StringArray{"read,write", "admin"}},
		{"value containing braces", StringArray{"{weird}", "normal"}},
		{"value containing a quote", StringArray{`has "quotes" inside`}},
		{"value containing a backslash", StringArray{`back\slash`}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driverValue, err := tc.in.Value()
			if err != nil {
				t.Fatalf("Value() error: %v", err)
			}

			// Simulate what lib/pq hands back from a real query: the
			// Postgres array literal as bytes.
			var raw []byte
			switch v := driverValue.(type) {
			case []byte:
				raw = v
			case string:
				raw = []byte(v)
			default:
				t.Fatalf("unexpected driver.Value type %T", driverValue)
			}

			var out StringArray
			if err := out.Scan(raw); err != nil {
				t.Fatalf("Scan() error: %v", err)
			}

			if !reflect.DeepEqual([]string(out), []string(tc.in)) {
				t.Errorf("round trip mismatch: got %#v, want %#v", []string(out), []string(tc.in))
			}
		})
	}
}

func TestStringArray_ScanNil(t *testing.T) {
	var out StringArray
	if err := out.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty array for nil scan, got %#v", out)
	}
}

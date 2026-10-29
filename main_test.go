package main

import "testing"

func TestGroupTranscriptionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    bool
		invalid bool
	}{
		{"", true, false}, {"true", true, false}, {"false", false, false}, {" false ", false, false}, {"invalid", false, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("TRANSCRIBE_GROUPS", tc.value)
			enabled, err := groupTranscriptionEnabled()
			if (err != nil) != tc.invalid || enabled != tc.want {
				t.Fatalf("enabled=%v err=%v; want enabled=%v invalid=%v", enabled, err, tc.want, tc.invalid)
			}
		})
	}
}

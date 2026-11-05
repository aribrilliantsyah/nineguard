package upstream

import "testing"

func TestCleanBucketName(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		displayName string
		want        string
	}{
		{name: "gemini session without display name", key: "gemini_session", displayName: "", want: "Gemini (5h)"},
		{name: "gemini weekly without display name", key: "gemini_weekly", displayName: "", want: "Gemini (Weekly)"},
		{name: "claude gpt session without display name", key: "claude_gpt_session", displayName: "", want: "Claude & GPT (5h)"},
		{name: "claude gpt weekly without display name", key: "claude_gpt_weekly", displayName: "", want: "Claude & GPT (Weekly)"},
		{name: "five_hour unchanged", key: "five_hour", displayName: "", want: "5h Session"},
		{name: "session unchanged", key: "session", displayName: "", want: "5h Session"},
		{name: "seven_day unchanged", key: "seven_day", displayName: "", want: "Weekly Limit"},
		{name: "weekly unchanged", key: "weekly", displayName: "", want: "Weekly Limit"},
		{name: "displayName overrides antigravity key", key: "gemini_session", displayName: "Custom Gemini", want: "Custom Gemini"},
		{name: "displayName overrides session key", key: "five_hour", displayName: "Custom Session", want: "Custom Session"},
		{name: "displayName overrides default key", key: "custom_key", displayName: "Explicit Name", want: "Explicit Name"},
		{name: "whitespace displayName ignored", key: "gemini_session", displayName: "   ", want: "Gemini (5h)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanBucketName(tc.key, tc.displayName)
			if got != tc.want {
				t.Errorf("cleanBucketName(%q, %q) = %q; want %q", tc.key, tc.displayName, got, tc.want)
			}
		})
	}
}

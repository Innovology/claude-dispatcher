package appearance

import "testing"

func TestParsePortal(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want Appearance
	}{
		{"v u 1\n", Dark},
		{"v u 2\n", Light},
		{"v u 0\n", Unknown}, // no preference is not a vote for light
		{"(<uint32 1>,)\n", Dark},
		{"(<uint32 2>,)\n", Light},
		{"", Unknown},
	} {
		if got := parsePortal(tc.out); got != tc.want {
			t.Errorf("parsePortal(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}

func TestParseRegistry(t *testing.T) {
	const head = "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize\r\n"
	if got := parseRegistry(head + "    AppsUseLightTheme    REG_DWORD    0x1\r\n"); got != Light {
		t.Errorf("0x1 = %v, want light", got)
	}
	if got := parseRegistry(head + "    AppsUseLightTheme    REG_DWORD    0x0\r\n"); got != Dark {
		t.Errorf("0x0 = %v, want dark", got)
	}
	if got := parseRegistry("ERROR: The system was unable to find the specified registry key"); got != Unknown {
		t.Errorf("error output = %v, want unknown", got)
	}
}

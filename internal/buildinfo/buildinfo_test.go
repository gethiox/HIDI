package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123"},
		{Key: "vcs.modified", Value: "true"},
	}}

	tests := []struct {
		name    string
		version string
		bi      *debug.BuildInfo
		want    string
	}{
		{"release with vcs", "2.0.0", bi, "hidi 2.0.0 (0123456789ab+dirty) "},
		{"development build", "", bi, "hidi dev (0123456789ab+dirty) "},
		{"no build info", "", nil, "hidi dev go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fromBuildInfo(tt.version, tt.bi).String()
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("String() = %q, want prefix %q", got, tt.want)
			}
		})
	}
}

func TestGetNeverEmpty(t *testing.T) {
	info := Get()
	if info.Version == "" || info.GoVersion == "" || info.OS == "" || info.Arch == "" {
		t.Errorf("Get() left fields empty: %+v", info)
	}
}

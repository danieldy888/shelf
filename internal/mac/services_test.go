//go:build darwin

package mac

import (
	"slices"
	"testing"
)

// 服务请求的分流规则是纯逻辑：有可用文件路径时只收文件，否则收文字；
// 两个都是空时什么都不收。
func TestServiceContent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		paths     []string
		text      string
		wantPaths []string
		wantText  string
	}{
		{"files beat text", []string{"/tmp/a.txt"}, "hello", []string{"/tmp/a.txt"}, ""},
		{"only files", []string{"/a", "/b"}, "", []string{"/a", "/b"}, ""},
		{"empty paths fall to text", []string{"", ""}, "hello", nil, "hello"},
		{"only text", nil, "hello", nil, "hello"},
		{"nothing", nil, "", nil, ""},
		{"empty strings", []string{""}, "", nil, ""},
	} {
		paths, text := serviceContent(tc.paths, tc.text)
		if !slices.Equal(paths, tc.wantPaths) || text != tc.wantText {
			t.Errorf("%s: serviceContent(%q, %q) = (%q, %q), want (%q, %q)",
				tc.name, tc.paths, tc.text, paths, text, tc.wantPaths, tc.wantText)
		}
	}
}

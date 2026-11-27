package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"2.0.1", "2.0.0", true},
		{"2.0.0", "2.0.0", false},
		{"2.0.0", "2.0.1", false},
		{"2.1.0", "2.0.9", true},
		{"3.0.0", "2.9.9", true},
		{"2.0.10", "2.0.9", true},
		{"2.0.0", "2.0.0-beta.1", true},
		{"2.0.0-beta.1", "2.0.0", false},
		{"2.0.0-beta.2", "2.0.0-beta.1", true},
		{"2.0.0-beta.10", "2.0.0-beta.9", true},
		{"2.0.0-rc.1", "2.0.0-beta.3", true},
		{"2.0.0-beta.1.1", "2.0.0-beta.1", true},
		{"2.0.0-beta", "2.0.0-1", true},
		{"v2.0.1", "2.0.0", true},
		{"2.0.1", "dev", false},
		{"dev", "2.0.0", false},
		{"2.0", "1.0.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestCheck(t *testing.T) {
	answer, status := `{"tag_name":"v2.1.0","html_url":"https://github.com/gethiox/HIDI/releases/tag/v2.1.0"}`, 200
	var agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	defer server.Close()
	ctx := context.Background()

	got, err := Check(ctx, server.Client(), server.URL, "2.0.0")
	if err != nil || !got.Newer || got.Newest != "2.1.0" || got.Page != "https://github.com/gethiox/HIDI/releases/tag/v2.1.0" {
		t.Errorf("a newer release: %+v, %v", got, err)
	}
	if agent != "HIDI/2.0.0" {
		t.Errorf("asked as %q", agent)
	}
	if got, err := Check(ctx, server.Client(), server.URL, "2.1.0"); err != nil || got.Newer {
		t.Errorf("the same release: %+v, %v", got, err)
	}
	// A HIDI built from source has nothing to compare.
	if got, err := Check(ctx, server.Client(), server.URL, "dev"); err != nil || got.Newer || got.Newest != "2.1.0" {
		t.Errorf("a development build: %+v, %v", got, err)
	}
	// Only pages of the project are passed on.
	answer = `{"tag_name":"v2.1.0","html_url":"https://example.com/elsewhere"}`
	if got, _ := Check(ctx, server.Client(), server.URL, "2.0.0"); got.Page != Project+"/releases" {
		t.Errorf("a page elsewhere is passed on: %q", got.Page)
	}
	answer = `{"tag_name":"nightly"}`
	if _, err := Check(ctx, server.Client(), server.URL, "2.0.0"); err == nil {
		t.Error("a release that is no version is accepted")
	}
	answer, status = `{"message":"rate limit"}`, 403
	if _, err := Check(ctx, server.Client(), server.URL, "2.0.0"); err == nil {
		t.Error("a refusal is taken for an answer")
	}
}

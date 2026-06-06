package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDetectBackendPlainExtended(t *testing.T) {
	typ, info := detectBackend("SubConverter-Extended v1.1.21-f1f875a backend\n")

	if typ != "SubConverter-Extended" {
		t.Fatalf("type = %q, want SubConverter-Extended", typ)
	}
	if info.version != "v1.1.21" {
		t.Errorf("version = %q, want v1.1.21", info.version)
	}
	if info.build != "f1f875a" {
		t.Errorf("build = %q, want f1f875a", info.build)
	}
}

func TestParseExtendedInfoFromVersionPages(t *testing.T) {
	tests := []struct {
		name string
		html string
	}{
		{
			name: "current bilingual page",
			html: `<title>SubConverter-Extended</title>
<span class="info-label">
  <span data-lang="en">Version</span>
  <span data-lang="zh">版本</span>
</span>
<div class="info-value">v1.1.21</div>
<span class="info-label">
  <span data-lang="en">Build</span>
  <span data-lang="zh">构建</span>
</span>
<div class="info-value"><a href="/commit/f1f875a">f1f875a</a></div>
<span class="info-label">
  <span data-lang="en">Build Date</span>
  <span data-lang="zh">构建日期</span>
</span>
<div class="info-value">2026-06-06</div>`,
		},
		{
			name: "legacy page",
			html: `<title>SubConverter-Extended</title>
<span class="info-label">Version</span><div class="info-value">v1.1.21</div>
<span class="info-label">Build</span><div class="info-value">f1f875a</div>
<span class="info-label">Build Date</span><div class="info-value">2026-06-06</div>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := parseExtendedInfo(tt.html)
			if !ok {
				t.Fatal("parseExtendedInfo returned false")
			}
			if info.version != "v1.1.21" || info.build != "f1f875a" || info.buildDate != "2026-06-06" {
				t.Fatalf("unexpected info: %+v", info)
			}
		})
	}
}

func TestFetchBackendInfoRequestsLightweightVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Origin"); got != "https://tg-backend-bot.invalid" {
			t.Errorf("Origin = %q", got)
		}
		if got := r.Header.Get("Sec-Fetch-Mode"); got != "cors" {
			t.Errorf("Sec-Fetch-Mode = %q", got)
		}
		if got := r.Header.Get("Sec-Fetch-Dest"); got != "empty" {
			t.Errorf("Sec-Fetch-Dest = %q", got)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("SubConverter-Extended dev-ec7afdf backend\n"))
	}))
	defer server.Close()

	result := fetchBackendInfo(server.Client(), server.URL)
	if !result.ok || result.typ != "SubConverter-Extended" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.info.version != "dev" || result.info.build != "ec7afdf" {
		t.Fatalf("unexpected info: %+v", result.info)
	}
}

func TestLiveExtendedVersionProbe(t *testing.T) {
	targetURL := os.Getenv("SCE_TEST_URL")
	if targetURL == "" {
		t.Skip("SCE_TEST_URL is not set")
	}

	result := fetchBackendInfo(newHTTPClient(), targetURL)
	if !result.ok || result.typ != "SubConverter-Extended" {
		t.Fatalf("unexpected live result: %+v", result)
	}
	if result.info.version == "" {
		t.Fatalf("live response did not contain a version: %+v", result.info)
	}

	response, err := http.Get(targetURL)
	if err != nil {
		t.Fatalf("fetch full live version page: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, backendBodyLimit))
	if err != nil {
		t.Fatalf("read full live version page: %v", err)
	}
	if info, ok := parseExtendedInfo(string(body)); !ok || info.version == "" {
		t.Fatalf("full live version page was not parsed: ok=%v info=%+v", ok, info)
	}
}

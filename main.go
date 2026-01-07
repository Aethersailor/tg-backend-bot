package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "regexp"
    "strings"
    "sync"
    "time"
)

const (
    defaultBackend    = "api.asailor.org"
    maxBackends       = 20
    maxConcurrency    = 5
    requestTimeout    = 10 * time.Second
    pollTimeout       = 30 * time.Second
    backendBodyLimit  = 128 * 1024
    updatesBodyLimit  = 1 * 1024 * 1024
)

var (
    versionPattern = regexp.MustCompile(`^subconverter\s+v[\d.]+-[\w]+ backend$`)
    extendedMarker = regexp.MustCompile(`(?i)SubConverter-Extended`)
    infoCardPattern = regexp.MustCompile(
        `(?is)<span class="info-label">\s*(Version|Build|Build Date)\s*</span>\s*<div class="info-value">(.*?)</div>`,
    )
    tagPattern        = regexp.MustCompile(`(?s)<[^>]+>`)
    whitespacePattern = regexp.MustCompile(`\s+`)
    schemePattern     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
)

type backendTarget struct {
    display string
    url     string
}

type backendInfo struct {
    version   string
    build     string
    buildDate string
    snippet   string
}

type backendResult struct {
    ok     bool
    status int
    err    string
    typ    string
    info   backendInfo
}

type updateResponse struct {
    Ok     bool     `json:"ok"`
    Result []update `json:"result"`
}

type update struct {
    UpdateID int      `json:"update_id"`
    Message  *message `json:"message"`
}

type message struct {
    Chat chat  `json:"chat"`
    Text string `json:"text"`
    From *user  `json:"from"`
}

type chat struct {
    ID int64 `json:"id"`
}

type user struct {
    ID int64 `json:"id"`
}

type sendMessageRequest struct {
    ChatID                int64  `json:"chat_id"`
    Text                  string `json:"text"`
    DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

func main() {
    if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
        if err := runHealthcheck(); err != nil {
            log.Printf("healthcheck failed: %v", err)
            os.Exit(1)
        }
        return
    }

    token := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
    if token == "" {
        log.Fatal("BOT_TOKEN is not set")
    }

    client := newHTTPClient()
    offset := 0

    for {
        updates, err := getUpdates(client, token, offset)
        if err != nil {
            log.Printf("getUpdates error: %v", err)
            time.Sleep(2 * time.Second)
            continue
        }

        for _, item := range updates {
            if item.UpdateID >= offset {
                offset = item.UpdateID + 1
            }
            if item.Message == nil {
                continue
            }
            if !isBackendCommand(item.Message.Text) {
                continue
            }

            reply := buildStatusMessage(client)
            if err := sendMessage(client, token, item.Message.Chat.ID, reply); err != nil {
                log.Printf("sendMessage error: %v", err)
            }
        }
    }
}

func runHealthcheck() error {
    targets, _ := loadBackendTargets()
    if len(targets) == 0 {
        return errors.New("no backend targets configured")
    }

    client := newHTTPClient()
    result := fetchBackendInfo(client, targets[0].url)
    if !result.ok {
        return fmt.Errorf("backend offline: %s", result.err)
    }
    return nil
}

func newHTTPClient() *http.Client {
    transport := &http.Transport{
        Proxy:               http.ProxyFromEnvironment,
        MaxIdleConns:        20,
        MaxIdleConnsPerHost: maxConcurrency,
        IdleConnTimeout:     30 * time.Second,
        TLSHandshakeTimeout: 10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
    }

    return &http.Client{Transport: transport}
}

func getUpdates(client *http.Client, token string, offset int) ([]update, error) {
    endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=%d&offset=%d&allowed_updates=message", token, int(pollTimeout.Seconds()), offset)
    ctx, cancel := context.WithTimeout(context.Background(), pollTimeout+5*time.Second)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
    if err != nil {
        return nil, err
    }

    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
        return nil, fmt.Errorf("getUpdates status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
    }

    data, err := io.ReadAll(io.LimitReader(resp.Body, updatesBodyLimit))
    if err != nil {
        return nil, err
    }

    var decoded updateResponse
    if err := json.Unmarshal(data, &decoded); err != nil {
        return nil, err
    }
    if !decoded.Ok {
        return nil, errors.New("telegram api returned ok=false")
    }

    return decoded.Result, nil
}

func sendMessage(client *http.Client, token string, chatID int64, text string) error {
    payload := sendMessageRequest{
        ChatID:                chatID,
        Text:                  text,
        DisableWebPagePreview: true,
    }
    body, err := json.Marshal(payload)
    if err != nil {
        return err
    }

    endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
    ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
        return fmt.Errorf("sendMessage status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
    }

    return nil
}

func isBackendCommand(text string) bool {
    trimmed := strings.TrimSpace(text)
    return trimmed == "/backend" || trimmed == "/后端状态" || trimmed == "后端状态"
}

func buildStatusMessage(client *http.Client) string {
    targets, truncated := loadBackendTargets()
    if len(targets) == 0 {
        return "未配置后端地址，请设置 BACKEND_URLS 环境变量。"
    }

    results := checkBackends(client, targets)
    blocks := make([]string, 0, len(results))
    onlineCount := 0

    for i, result := range results {
        if result.ok {
            onlineCount++
        }
        blocks = append(blocks, formatBackendBlock(i+1, targets[i].display, result))
    }

    offlineCount := len(results) - onlineCount
    title := fmt.Sprintf("📡 后端状态 (%d) ✅ %d / ❌ %d", len(results), onlineCount, offlineCount)
    if truncated {
        title += fmt.Sprintf(" - 仅显示前 %d 个", maxBackends)
    }

    return title + "\n\n" + strings.Join(blocks, "\n\n")
}

func checkBackends(client *http.Client, targets []backendTarget) []backendResult {
    results := make([]backendResult, len(targets))
    sem := make(chan struct{}, maxConcurrency)
    var wg sync.WaitGroup

    for i, target := range targets {
        wg.Add(1)
        go func(idx int, url string) {
            defer wg.Done()
            sem <- struct{}{}
            results[idx] = fetchBackendInfo(client, url)
            <-sem
        }(i, target.url)
    }

    wg.Wait()
    return results
}

func fetchBackendInfo(client *http.Client, targetURL string) backendResult {
    ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
    defer cancel()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
    if err != nil {
        return backendResult{ok: false, err: "request_error"}
    }
    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
    req.Header.Set("Accept", "text/plain,text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

    resp, err := client.Do(req)
    if err != nil {
        return backendResult{ok: false, err: classifyError(err)}
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(io.LimitReader(resp.Body, backendBodyLimit))
    if err != nil {
        return backendResult{ok: false, err: "read_error"}
    }

    if resp.StatusCode != http.StatusOK {
        return backendResult{ok: false, status: resp.StatusCode, err: fmt.Sprintf("HTTP %d", resp.StatusCode)}
    }

    text := strings.TrimSpace(string(body))
    typ, info := detectBackend(text)

    return backendResult{ok: true, status: resp.StatusCode, typ: typ, info: info}
}

func classifyError(err error) string {
    if errors.Is(err, context.DeadlineExceeded) {
        return "timeout"
    }

    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        return "timeout"
    }

    return "connection_error"
}

func detectBackend(text string) (string, backendInfo) {
    if info, ok := parseExtendedInfo(text); ok {
        return "SubConverter-Extended", info
    }

    trimmed := strings.TrimSpace(text)
    if versionPattern.MatchString(trimmed) || strings.Contains(strings.ToLower(trimmed), "subconverter") {
        return "subconverter", backendInfo{version: trimmed}
    }

    return "unknown", backendInfo{snippet: compactSnippet(trimmed, 200)}
}

func parseExtendedInfo(text string) (backendInfo, bool) {
    if !extendedMarker.MatchString(text) {
        return backendInfo{}, false
    }

    matches := infoCardPattern.FindAllStringSubmatch(text, -1)
    if len(matches) == 0 {
        return backendInfo{}, false
    }

    info := backendInfo{}
    for _, match := range matches {
        label := strings.ToLower(strings.TrimSpace(match[1]))
        value := stripHTML(match[2])

        switch label {
        case "version":
            info.version = value
        case "build":
            info.build = value
        case "build date":
            info.buildDate = value
        }
    }

    if info.version == "" && info.build == "" && info.buildDate == "" {
        return backendInfo{}, false
    }

    return info, true
}

func stripHTML(value string) string {
    return strings.TrimSpace(tagPattern.ReplaceAllString(value, ""))
}

func compactSnippet(text string, limit int) string {
    text = whitespacePattern.ReplaceAllString(text, " ")
    if len(text) > limit {
        return text[:limit] + "..."
    }
    return text
}

func formatBackendBlock(index int, display string, result backendResult) string {
    lines := []string{fmt.Sprintf("🔗 [%d] %s", index, display)}

    if !result.ok {
        lines = append(lines, "类型: ❓ 未知")
        lines = append(lines, "状态: ❌ 离线")
        if result.err != "" {
            lines = append(lines, fmt.Sprintf("错误: ⚠️ %s", result.err))
        }
        return strings.Join(lines, "\n")
    }

    switch result.typ {
    case "SubConverter-Extended":
        lines = append(lines, "类型: ✨ SubConverter-Extended")
    case "subconverter":
        lines = append(lines, "类型: 🧩 subconverter")
    default:
        lines = append(lines, "类型: ❓ unknown")
    }

    lines = append(lines, "状态: ✅ 在线")

    if result.typ == "SubConverter-Extended" {
        if result.info.version != "" {
            lines = append(lines, fmt.Sprintf("🔖 Version: %s", result.info.version))
        }
        if result.info.build != "" {
            lines = append(lines, fmt.Sprintf("🧱 Build: %s", result.info.build))
        }
        if result.info.buildDate != "" {
            lines = append(lines, fmt.Sprintf("📅 Build Date: %s", result.info.buildDate))
        }
    } else if result.typ == "subconverter" {
        if result.info.version != "" {
            lines = append(lines, fmt.Sprintf("🔖 版本: %s", result.info.version))
        }
    } else if result.info.snippet != "" {
        lines = append(lines, fmt.Sprintf("📝 内容: %s", result.info.snippet))
    }

    return strings.Join(lines, "\n")
}

func loadBackendTargets() ([]backendTarget, bool) {
    raw := strings.TrimSpace(os.Getenv("BACKEND_URLS"))
    if raw == "" {
        raw = strings.TrimSpace(os.Getenv("BACKEND_URL"))
    }
    if raw == "" {
        raw = defaultBackend
    }

    items := parseBackendList(raw)
    truncated := len(items) > maxBackends
    if len(items) > maxBackends {
        items = items[:maxBackends]
    }

    targets := make([]backendTarget, 0, len(items))
    for _, item := range items {
        display, urlValue := normalizeBackendTarget(item)
        if display == "" || urlValue == "" {
            continue
        }
        targets = append(targets, backendTarget{display: display, url: urlValue})
    }

    return targets, truncated
}

func parseBackendList(value string) []string {
    if value == "" {
        return nil
    }

    return strings.FieldsFunc(value, func(r rune) bool {
        switch r {
        case ',', ' ', '\t', '\n', '\r':
            return true
        default:
            return false
        }
    })
}

func normalizeBackendTarget(raw string) (string, string) {
    trimmed := strings.TrimSpace(raw)
    if trimmed == "" {
        return "", ""
    }

    input := trimmed
    if !schemePattern.MatchString(input) {
        input = "https://" + input
    }

    parsed, err := url.Parse(input)
    if err != nil {
        return trimmed, ""
    }

    if parsed.Host == "" {
        parsed.Host = parsed.Path
        parsed.Path = ""
    }

    path := parsed.Path
    if path == "" || path == "/" {
        path = "/version"
    } else if strings.TrimSuffix(path, "/") == "/version" {
        path = "/version"
    } else {
        path = strings.TrimSuffix(path, "/") + "/version"
    }

    parsed.Path = path
    parsed.RawQuery = ""
    parsed.Fragment = ""

    return trimmed, parsed.String()
}

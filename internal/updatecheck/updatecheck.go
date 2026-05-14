package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/5nYqnHvk/RelayCode/internal/config"
	"github.com/5nYqnHvk/RelayCode/internal/version"
)

func MaybeNotify(parent context.Context, cfg config.ServerConfig) {
	if !shouldCheck(cfg, version.Version) {
		return
	}
	timeout := updateTimeout(cfg)
	current := version.Version
	go func() {
		ctx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		notify(ctx, cfg.UpdateCheckURL, current)
	}()
}

func shouldCheck(cfg config.ServerConfig, current string) bool {
	return cfg.EnableUpdateNotification && current != "dev" && strings.TrimSpace(cfg.UpdateCheckURL) != ""
}

func updateTimeout(cfg config.ServerConfig) time.Duration {
	timeout := time.Duration(cfg.UpdateCheckTimeoutSeconds) * time.Second
	if timeout <= 0 {
		return 3 * time.Second
	}
	return timeout
}

func notify(ctx context.Context, url, current string) {
	latest, err := fetchLatestTag(ctx, url)
	if err != nil || !isNewer(latest, current) {
		return
	}
	log.Printf("update available: current=%s latest=%s https://github.com/5nYqnHvk/RelayCode/releases/tag/%s", current, latest, latest)
}

func fetchLatestTag(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "RelayCodeUpdateCheck")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("update check status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if strings.TrimSpace(body.TagName) == "" {
		return "", fmt.Errorf("update check missing tag_name")
	}
	return body.TagName, nil
}

func isNewer(latest, current string) bool {
	lv, ok := parseVersionTag(latest)
	if !ok {
		return false
	}
	cv, ok := parseVersionTag(current)
	if !ok {
		return false
	}
	for i := range lv {
		if lv[i] > cv[i] {
			return true
		}
		if lv[i] < cv[i] {
			return false
		}
	}
	return false
}

func parseVersionTag(tag string) ([3]int, bool) {
	var out [3]int
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "v")
	parts := strings.Split(tag, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		if part == "" || strings.ContainsAny(part, "-+") {
			return out, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

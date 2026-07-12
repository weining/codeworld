//go:build darwin

package codex

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

func platformHTTPClient() *http.Client {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return http.DefaultClient
		}
	}
	proxyURL := darwinHTTPSProxy()
	if proxyURL == nil {
		return http.DefaultClient
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultClient
	}
	cloned := transport.Clone()
	environmentProxy := cloned.Proxy
	cloned.Proxy = func(request *http.Request) (*url.URL, error) {
		if environmentProxy != nil {
			configured, err := environmentProxy(request)
			if configured != nil || err != nil {
				return configured, err
			}
		}
		return proxyURL, nil
	}
	return &http.Client{Transport: cloned}
}

func darwinHTTPSProxy() *url.URL {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return nil
	}
	return parseDarwinHTTPSProxy(string(output))
}

func parseDarwinHTTPSProxy(output string) *url.URL {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if values["HTTPSEnable"] != "1" || values["HTTPSProxy"] == "" || values["HTTPSPort"] == "" {
		return nil
	}
	parsed, err := url.Parse("http://" + values["HTTPSProxy"] + ":" + values["HTTPSPort"])
	if err != nil {
		return nil
	}
	return parsed
}

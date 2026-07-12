//go:build !darwin

package codex

import "net/http"

func platformHTTPClient() *http.Client {
	return http.DefaultClient
}

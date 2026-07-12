//go:build darwin

package codex

import "testing"

func TestParseDarwinHTTPSProxy(t *testing.T) {
	proxy := parseDarwinHTTPSProxy(`<dictionary> {
  HTTPSEnable : 1
  HTTPSPort : 7897
  HTTPSProxy : 127.0.0.1
}`)
	if proxy == nil || proxy.String() != "http://127.0.0.1:7897" {
		t.Fatalf("proxy = %v", proxy)
	}
	if proxy := parseDarwinHTTPSProxy("HTTPSEnable : 0\n"); proxy != nil {
		t.Fatalf("disabled proxy = %v", proxy)
	}
}

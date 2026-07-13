package codexauth

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

type LoginOptions struct {
	Root       string
	In         io.Reader
	Out        io.Writer
	Random     io.Reader
	OAuth      OAuthClient
	OpenURL    func(string) error
	Originator string
	Store      Store
}

// Login 执行 OpenClaw 风格 Codex OAuth browser flow，并保存凭据。
func Login(ctx context.Context, opts LoginOptions) (Credentials, error) {
	flow, err := NewFlow(opts.Random)
	if err != nil {
		return Credentials{}, err
	}
	originator := opts.Originator
	if originator == "" {
		originator = "codeworld"
	}
	authURL, err := flow.AuthorizeURL(originator)
	if err != nil {
		return Credentials{}, err
	}
	server := startCallbackServer(flow.State)
	defer server.Close()

	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	fmt.Fprintln(out, "Open this URL to authorize Codeworld with Codex OAuth:")
	fmt.Fprintln(out, authURL)
	fmt.Fprintln(out, "Waiting for browser callback on http://localhost:1455/auth/callback")
	fmt.Fprintln(out, "If the callback cannot reach this process, paste the redirect URL or authorization code here.")
	if open := opts.OpenURL; open != nil {
		_ = open(authURL)
	} else {
		_ = openBrowser(authURL)
	}

	code, err := waitForCode(ctx, server, opts.In, flow.State)
	if err != nil {
		return Credentials{}, err
	}
	oauth := opts.OAuth
	if oauth.tokenURL == "" {
		oauth = NewOAuthClient(Options{})
	}
	cred, err := oauth.Exchange(ctx, code, flow.Verifier, RedirectURI)
	if err != nil {
		return Credentials{}, err
	}
	store := opts.Store
	if store.Root == "" && store.BaseDir == "" {
		store.Root = opts.Root
	}
	if err := store.Save(cred); err != nil {
		return Credentials{}, err
	}
	fmt.Fprintf(out, "Codex OAuth saved for account %s\n", cred.AccountID)
	return cred, nil
}

type callbackServer struct {
	server *http.Server
	codeCh chan AuthorizationInput
}

// startCallbackServer 启动 localhost callback；端口占用时仍允许手动粘贴 code。
func startCallbackServer(state string) callbackServer {
	codeCh := make(chan AuthorizationInput, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		input := AuthorizationInput{Code: r.URL.Query().Get("code"), State: r.URL.Query().Get("state")}
		if input.State != state {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, oauthPage("Authentication failed", "State mismatch."))
			return
		}
		if input.Code == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, oauthPage("Authentication failed", "Missing authorization code."))
			return
		}
		select {
		case codeCh <- input:
		default:
		}
		_, _ = fmt.Fprint(w, oauthPage("Authentication successful", "OpenAI authentication completed. You can close this window."))
	})
	server := &http.Server{Addr: "localhost:1455", Handler: mux}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return callbackServer{codeCh: codeCh}
	}
	go func() {
		_ = server.Serve(listener)
	}()
	return callbackServer{server: server, codeCh: codeCh}
}

func (s callbackServer) Close() {
	if s.server != nil {
		_ = s.server.Close()
	}
}

// waitForCode 等待 callback 或手动输入，先到者生效。
func waitForCode(ctx context.Context, server callbackServer, in io.Reader, state string) (string, error) {
	manualCh := make(chan AuthorizationInput, 1)
	if in != nil {
		go func() {
			line, _ := bufio.NewReader(in).ReadString('\n')
			manualCh <- ParseAuthorizationInput(line)
		}()
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case input := <-server.codeCh:
			if input.Code != "" {
				return input.Code, nil
			}
		case input := <-manualCh:
			if input.State != "" && input.State != state {
				return "", fmt.Errorf("state mismatch")
			}
			if strings.TrimSpace(input.Code) != "" {
				return strings.TrimSpace(input.Code), nil
			}
		}
	}
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func oauthPage(title, message string) string {
	return "<!doctype html><meta charset=\"utf-8\"><title>" + html.EscapeString(title) + "</title><h1>" + html.EscapeString(title) + "</h1><p>" + html.EscapeString(message) + "</p>"
}

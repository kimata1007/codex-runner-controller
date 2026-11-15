package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	authorizeEndpoint = "https://auth.openai.com/api/accounts/authorize"
	tokenEndpoint     = "https://auth.openai.com/api/accounts/oauth/token"
	resource          = "https://api.openai.com/v1"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
}

func main() {
	if len(os.Args) < 3 || os.Args[1] != "auth" || os.Args[2] != "connect" {
		fmt.Fprintln(os.Stderr, "usage: codex-runner auth connect --server URL --attempt ID --host-id ID")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("auth connect", flag.ExitOnError)
	server := flags.String("server", "", "Codex Runner base URL")
	attempt := flags.String("attempt", "", "one-time sign-in attempt ID")
	hostID := flags.String("host-id", "", "Codex Runner host ID")
	_ = flags.Parse(os.Args[3:])
	if err := connect(context.Background(), *server, *attempt, *hostID); err != nil {
		fmt.Fprintln(os.Stderr, "ChatGPT sign-in failed:", err)
		os.Exit(1)
	}
	fmt.Println("ChatGPT is connected to Codex Runner. You can return to the browser.")
}

func connect(ctx context.Context, server, attemptID, hostID string) error {
	base, err := url.Parse(strings.TrimRight(server, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Hostname() != "localhost" && base.Hostname() != "127.0.0.1") {
		return fmt.Errorf("--server must be an HTTPS URL")
	}
	if attemptID == "" || hostID == "" {
		return fmt.Errorf("attempt and host ID are required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/auth/callback", listener.Addr().(*net.TCPAddr).Port)
	state, err := randomValue(32)
	if err != nil {
		return err
	}
	nonce := attemptID
	verifier, err := randomValue(48)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	callback := make(chan url.Values, 1)
	callbackServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/callback" {
			http.NotFound(w, r)
			return
		}
		select {
		case callback <- r.URL.Query():
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>Codex Runner</title><p>認証結果をCodex Runnerに返しました。このタブを閉じてください。</p>")
	})}
	go func() { _ = callbackServer.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = callbackServer.Shutdown(shutdownCtx)
	}()
	params := url.Values{
		"client_id": {"dynamic_agent_client"}, "agent_name_hint": {"Codex Runner"}, "ext_agent_host_id": {hostID},
		"response_type": {"code"}, "redirect_uri": {redirectURI},
		"scope":    {"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"},
		"resource": {resource}, "state": {state}, "nonce": {nonce},
		"code_challenge_method": {"S256"}, "code_challenge": {challenge},
	}
	authorizeURL := authorizeEndpoint + "?" + params.Encode()
	fmt.Println("Opening ChatGPT sign-in in your browser…")
	if err := openBrowser(authorizeURL); err != nil {
		fmt.Println("Open this URL in your browser:", authorizeURL)
	}
	var result url.Values
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result = <-callback:
	}
	if result.Get("state") != state {
		return fmt.Errorf("OAuth state did not match")
	}
	if oauthError := result.Get("error"); oauthError != "" {
		return fmt.Errorf("ChatGPT authorization was not completed")
	}
	code, clientID := result.Get("code"), result.Get("client_id")
	if code == "" || clientID == "" || clientID == "dynamic_agent_client" {
		return fmt.Errorf("callback did not include an authorization code and issued client ID")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirectURI}, "resource": {resource}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ChatGPT token exchange failed (%s)", response.Status)
	}
	var tokens tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&tokens); err != nil {
		return err
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" {
		return fmt.Errorf("ChatGPT returned an incomplete credential")
	}
	body, _ := json.Marshal(map[string]any{"attemptId": attemptID, "clientId": clientID, "idToken": tokens.IDToken, "accessToken": tokens.AccessToken, "refreshToken": tokens.RefreshToken, "scope": tokens.Scope, "expiresIn": tokens.ExpiresIn, "nonce": nonce})
	importURL := base.ResolveReference(&url.URL{Path: "/v1/auth/import"})
	upload, err := http.NewRequestWithContext(ctx, http.MethodPost, importURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	upload.Header.Set("Content-Type", "application/json")
	resultResponse, err := client.Do(upload)
	if err != nil {
		return err
	}
	defer resultResponse.Body.Close()
	if resultResponse.StatusCode < 200 || resultResponse.StatusCode >= 300 {
		return errors.New("Runner API rejected the ChatGPT sign-in")
	}
	return nil
}

func randomValue(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(address string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{address}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		command, args = "xdg-open", []string{address}
	}
	return exec.Command(command, args...).Start()
}

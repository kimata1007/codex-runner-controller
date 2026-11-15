package authbroker

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kimata1007/codex-runner-controller/internal/agent"
)

const tokenEndpoint = "https://auth.openai.com/api/accounts/oauth/token"

type Credential struct {
	HostID        string    `json:"hostId"`
	ClientID      string    `json:"clientId"`
	TokenEndpoint string    `json:"tokenEndpoint,omitempty"`
	AccountID     string    `json:"accountId"`
	Subject       string    `json:"subject"`
	Email         string    `json:"email,omitempty"`
	PlanType      string    `json:"planType,omitempty"`
	AccessToken   string    `json:"accessToken"`
	RefreshToken  string    `json:"refreshToken"`
	IDToken       string    `json:"idToken"`
	Scopes        []string  `json:"scopes"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type Store interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Delete(context.Context) error
}

type Broker struct {
	store      Store
	key        []byte
	client     *http.Client
	mu         sync.Mutex
	now        func() time.Time
	attempts   map[string]loginAttempt
	loginAgent DeviceLoginAgent
}

type DeviceLoginAgent interface {
	StartDeviceLogin(context.Context) (agent.DeviceLogin, error)
	ReadAccount(context.Context, string) (agent.AccountStatus, error)
	CancelDeviceLogin(context.Context, string) error
	ReadAuthCredentials(context.Context) (json.RawMessage, error)
	Logout(context.Context) error
}

func (b *Broker) WithDeviceLoginAgent(loginAgent DeviceLoginAgent) *Broker {
	b.loginAgent = loginAgent
	return b
}

type loginAttempt struct {
	hostID    string
	createdAt time.Time
}

type DeviceLogin struct {
	LoginID         string `json:"loginId"`
	VerificationURL string `json:"verificationUrl"`
	UserCode        string `json:"userCode"`
}

func New(store Store, encryptionKey []byte, client *http.Client) (*Broker, error) {
	if store == nil || len(encryptionKey) != 32 {
		return nil, fmt.Errorf("auth broker requires a store and a 32-byte encryption key")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Broker{store: store, key: append([]byte(nil), encryptionKey...), client: client, now: time.Now, attempts: map[string]loginAttempt{}}, nil
}

func (b *Broker) BeginLogin(ctx context.Context) (DeviceLogin, error) {
	if b.loginAgent != nil {
		login, err := b.loginAgent.StartDeviceLogin(ctx)
		if err != nil {
			return DeviceLogin{}, err
		}
		b.mu.Lock()
		b.removeExpiredAttempts()
		b.attempts[login.LoginID] = loginAttempt{createdAt: b.now()}
		b.mu.Unlock()
		return DeviceLogin{LoginID: login.LoginID, VerificationURL: login.VerificationURL, UserCode: login.UserCode}, nil
	}
	return DeviceLogin{}, fmt.Errorf("ChatGPT device sign-in agent is unavailable")
}

func (b *Broker) CancelLogin(attemptID string) {
	if b.loginAgent != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = b.loginAgent.CancelDeviceLogin(ctx, attemptID)
	}
	b.mu.Lock()
	delete(b.attempts, attemptID)
	b.mu.Unlock()
}

func (b *Broker) removeExpiredAttempts() {
	for id, attempt := range b.attempts {
		if b.now().Sub(attempt.createdAt) > 10*time.Minute {
			delete(b.attempts, id)
		}
	}
}

func (b *Broker) Status(ctx context.Context) (map[string]any, error) {
	credential, ok, err := b.read(ctx)
	if err != nil {
		return nil, err
	}
	if (!ok || credential.AccessToken == "") && b.loginAgent != nil {
		status, statusErr := b.loginAgent.ReadAccount(ctx, "")
		if statusErr != nil {
			return nil, statusErr
		}
		if status.Account != nil && status.Account.Type == "chatgpt" {
			raw, readErr := b.loginAgent.ReadAuthCredentials(ctx)
			if readErr != nil {
				return nil, readErr
			}
			credential, readErr = credentialFromCodexAuth(raw, status.Account.Email, status.Account.PlanType)
			if readErr != nil {
				return nil, readErr
			}
			if err := b.Save(ctx, credential); err != nil {
				return nil, err
			}
			ok = true
		}
	}
	if !ok || credential.AccessToken == "" {
		return map[string]any{"authenticated": false}, nil
	}
	return map[string]any{"authenticated": true, "account": map[string]string{"type": "chatgpt", "email": credential.Email, "planType": credential.PlanType}}, nil
}

func (b *Broker) GetChatGPTAuthTokens(ctx context.Context) (agent.ChatGPTAuthTokens, error) {
	credential, err := b.refresh(ctx, false, "")
	if err != nil {
		return agent.ChatGPTAuthTokens{}, err
	}
	return toAgentTokens(credential), nil
}

func (b *Broker) RefreshChatGPTAuthTokens(ctx context.Context, accountID string) (agent.ChatGPTAuthTokens, error) {
	credential, err := b.refresh(ctx, true, accountID)
	if err != nil {
		return agent.ChatGPTAuthTokens{}, err
	}
	return toAgentTokens(credential), nil
}

func (b *Broker) Save(ctx context.Context, credential Credential) error {
	if credential.AccessToken == "" || credential.RefreshToken == "" || credential.ClientID == "" || credential.AccountID == "" {
		return fmt.Errorf("auth broker: incomplete ChatGPT credential")
	}
	return b.write(ctx, credential)
}

func (b *Broker) Logout(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	credential, ok, err := b.read(ctx)
	if err != nil {
		return err
	}
	if b.loginAgent != nil {
		if err := b.loginAgent.Logout(ctx); err != nil {
			return err
		}
	}
	if !ok {
		return nil
	}
	hostID := credential.HostID
	return b.write(ctx, Credential{HostID: hostID})
}

func (b *Broker) refresh(ctx context.Context, force bool, expectedAccountID string) (Credential, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	credential, ok, err := b.read(ctx)
	if err != nil {
		return Credential{}, err
	}
	if !ok {
		return Credential{}, fmt.Errorf("ChatGPT is not connected")
	}
	if expectedAccountID != "" && credential.AccountID != expectedAccountID {
		return Credential{}, fmt.Errorf("ChatGPT account changed")
	}
	if !force && credential.ExpiresAt.After(b.now().Add(2*time.Minute)) {
		return credential, nil
	}
	form := url.Values{
		"grant_type": {"refresh_token"}, "client_id": {credential.ClientID},
		"refresh_token": {credential.RefreshToken},
	}
	refreshEndpoint := credential.TokenEndpoint
	if refreshEndpoint == "" {
		refreshEndpoint = tokenEndpoint
		form.Set("resource", "https://api.openai.com/v1")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := b.client.Do(request)
	if err != nil {
		return Credential{}, fmt.Errorf("refresh ChatGPT credentials: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Credential{}, fmt.Errorf("refresh ChatGPT credentials: %s", response.Status)
	}
	var refreshed tokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&refreshed); err != nil {
		return Credential{}, err
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		return Credential{}, fmt.Errorf("refresh response is incomplete")
	}
	accountID, err := chatGPTAccountID(refreshed.AccessToken)
	if err != nil || accountID != credential.AccountID {
		return Credential{}, fmt.Errorf("refreshed ChatGPT token belongs to a different account")
	}
	credential.AccessToken = refreshed.AccessToken
	credential.RefreshToken = refreshed.RefreshToken
	credential.ExpiresAt = b.now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
	if refreshed.IDToken != "" {
		credential.IDToken = refreshed.IDToken
	}
	if refreshed.Scope != "" {
		credential.Scopes = strings.Fields(refreshed.Scope)
	}
	if err := b.write(ctx, credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func credentialFromCodexAuth(raw json.RawMessage, email, planType string) (Credential, error) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return Credential{}, fmt.Errorf("decode Codex credentials: %w", err)
	}
	tokens, _ := document["tokens"].(map[string]any)
	provenance, _ := tokens["oauth_provenance"].(map[string]any)
	access, _ := tokens["access_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	idToken, _ := tokens["id_token"].(string)
	clientID, _ := provenance["client_id"].(string)
	issuer, _ := provenance["issuer"].(string)
	if issuer == "" {
		issuer, _ = document["issuer"].(string)
	}
	if access == "" || refresh == "" || clientID == "" {
		return Credential{}, fmt.Errorf("Codex credentials are incomplete")
	}
	accountID, err := chatGPTAccountID(access)
	if err != nil {
		return Credential{}, err
	}
	endpoint := ""
	if issuer != "" {
		endpoint = strings.TrimRight(issuer, "/") + "/oauth/token"
	}
	return Credential{ClientID: clientID, TokenEndpoint: endpoint, AccountID: accountID, Email: email, PlanType: planType, AccessToken: access, RefreshToken: refresh, IDToken: idToken, ExpiresAt: time.Now().Add(45 * time.Minute)}, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
}

func chatGPTAccountID(accessToken string) (string, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("ChatGPT access token does not contain an account identifier")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode ChatGPT access-token claims: %w", err)
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || strings.TrimSpace(claims.Auth.AccountID) == "" {
		return "", fmt.Errorf("ChatGPT access token does not contain a workspace/account identifier")
	}
	return claims.Auth.AccountID, nil
}

func toAgentTokens(c Credential) agent.ChatGPTAuthTokens {
	return agent.ChatGPTAuthTokens{AccessToken: c.AccessToken, AccountID: c.AccountID, PlanType: c.PlanType}
}

func (b *Broker) read(ctx context.Context) (Credential, bool, error) {
	data, err := b.store.Read(ctx)
	if err != nil {
		return Credential{}, false, err
	}
	if len(data) == 0 {
		return Credential{}, false, nil
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return Credential{}, false, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Credential{}, false, err
	}
	if len(data) < aead.NonceSize() {
		return Credential{}, false, fmt.Errorf("auth broker: corrupt credential record")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], []byte("codex-runner-chatgpt-auth-v1"))
	if err != nil {
		return Credential{}, false, fmt.Errorf("auth broker: decrypt credentials: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal(plain, &credential); err != nil {
		return Credential{}, false, err
	}
	return credential, true, nil
}

func (b *Broker) write(ctx context.Context, credential Credential) error {
	plain, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plain, []byte("codex-runner-chatgpt-auth-v1"))
	return b.store.Write(ctx, append(nonce, ciphertext...))
}

func ParseEncryptionKey(encoded string) ([]byte, error) {
	key, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("RUNNER_AUTH_ENCRYPTION_KEY must be base64 for exactly 32 random bytes")
	}
	return key, nil
}

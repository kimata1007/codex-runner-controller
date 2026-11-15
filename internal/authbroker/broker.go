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
	HostID       string    `json:"hostId"`
	ClientID     string    `json:"clientId"`
	AccountID    string    `json:"accountId"`
	Email        string    `json:"email,omitempty"`
	PlanType     string    `json:"planType,omitempty"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	IDToken      string    `json:"idToken"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type Store interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Delete(context.Context) error
}

type Broker struct {
	store    Store
	key      []byte
	client   *http.Client
	mu       sync.Mutex
	now      func() time.Time
	attempts map[string]loginAttempt
}

type loginAttempt struct {
	hostID    string
	createdAt time.Time
}

type OAuthTokenBundle struct {
	ClientID     string `json:"clientId"`
	IDToken      string `json:"idToken"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expiresIn"`
	Nonce        string `json:"nonce"`
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

func (b *Broker) BeginLogin(ctx context.Context) (string, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeExpiredAttempts()
	credential, ok, err := b.read(ctx)
	if err != nil {
		return "", "", err
	}
	hostID := credential.HostID
	if hostID == "" {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", "", err
		}
		raw[6] = raw[6]&0x0f | 0x40
		raw[8] = raw[8]&0x3f | 0x80
		hostID = fmt.Sprintf("urn:uuid:%08x-%04x-%04x-%04x-%012x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
	}
	if !ok {
		credential = Credential{}
	}
	credential.HostID = hostID
	if err := b.write(ctx, credential); err != nil {
		return "", "", err
	}
	var attempt [32]byte
	if _, err := rand.Read(attempt[:]); err != nil {
		return "", "", err
	}
	id := base64.RawURLEncoding.EncodeToString(attempt[:])
	b.attempts[id] = loginAttempt{hostID: hostID, createdAt: b.now()}
	return id, hostID, nil
}

func (b *Broker) CancelLogin(attemptID string) {
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

func (b *Broker) Import(ctx context.Context, attemptID string, bundle OAuthTokenBundle) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	attempt, ok := b.attempts[attemptID]
	if !ok || b.now().Sub(attempt.createdAt) > 10*time.Minute {
		return fmt.Errorf("sign-in attempt is invalid or expired")
	}
	delete(b.attempts, attemptID)
	if bundle.Nonce != attemptID {
		return fmt.Errorf("ID token nonce does not match the sign-in attempt")
	}
	scopes := strings.Fields(bundle.Scope)
	if !contains(scopes, "chatgpt.tokens.use.direct") || !contains(scopes, "offline_access") {
		return fmt.Errorf("ChatGPT plan usage permission was not granted")
	}
	if bundle.ClientID == "" || strings.TrimSpace(bundle.ClientID) == "dynamic_agent_client" || bundle.AccessToken == "" || bundle.RefreshToken == "" || bundle.IDToken == "" {
		return fmt.Errorf("sign-in token response is incomplete")
	}
	claims, err := validateIDToken(ctx, b.client, bundle.IDToken, bundle.ClientID, bundle.Nonce)
	if err != nil {
		return err
	}
	credential := Credential{HostID: attempt.hostID, ClientID: bundle.ClientID, AccountID: claims.Subject, Email: claims.Email,
		AccessToken: bundle.AccessToken, RefreshToken: bundle.RefreshToken, IDToken: bundle.IDToken, Scopes: scopes,
		ExpiresAt: b.now().Add(time.Duration(bundle.ExpiresIn) * time.Second)}
	return b.write(ctx, credential)
}

func (b *Broker) Status(ctx context.Context) (map[string]any, error) {
	credential, ok, err := b.read(ctx)
	if err != nil {
		return nil, err
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
		"refresh_token": {credential.RefreshToken}, "resource": {"https://api.openai.com/v1"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
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

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
}

func toAgentTokens(c Credential) agent.ChatGPTAuthTokens {
	return agent.ChatGPTAuthTokens{AccessToken: c.AccessToken, AccountID: c.AccountID, PlanType: c.PlanType}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

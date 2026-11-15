package authbroker

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const jwksURL = "https://auth.openai.com/.well-known/jwks.json"

type idTokenClaims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Email     string `json:"email"`
	Audience  any    `json:"aud"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"nonce"`
}

func validateIDToken(ctx context.Context, client *http.Client, token, clientID, nonce string) (idTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return idTokenClaims{}, fmt.Errorf("invalid ID token")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return idTokenClaims{}, err
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil || header.Algorithm != "RS256" || header.KeyID == "" {
		return idTokenClaims{}, fmt.Errorf("unsupported ID token signing key")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return idTokenClaims{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return idTokenClaims{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return idTokenClaims{}, fmt.Errorf("fetch identity signing keys: %s", response.Status)
	}
	var set struct {
		Keys []struct {
			KeyID    string `json:"kid"`
			KeyType  string `json:"kty"`
			Modulus  string `json:"n"`
			Exponent string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&set); err != nil {
		return idTokenClaims{}, err
	}
	var key *rsa.PublicKey
	for _, candidate := range set.Keys {
		if candidate.KeyID != header.KeyID || candidate.KeyType != "RSA" {
			continue
		}
		n, e := base64.RawURLEncoding.DecodeString(candidate.Modulus)
		if e != nil {
			return idTokenClaims{}, e
		}
		eb, e := base64.RawURLEncoding.DecodeString(candidate.Exponent)
		if e != nil {
			return idTokenClaims{}, e
		}
		exponent := new(big.Int).SetBytes(eb).Int64()
		if exponent < 3 || exponent > int64(^uint(0)>>1) {
			return idTokenClaims{}, fmt.Errorf("invalid identity signing key")
		}
		key = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}
		break
	}
	if key == nil {
		return idTokenClaims{}, fmt.Errorf("identity signing key not found")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return idTokenClaims{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return idTokenClaims{}, fmt.Errorf("verify ID token signature: %w", err)
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return idTokenClaims{}, err
	}
	var claims idTokenClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return idTokenClaims{}, err
	}
	if claims.Issuer != "https://auth.openai.com" || claims.Subject == "" || claims.Nonce != nonce || claims.ExpiresAt <= time.Now().Unix() || !hasAudience(claims.Audience, clientID) {
		return idTokenClaims{}, fmt.Errorf("ID token claims do not match the sign-in attempt")
	}
	return claims, nil
}

func hasAudience(audience any, expected string) bool {
	switch value := audience.(type) {
	case string:
		return value == expected
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok && text == expected {
				return true
			}
		}
	}
	return false
}

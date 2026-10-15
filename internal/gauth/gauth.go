// Package gauth signs the user in to Google Drive with the narrow drive.file scope (CardHaul can only
// see files it created), using the loopback + PKCE flow Google recommends for desktop apps.
// The token is kept in the macOS Keychain / Windows Credential Manager.
package gauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/browser"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	Scope          = "https://www.googleapis.com/auth/drive.file"
	keyringService = "CardHaul"
	keyringUser    = "google-token"
)

// Built-in OAuth client, injected at build time:
//
//	-ldflags "-X github.com/cknm-cmd/cardhaul/internal/gauth.clientID=... -X ...clientSecret=..."
//
// A desktop client's secret isn't confidential (Google documents this), but it's kept out of git.
// For development, google-client.json (the file Google Cloud lets you download) in the CardHaul
// config folder is used instead.
var clientID, clientSecret string

// Client returns the OAuth client credentials CardHaul should use.
func Client(configDir string) (id, secret string, err error) {
	if data, readErr := os.ReadFile(filepath.Join(configDir, "google-client.json")); readErr == nil {
		var f struct {
			Installed struct {
				ClientID     string `json:"client_id"`
				ClientSecret string `json:"client_secret"`
			} `json:"installed"`
		}
		if json.Unmarshal(data, &f) == nil && f.Installed.ClientID != "" {
			return f.Installed.ClientID, f.Installed.ClientSecret, nil
		}
	}
	if clientID == "" {
		return "", "", errors.New("no Google client configured for this build")
	}
	return clientID, clientSecret, nil
}

func oauthConfig(id, secret, redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     id,
		ClientSecret: secret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirect,
		Scopes:       []string{Scope},
	}
}

// SignIn opens the browser for Google sign-in and stores the resulting token.
func SignIn(ctx context.Context, configDir string) (*oauth2.Token, error) {
	id, secret, err := Client(configDir)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", listener.Addr().(*net.TCPAddr).Port)
	conf := oauthConfig(id, secret, redirect)

	verifier := oauth2.GenerateVerifier()
	state := oauth2.GenerateVerifier()
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("error") != "":
			fmt.Fprint(w, page("Sign-in was cancelled.", "You can close this tab."))
			results <- result{err: fmt.Errorf("Google sign-in failed: %s", q.Get("error"))}
		case q.Get("code") != "" && q.Get("state") == state:
			fmt.Fprint(w, page("You're signed in.", "You can close this tab and go back to CardHaul."))
			results <- result{code: q.Get("code")}
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	if err := browser.OpenURL(authURL); err != nil {
		return nil, fmt.Errorf("couldn't open the browser: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Minute):
		return nil, errors.New("sign-in timed out")
	case r := <-results:
		if r.err != nil {
			return nil, r.err
		}
		token, err := conf.Exchange(ctx, r.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, err
		}
		if token.RefreshToken == "" {
			return nil, errors.New("Google didn't return a refresh token. Try signing in again")
		}
		return token, SaveToken(token)
	}
}

// SaveToken stores the token (including refreshed access tokens) in the system keychain.
func SaveToken(token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, keyringUser, string(data))
}

// LoadToken returns the saved token, or nil when signed out.
func LoadToken() *oauth2.Token {
	data, err := keyring.Get(keyringService, keyringUser)
	if err != nil {
		return nil
	}
	var token oauth2.Token
	if json.Unmarshal([]byte(data), &token) != nil || token.RefreshToken == "" {
		return nil
	}
	return &token
}

// SignOut revokes the token with Google (best effort) and removes it from the keychain.
func SignOut() {
	if token := LoadToken(); token != nil {
		go func() {
			resp, err := http.PostForm("https://oauth2.googleapis.com/revoke", map[string][]string{"token": {token.RefreshToken}})
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	_ = keyring.Delete(keyringService, keyringUser)
}

func page(title, body string) string {
	return `<!doctype html><meta charset="utf-8"><title>CardHaul</title>
<style>body{font:16px -apple-system,Segoe UI,sans-serif;display:grid;place-items:center;height:90vh;color:#333}</style>
<div><h2>` + title + `</h2><p>` + body + `</p></div>`
}

// Account is shown in Settings: who's signed in and how much space is left.
type Account struct {
	Email     string `json:"email"`
	FreeBytes int64  `json:"freeBytes"` // -1 when unlimited
}

// AccountInfo asks Drive who's signed in and how much space is free.
func AccountInfo(ctx context.Context, configDir string) (Account, error) {
	token := LoadToken()
	if token == nil {
		return Account{}, errors.New("not signed in")
	}
	id, secret, err := Client(configDir)
	if err != nil {
		return Account{}, err
	}
	source := oauthConfig(id, secret, "").TokenSource(ctx, token)
	client := oauth2.NewClient(ctx, source)
	resp, err := client.Get("https://www.googleapis.com/drive/v3/about?fields=user(emailAddress),storageQuota(limit,usage)")
	if err != nil {
		return Account{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Account{}, fmt.Errorf("Google Drive returned %s", resp.Status)
	}
	var about struct {
		User struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
		StorageQuota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&about); err != nil {
		return Account{}, err
	}
	if fresh, err := source.Token(); err == nil && fresh.AccessToken != token.AccessToken {
		if fresh.RefreshToken == "" {
			fresh.RefreshToken = token.RefreshToken
		}
		_ = SaveToken(fresh)
	}
	acct := Account{Email: about.User.EmailAddress, FreeBytes: -1}
	var limit, usage int64
	if _, err := fmt.Sscan(about.StorageQuota.Limit, &limit); err == nil && limit > 0 {
		fmt.Sscan(about.StorageQuota.Usage, &usage)
		acct.FreeBytes = limit - usage
	}
	return acct, nil
}

package hydra

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

// OAuthConfig holds the client side settings of the authorization code flow.
// The authorize URL is opened by the browser, the token URL is called by us,
// so in docker they point at different hostnames.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	Scopes       []string
}

// OAuthClient runs the authorization code flow with PKCE.
type OAuthClient struct {
	config *oauth2.Config
	http   *http.Client
}

func NewOAuthClient(cfg OAuthConfig, client *http.Client) *OAuthClient {
	return &OAuthClient{
		http: client,
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:   cfg.AuthURL,
				TokenURL:  cfg.TokenURL,
				AuthStyle: oauth2.AuthStyleInHeader, // client_secret_basic
			},
		},
	}
}

// NewFlow returns the PKCE verifier and the URL that starts the flow.
func (c *OAuthClient) NewFlow(state string) (verifier, authURL string) {
	verifier = oauth2.GenerateVerifier()
	return verifier, c.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange trades an authorization code for tokens. It is never retried: a
// code works once, so a repeat attempt after a lost reply would always fail.
func (c *OAuthClient) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.http)
	token, err := c.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}
	return token, nil
}

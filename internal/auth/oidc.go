// Package auth implements the OIDC login flow.
package auth

import (
    "context"
    "errors"
    "fmt"
    "net/url"
    "strings"
    "sync"

    "github.com/coreos/go-oidc/v3/oidc"
    "golang.org/x/oauth2"

    "drawered/internal/config"
    "drawered/internal/service"
)

// DevAuthAllowed is true only in binaries built with the dev tag. It
// gates DRAWERED_DEV_AUTH, which bypasses OIDC.
var DevAuthAllowed = false

// DevIssuer is the issuer recorded for development-login users.
const DevIssuer = "dev"

// ErrInvalidState is returned when the callback state is unknown or stale.
var ErrInvalidState = errors.New("login request expired or invalid; please try again")

// OIDC performs the authorisation code flow with PKCE.
type OIDC struct {
    cfg *config.Config
    svc *service.Service

    mu         sync.Mutex
    provider   *oidc.Provider
    verifier   *oidc.IDTokenVerifier
    oauth      *oauth2.Config
    endSession string
}

// New creates an OIDC client. Provider discovery happens lazily so the
// server can start while the IdP is unreachable.
func New(cfg *config.Config, svc *service.Service) *OIDC {
    return &OIDC{cfg: cfg, svc: svc}
}

// DevMode reports whether development login is active.
func (o *OIDC) DevMode() bool {
    return DevAuthAllowed && o.cfg.DevAuth != ""
}

func (o *OIDC) init(ctx context.Context) error {
    o.mu.Lock()
    defer o.mu.Unlock()
    if o.provider != nil {
        return nil
    }
    p, err := oidc.NewProvider(ctx, o.cfg.OIDCIssuer)
    if err != nil {
        return fmt.Errorf("OIDC discovery: %w", err)
    }
    var extra struct {
        EndSession string `json:"end_session_endpoint"`
    }
    _ = p.Claims(&extra)
    o.provider = p
    o.verifier = p.Verifier(&oidc.Config{ClientID: o.cfg.OIDCClientID})
    o.oauth = &oauth2.Config{
        ClientID:     o.cfg.OIDCClientID,
        ClientSecret: o.cfg.OIDCClientSecret,
        Endpoint:     p.Endpoint(),
        RedirectURL:  o.cfg.BaseURL + "/auth/callback",
        Scopes:       o.cfg.OIDCScopes,
    }
    o.endSession = extra.EndSession
    return nil
}

// SafeReturnTo restricts post-login redirects to local paths.
func SafeReturnTo(s string) string {
    if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") ||
        strings.HasPrefix(s, "/auth/") {
        return "/"
    }
    return s
}

// LoginURL starts a login and returns the IdP URL to redirect to.
func (o *OIDC) LoginURL(ctx context.Context, returnTo string) (string, error) {
    if err := o.init(ctx); err != nil {
        return "", err
    }
    state := service.RandomToken(24)
    nonce := service.RandomToken(24)
    verifier := oauth2.GenerateVerifier()
    if err := o.svc.SaveLoginState(ctx, state, service.LoginState{
        Nonce: nonce, Verifier: verifier, ReturnTo: SafeReturnTo(returnTo),
    }); err != nil {
        return "", err
    }
    return o.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Callback completes a login, returning the user and where to send them.
func (o *OIDC) Callback(ctx context.Context, q url.Values) (*service.User, string, error) {
    if e := q.Get("error"); e != "" {
        return nil, "", fmt.Errorf("identity provider error: %s %s", e, q.Get("error_description"))
    }
    if err := o.init(ctx); err != nil {
        return nil, "", err
    }
    ls, err := o.svc.TakeLoginState(ctx, q.Get("state"))
    if err != nil {
        return nil, "", err
    }
    if ls == nil {
        return nil, "", ErrInvalidState
    }
    tok, err := o.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(ls.Verifier))
    if err != nil {
        return nil, "", fmt.Errorf("token exchange: %w", err)
    }
    raw, ok := tok.Extra("id_token").(string)
    if !ok {
        return nil, "", errors.New("token response has no id_token")
    }
    idt, err := o.verifier.Verify(ctx, raw)
    if err != nil {
        return nil, "", fmt.Errorf("verify id_token: %w", err)
    }
    if idt.Nonce != ls.Nonce {
        return nil, "", errors.New("id_token nonce mismatch")
    }
    claims := map[string]any{}
    if err := idt.Claims(&claims); err != nil {
        return nil, "", err
    }
    // Merge userinfo for claims some providers leave out of the ID token.
    if ui, err := o.provider.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
        extra := map[string]any{}
        if ui.Claims(&extra) == nil {
            for k, v := range extra {
                if _, exists := claims[k]; !exists {
                    claims[k] = v
                }
            }
        }
    }
    u, err := o.svc.LoginUser(ctx, idt.Issuer, idt.Subject, claims)
    if err != nil {
        return nil, "", err
    }
    return u, ls.ReturnTo, nil
}

// DevLogin logs in as the configured development user.
func (o *OIDC) DevLogin(ctx context.Context) (*service.User, error) {
    email := o.cfg.DevAuth
    name, _, _ := strings.Cut(email, "@")
    return o.svc.LoginUser(ctx, DevIssuer, email, map[string]any{
        "email": email, "name": name, "preferred_username": name,
    })
}

// LogoutURL returns the IdP's end-session URL, if it advertises one.
func (o *OIDC) LogoutURL() string {
    o.mu.Lock()
    defer o.mu.Unlock()
    if o.endSession == "" {
        return ""
    }
    u, err := url.Parse(o.endSession)
    if err != nil {
        return ""
    }
    q := u.Query()
    q.Set("client_id", o.cfg.OIDCClientID)
    q.Set("post_logout_redirect_uri", o.cfg.BaseURL+"/")
    u.RawQuery = q.Encode()
    return u.String()
}

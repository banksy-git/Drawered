package api

import (
    "context"
    "crypto/rand"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/go-jose/go-jose/v4"

    "drawered/internal/auth"
    "drawered/internal/service"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS, token and userinfo.
type fakeIdP struct {
    t      *testing.T
    srv    *httptest.Server
    key    *rsa.PrivateKey
    mu     sync.Mutex
    codes  map[string]pendingCode
    claims map[string]any
}

type pendingCode struct {
    nonce     string
    challenge string
}

func newFakeIdP(t *testing.T, claims map[string]any) *fakeIdP {
    key, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        t.Fatal(err)
    }
    f := &fakeIdP{t: t, key: key, codes: map[string]pendingCode{}, claims: claims}
    mux := http.NewServeMux()
    mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]any{
            "issuer":                                f.srv.URL,
            "authorization_endpoint":                f.srv.URL + "/authorize",
            "token_endpoint":                        f.srv.URL + "/token",
            "jwks_uri":                              f.srv.URL + "/jwks",
            "userinfo_endpoint":                     f.srv.URL + "/userinfo",
            "end_session_endpoint":                  f.srv.URL + "/logout",
            "id_token_signing_alg_values_supported": []string{"RS256"},
        })
    })
    mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
            {Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
        }})
    })
    mux.HandleFunc("POST /token", f.token)
    mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("Authorization") != "Bearer access-token" {
            http.Error(w, "bad token", http.StatusUnauthorized)
            return
        }
        json.NewEncoder(w).Encode(map[string]any{"sub": claims["sub"], "preferred_username": "from-userinfo"})
    })
    f.srv = httptest.NewServer(mux)
    t.Cleanup(f.srv.Close)
    return f
}

// authorize records what the relying party sent and returns a code.
func (f *fakeIdP) authorize(authURL string) (code, state string) {
    u, err := url.Parse(authURL)
    if err != nil {
        f.t.Fatal(err)
    }
    q := u.Query()
    if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
        f.t.Fatalf("PKCE not used: %v", q)
    }
    if q.Get("nonce") == "" {
        f.t.Fatal("no nonce")
    }
    code = "code-" + q.Get("state")[:8]
    f.mu.Lock()
    f.codes[code] = pendingCode{nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
    f.mu.Unlock()
    return code, q.Get("state")
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
    r.ParseForm()
    f.mu.Lock()
    pc, ok := f.codes[r.Form.Get("code")]
    delete(f.codes, r.Form.Get("code"))
    f.mu.Unlock()
    if !ok {
        http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
        return
    }
    sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
    if base64.RawURLEncoding.EncodeToString(sum[:]) != pc.challenge {
        http.Error(w, `{"error":"invalid_grant","error_description":"PKCE"}`, http.StatusBadRequest)
        return
    }
    now := time.Now()
    claims := map[string]any{
        "iss": f.srv.URL, "aud": "drawered", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": pc.nonce,
    }
    for k, v := range f.claims {
        claims[k] = v
    }
    payload, _ := json.Marshal(claims)
    signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
        (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
    if err != nil {
        f.t.Fatal(err)
    }
    jws, err := signer.Sign(payload)
    if err != nil {
        f.t.Fatal(err)
    }
    idToken, _ := jws.CompactSerialize()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "access_token": "access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
    })
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func TestOIDCLogin(t *testing.T) {
    idp := newFakeIdP(t, map[string]any{
        "sub": "user-123", "email": "Jo@Example.com", "name": "Jo Bloggs", "groups": []string{"inventory-stores"},
    })
    h := newHarness(t)
    h.cfg.OIDCIssuer = idp.srv.URL
    h.cfg.OIDCClientID = "drawered"
    h.cfg.OIDCScopes = []string{"openid", "profile", "email"}
    h.cfg.BaseURL = h.srv.URL
    h.cfg.BootstrapAdmins = []string{"jo@example.com"}

    // A mapping that grants Stores from the groups claim.
    if _, err := h.svc.CreateMapping(context.Background(), mappingInput("groups", "contains", "inventory-stores", h.roleID("Stores"))); err != nil {
        t.Fatal(err)
    }

    hc := &http.Client{CheckRedirect: noRedirect}
    resp, err := hc.Get(h.srv.URL + "/auth/login?return_to=/parts/7")
    if err != nil {
        t.Fatal(err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), idp.srv.URL+"/authorize") {
        t.Fatalf("login redirect %d %q", resp.StatusCode, resp.Header.Get("Location"))
    }
    code, state := idp.authorize(resp.Header.Get("Location"))

    // A forged state is refused.
    resp, _ = hc.Get(h.srv.URL + "/auth/callback?code=" + code + "&state=forged")
    resp.Body.Close()
    if resp.StatusCode != http.StatusForbidden {
        t.Fatalf("forged state: %d", resp.StatusCode)
    }

    resp, err = hc.Get(h.srv.URL + "/auth/callback?code=" + code + "&state=" + url.QueryEscape(state))
    if err != nil {
        t.Fatal(err)
    }
    resp.Body.Close()
    if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/parts/7" {
        t.Fatalf("callback %d %q", resp.StatusCode, resp.Header.Get("Location"))
    }
    var token string
    for _, c := range resp.Cookies() {
        if c.Name == SessionCookie {
            token = c.Value
            if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
                t.Fatalf("cookie flags %+v", c)
            }
        }
    }
    if token == "" {
        t.Fatal("no session cookie")
    }

    c := &client{h: h, t: t, cookie: token}
    var me Me
    c.must("GET", "/api/v1/me", nil, 200, &me)
    if me.User.Subject != "user-123" || me.User.DisplayName != "Jo Bloggs" || me.User.Username != "from-userinfo" {
        t.Fatalf("user %+v", me.User)
    }
    sources := map[string]string{}
    for _, r := range me.User.Roles {
        sources[r.Name] = r.Source
    }
    if sources["Stores"] != "mapping" || sources["Admin"] != "bootstrap" || sources["Read only"] != "manual" {
        t.Fatalf("roles %+v", me.User.Roles)
    }

    // The state cannot be replayed.
    resp, _ = hc.Get(h.srv.URL + "/auth/callback?code=" + code + "&state=" + url.QueryEscape(state))
    resp.Body.Close()
    if resp.StatusCode != http.StatusForbidden {
        t.Fatalf("replayed state: %d", resp.StatusCode)
    }

    // Logout ends the session and points at the IdP's end-session endpoint.
    c.csrf = me.CSRFToken
    var out struct {
        Redirect string `json:"redirect"`
    }
    c.must("POST", "/auth/logout", nil, 200, &out)
    if !strings.HasPrefix(out.Redirect, idp.srv.URL+"/logout") {
        t.Fatalf("logout redirect %q", out.Redirect)
    }
    c.errCode("GET", "/api/v1/me", nil, 401)
}

func TestSafeReturnTo(t *testing.T) {
    for in, want := range map[string]string{
        "/parts/1":           "/parts/1",
        "//evil.example":     "/",
        "https://evil":       "/",
        "/\\evil":            "/",
        "/auth/login":        "/",
        "":                   "/",
        "/?q=resistor&tag=a": "/?q=resistor&tag=a",
    } {
        if got := safeReturnTo(in); got != want {
            t.Errorf("%q: got %q want %q", in, got, want)
        }
    }
}

var safeReturnTo = auth.SafeReturnTo

func mappingInput(claim, match, value string, roleID int64) service.MappingInput {
    return service.MappingInput{Claim: claim, MatchType: match, Value: value, RoleID: roleID}
}

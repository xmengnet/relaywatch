package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"relaywatch/internal/store"
)

// OIDCService OIDC 授权码流 + PKCE。
type OIDCService struct {
	Store      store.Store
	SessionTTL time.Duration
	BaseURL    string // 外部可达地址，回调 URL 用

	mu        sync.Mutex
	verifiers map[string]*oidc.IDTokenVerifier // issuer -> verifier
	pending   map[string]*pendingAuth          // state -> 请求上下文
}

type pendingAuth struct {
	ProviderName string
	Verifier     string // PKCE code_verifier
	CreatedAt    time.Time
}

// NewOIDCService 构造。
func NewOIDCService(st store.Store, sessionTTL time.Duration, baseURL string) *OIDCService {
	return &OIDCService{
		Store:      st,
		SessionTTL: sessionTTL,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		verifiers:  map[string]*oidc.IDTokenVerifier{},
		pending:    map[string]*pendingAuth{},
	}
}

// Begin 生成 state 与 PKCE verifier，返回 IdP 授权页 URL。
func (s *OIDCService) Begin(ctx context.Context, providerName string) (redirectURL string, err error) {
	p, err := s.Store.OIDCProviderByName(ctx, providerName)
	if err == store.ErrNotFound {
		return "", fmt.Errorf("OIDC provider %q 不存在", providerName)
	}
	if err != nil {
		return "", err
	}
	if !p.Enabled {
		return "", fmt.Errorf("OIDC provider %q 已停用", providerName)
	}
	prov, err := oidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return "", fmt.Errorf("发现 IdP 配置失败: %w", err)
	}

	verifier := RandomToken(48)
	state := RandomToken(24)
	s.mu.Lock()
	// 清理 10 分钟前的过期项
	for k, v := range s.pending {
		if time.Since(v.CreatedAt) > 10*time.Minute {
			delete(s.pending, k)
		}
	}
	s.pending[state] = &pendingAuth{ProviderName: providerName, Verifier: verifier, CreatedAt: time.Now()}
	s.mu.Unlock()

	oc := oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  s.BaseURL + "/auth/oidc/" + providerName + "/callback",
		Scopes:       strings.Fields(p.Scopes),
	}
	url := oc.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	return url, nil
}

// OIDCUser 回调校验后的用户信息。
type OIDCUser struct {
	Subject string
	Name    string
	Email   string
}

// Complete 处理回调：校验 state、换 token、验证 ID Token、检查 claim 规则。
func (s *OIDCService) Complete(ctx context.Context, providerName, state, code string) (*store.User, error) {
	s.mu.Lock()
	pa, ok := s.pending[state]
	s.mu.Unlock()
	if !ok || pa.ProviderName != providerName {
		return nil, fmt.Errorf("state 无效或已过期")
	}
	s.mu.Lock()
	delete(s.pending, state)
	s.mu.Unlock()

	p, err := s.Store.OIDCProviderByName(ctx, providerName)
	if err != nil {
		return nil, err
	}
	prov, err := oidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return nil, fmt.Errorf("发现 IdP 配置失败: %w", err)
	}
	oc := oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  s.BaseURL + "/auth/oidc/" + providerName + "/callback",
		Scopes:       strings.Fields(p.Scopes),
	}
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(pa.Verifier))
	if err != nil {
		return nil, fmt.Errorf("code 换 token 失败: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("IdP 未返回 id_token")
	}
	verifier := prov.Verifier(&oidc.Config{ClientID: p.ClientID})
	idToken, err := verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("id_token 校验失败: %w", err)
	}
	var claims struct {
		Sub   string         `json:"sub"`
		Name  string         `json:"name"`
		Email string         `json:"email"`
		Other map[string]any `json:"-"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}

	// claim 规则
	if err := checkClaimRules(p.ClaimRules, claims.Email, idToken); err != nil {
		return nil, err
	}

	// 查找或创建用户
	u, err := s.Store.UserByOIDC(ctx, p.Name, claims.Sub)
	if err == store.ErrNotFound {
		u = &store.User{
			Username:     firstNonEmpty(claims.Name, claims.Email, claims.Sub),
			Email:        claims.Email,
			Role:         s.defaultOIDCRole(ctx), // 系统设置 oidc.default_role，默认 viewer
			OIDCProvider: p.Name,
			OIDCSubject:  claims.Sub,
		}
		if err := s.Store.CreateUser(ctx, u); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, ErrForbidden
	}
	if err := s.Store.UpdateUserLogin(ctx, u.ID, time.Now()); err != nil {
		return nil, err
	}
	return u, nil
}

// defaultOIDCRole 读取 OIDC 首次登录自动建号的默认角色设置；非法或缺省回 viewer。
func (s *OIDCService) defaultOIDCRole(ctx context.Context) string {
	v, err := s.Store.GetSetting(ctx, "oidc.default_role")
	if err == nil && (v == "user" || v == "viewer") {
		return v
	}
	return "viewer"
}

// checkClaimRules 校验 email_domains / groups 白名单。规则为空 = 不限制。
func checkClaimRules(rulesJSON, email string, idToken *oidc.IDToken) error {
	if rulesJSON == "" {
		return nil
	}
	var rules struct {
		EmailDomains []string `json:"email_domains"`
		Groups       *struct {
			Claim string   `json:"claim"`
			AnyOf []string `json:"any_of"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
		return fmt.Errorf("claim_rules 解析失败: %w", err)
	}
	if len(rules.EmailDomains) > 0 {
		at := strings.LastIndex(email, "@")
		if at < 0 {
			return ErrForbidden
		}
		domain := strings.ToLower(email[at+1:])
		if !containsFold(rules.EmailDomains, domain) {
			return ErrForbidden
		}
	}
	if rules.Groups != nil && len(rules.Groups.AnyOf) > 0 {
		claim := rules.Groups.Claim
		if claim == "" {
			claim = "groups"
		}
		var extra map[string]any
		_ = idToken.Claims(&extra)
		raw, present := extra[claim]
		if !present {
			return ErrForbidden
		}
		userGroups := map[string]bool{}
		switch gv := raw.(type) {
		case []any:
			for _, g := range gv {
				userGroups[fmt.Sprint(g)] = true
			}
		case string:
			for _, g := range strings.Fields(gv) {
				userGroups[g] = true
			}
		}
		ok := false
		for _, want := range rules.Groups.AnyOf {
			if userGroups[want] {
				ok = true
				break
			}
		}
		if !ok {
			return ErrForbidden
		}
	}
	return nil
}

// SessionFor 为 OIDC 用户建会话。
func (s *OIDCService) SessionFor(ctx context.Context, userID int64, userAgent, ip string) (*store.Session, error) {
	sess := &store.Session{
		ID:        RandomToken(32),
		UserID:    userID,
		CSRFToken: RandomToken(24),
		UserAgent: userAgent,
		IP:        ip,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.SessionTTL),
	}
	if err := s.Store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// 密码学随机已由 RandomToken 提供；以下为 SHA256 工具备用。
var _ = sha256.Sum256
var _ = base64.RawURLEncoding

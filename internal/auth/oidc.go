package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"relaywatch/internal/store"
)

// OIDC 流程错误：
//   - ErrOIDCNotBound：IdP 身份未绑定任何用户，且注册已关闭（不能自动建号）。
//   - ErrOIDCTaken：IdP 身份已被其他用户绑定。
var ErrOIDCNotBound = errors.New("该 IdP 账号未绑定系统用户，且注册已关闭")
var ErrOIDCTaken = errors.New("该 IdP 账号已绑定其他用户")

// OIDCService OIDC 授权码流 + PKCE。
type OIDCService struct {
	Store      store.Store
	SessionTTL time.Duration
	BaseURL    string // 外部可达地址，回调 URL 用

	mu          sync.Mutex
	verifiers   map[string]*oidc.IDTokenVerifier // issuer -> verifier
	pending     map[string]*pendingAuth          // state -> 请求上下文
	bindConfirm map[string]*bindConfirmReq       // 确认 token -> 待确认绑定
}

type pendingAuth struct {
	ProviderName string
	Verifier     string // PKCE code_verifier
	BindUserID   int64  // 非 0 = 绑定流程：回调后把 IdP 身份绑到该用户，而非建会话
	CreatedAt    time.Time
}

// bindConfirmReq 回调校验通过后待用户确认的绑定。绑定生效前必须由
// 该用户在自己的会话里确认：回调 URL 中的 state 可能经浏览器历史/代理日志
// 泄露，或 IdP 侧会话与发起者不符，直接生效等于无确认的账号接管通道。
type bindConfirmReq struct {
	UserID    int64
	Provider  string
	Subject   string
	Display   string
	CreatedAt time.Time
}

// BindConfirmation 待确认绑定对外视图（不含 subject）。
type BindConfirmation struct {
	Token    string
	Provider string
	Identity string // IdP 侧显示名/邮箱
}

// pendingAuthTTL 是 OIDC state 的有效期：超时后回调一律拒绝。
const pendingAuthTTL = 10 * time.Minute

// NewOIDCService 构造。
func NewOIDCService(st store.Store, sessionTTL time.Duration, baseURL string) *OIDCService {
	return &OIDCService{
		Store:       st,
		SessionTTL:  sessionTTL,
		BaseURL:     strings.TrimRight(baseURL, "/"),
		verifiers:   map[string]*oidc.IDTokenVerifier{},
		pending:     map[string]*pendingAuth{},
		bindConfirm: map[string]*bindConfirmReq{},
	}
}

// Begin 生成 state 与 PKCE verifier，返回 IdP 授权页 URL（登录流程）。
func (s *OIDCService) Begin(ctx context.Context, providerName string) (redirectURL string, err error) {
	return s.begin(ctx, providerName, 0)
}

// BeginBind 绑定流程入口：已登录用户跳转 IdP，回调后把 IdP 身份绑到该用户而非建会话。
// 回调与登录共用同一 redirect URL（/auth/oidc/{name}/callback），由 state 区分流程，
// IdP 侧只需注册一个回调地址。
func (s *OIDCService) BeginBind(ctx context.Context, providerName string, userID int64) (redirectURL string, err error) {
	return s.begin(ctx, providerName, userID)
}

func (s *OIDCService) begin(ctx context.Context, providerName string, bindUserID int64) (string, error) {
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
	prov, err := oidc.NewProvider(ctx, normalizeIssuer(p.Issuer))
	if err != nil {
		return "", fmt.Errorf("发现 IdP 配置失败: %w", err)
	}

	verifier := RandomToken(48)
	state := RandomToken(24)
	s.mu.Lock()
	// 清理过期的待回调项
	for k, v := range s.pending {
		if time.Since(v.CreatedAt) > pendingAuthTTL {
			delete(s.pending, k)
		}
	}
	s.pending[state] = &pendingAuth{ProviderName: providerName, Verifier: verifier, BindUserID: bindUserID, CreatedAt: time.Now()}
	s.mu.Unlock()

	redirectPath := "/auth/oidc/" + providerName + "/callback"
	oc := oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  s.BaseURL + redirectPath,
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

// Finish 统一处理 IdP 回调：登录与绑定共用同一 redirect URL，按 pending 中
// 记录的流程类型（BindUserID）分流。绑定流程返回 bound=true 与待确认的
// confirm（不直接落库，见 ConfirmBind）；身份已被他人绑定时返回 ErrOIDCTaken；
// 未绑定且注册已关闭返回 ErrOIDCNotBound。
func (s *OIDCService) Finish(ctx context.Context, providerName, state, code string) (u *store.User, bound bool, confirm *BindConfirmation, err error) {
	pa, perr := s.takePending(providerName, state)
	if perr != nil {
		return nil, false, nil, perr
	}
	bound = pa.BindUserID != 0
	p, err := s.Store.OIDCProviderByName(ctx, providerName)
	if err != nil {
		return nil, bound, nil, err
	}
	// begin 时校验过 Enabled，但回调距发起可达 10 分钟，中途停用不得继续
	if !p.Enabled {
		return nil, bound, nil, fmt.Errorf("OIDC provider %q 已停用", providerName)
	}
	claims, err := s.finishOIDC(ctx, p, code, pa.Verifier)
	if err != nil {
		return nil, bound, nil, err
	}

	if bound {
		existing, err := s.Store.UserByOIDC(ctx, p.Name, claims.Subject)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, bound, nil, err
		}
		if existing != nil && existing.ID != pa.BindUserID {
			return nil, bound, nil, ErrOIDCTaken
		}
		if existing != nil {
			// 已绑到发起用户本人：无需重复绑定
			return nil, bound, nil, nil
		}
		return nil, bound, s.stageBindConfirm(pa.BindUserID, p.Name, claims), nil
	}

	// 登录流程：查找或创建用户
	u, err = s.Store.UserByOIDC(ctx, p.Name, claims.Subject)
	if errors.Is(err, store.ErrNotFound) {
		if !s.autoProvisionAllowed(ctx) {
			return nil, bound, nil, ErrOIDCNotBound
		}
		u = &store.User{
			Username:     firstNonEmpty(claims.Name, claims.Email, claims.Subject),
			Email:        claims.Email,
			Role:         s.defaultOIDCRole(ctx), // 系统设置 oidc.default_role，默认 viewer
			OIDCProvider: p.Name,
			OIDCSubject:  claims.Subject,
		}
		if err := s.Store.CreateUser(ctx, u); err != nil {
			return nil, bound, nil, err
		}
	} else if err != nil {
		return nil, bound, nil, err
	}
	if u.Disabled {
		return nil, bound, nil, ErrForbidden
	}
	if err := s.Store.UpdateUserLogin(ctx, u.ID, time.Now()); err != nil {
		return nil, bound, nil, err
	}
	return u, bound, nil, nil
}

// takePending 取出并删除一次性 state；流程类型由 pending 记录的 BindUserID 决定。
func (s *OIDCService) takePending(providerName, state string) (*pendingAuth, error) {
	s.mu.Lock()
	pa, ok := s.pending[state]
	if ok {
		delete(s.pending, state) // state 一次性，无论后续校验是否通过
	}
	s.mu.Unlock()
	if !ok || pa.ProviderName != providerName || time.Since(pa.CreatedAt) > pendingAuthTTL {
		return nil, fmt.Errorf("state 无效或已过期")
	}
	return pa, nil
}

// TakePendingIsBind 消费 state 并返回是否绑定流程。IdP 错误回跳后该流程已失败，
// state 同样一次性作废（否则 10 分钟内仍可被 Finish 带合法 code 复用）。
func (s *OIDCService) TakePendingIsBind(state string) bool {
	s.mu.Lock()
	pa, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()
	return ok && pa.BindUserID != 0
}

// stageBindConfirm 登记待确认绑定并返回确认令牌。
func (s *OIDCService) stageBindConfirm(userID int64, provider string, claims *OIDCUser) *BindConfirmation {
	token := RandomToken(32)
	display := firstNonEmpty(claims.Name, claims.Email, claims.Subject)
	s.mu.Lock()
	// 清理过期确认项
	now := time.Now()
	for k, v := range s.bindConfirm {
		if now.Sub(v.CreatedAt) > pendingAuthTTL {
			delete(s.bindConfirm, k)
		}
	}
	s.bindConfirm[token] = &bindConfirmReq{
		UserID: userID, Provider: provider, Subject: claims.Subject,
		Display: display, CreatedAt: now,
	}
	s.mu.Unlock()
	return &BindConfirmation{Token: token, Provider: provider, Identity: display}
}

// BindConfirmInfo 查询待确认绑定（校验归属当前用户，不消费）。
func (s *OIDCService) BindConfirmInfo(userID int64, token string) (*BindConfirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.bindConfirm[token]
	if !ok || req.UserID != userID || time.Since(req.CreatedAt) > pendingAuthTTL {
		return nil, fmt.Errorf("确认请求无效或已过期")
	}
	return &BindConfirmation{Token: token, Provider: req.Provider, Identity: req.Display}, nil
}

// ConfirmBind 确认绑定生效：校验归属与时效后落库并消费确认令牌。
// 校验失败不消费（避免知道 token 的第三方抢先作废发起人的确认）；
// 确认期间身份可能已被他人绑定（ErrOIDCTaken）；发起用户可能已被删除
// （store 层 ErrNotFound）。
func (s *OIDCService) ConfirmBind(ctx context.Context, token string, userID int64) error {
	s.mu.Lock()
	req, ok := s.bindConfirm[token]
	if ok && req.UserID == userID && time.Since(req.CreatedAt) <= pendingAuthTTL {
		delete(s.bindConfirm, token) // 校验通过才消费（一次性）
	} else {
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("确认请求无效或已过期")
	}
	existing, err := s.Store.UserByOIDC(ctx, req.Provider, req.Subject)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if existing != nil && existing.ID != userID {
		return ErrOIDCTaken
	}
	if existing == nil {
		if err := s.Store.SetUserOIDC(ctx, userID, req.Provider, req.Subject); err != nil {
			return err
		}
	}
	return nil
}

// finishOIDC 回调公共段：code 换 token、验证 ID Token、提取 claims 并校验 claim 规则。
func (s *OIDCService) finishOIDC(ctx context.Context, p *store.OIDCProvider, code, verifier string) (*OIDCUser, error) {
	prov, err := oidc.NewProvider(ctx, normalizeIssuer(p.Issuer))
	if err != nil {
		return nil, fmt.Errorf("发现 IdP 配置失败: %w", err)
	}
	oc := oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     prov.Endpoint(),
		Scopes:       strings.Fields(p.Scopes),
	}
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("code 换 token 失败: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("IdP 未返回 id_token")
	}
	idToken, err := prov.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("id_token 校验失败: %w", err)
	}
	var c struct {
		Subject string `json:"sub"`
		Name    string `json:"name"`
		Email   string `json:"email"`
	}
	if err := idToken.Claims(&c); err != nil {
		return nil, err
	}
	if err := checkClaimRules(p.ClaimRules, c.Email, idToken); err != nil {
		return nil, err
	}
	return &OIDCUser{Subject: c.Subject, Name: c.Name, Email: c.Email}, nil
}

// autoProvisionAllowed OIDC 首次登录自动建号与本地注册共用 registration.enabled 开关。
func (s *OIDCService) autoProvisionAllowed(ctx context.Context) bool {
	v, err := s.Store.GetSetting(ctx, "registration.enabled")
	return err == nil && v == "true"
}

// defaultOIDCRole 读取 OIDC 首次登录自动建号的默认角色设置；非法或缺省回 viewer。
func (s *OIDCService) defaultOIDCRole(ctx context.Context) string {
	v, err := s.Store.GetSetting(ctx, "oidc.default_role")
	if err == nil && (v == "user" || v == "viewer") {
		return v
	}
	return "viewer"
}

// normalizeIssuer 去掉 issuer 的首尾空白与尾部斜杠：go-oidc 拿它与 IdP 发现文档的
// issuer 字段做严格字符串比较，录入时多打的斜杠会导致 "did not match" 校验失败。
func normalizeIssuer(issuer string) string {
	return strings.TrimRight(strings.TrimSpace(issuer), "/")
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

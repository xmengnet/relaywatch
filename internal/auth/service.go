package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"relaywatch/internal/store"
)

// Service 认证服务：本地登录、会话签发、API Token 校验。
type Service struct {
	st         store.Store
	sessionTTL time.Duration
}

func NewService(st store.Store, sessionTTL time.Duration) *Service {
	return &Service{st: st, sessionTTL: sessionTTL}
}

// HasUsers 判断是否需要首次引导。
func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	n, err := s.st.UserCount(ctx)
	return n > 0, err
}

// Setup 创建首个管理员账户，仅当用户表为空时允许。
func (s *Service) Setup(ctx context.Context, username, password string) (*store.User, error) {
	has, err := s.HasUsers(ctx)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, ErrSetupClosed
	}
	return s.createUser(ctx, username, password, "admin")
}

// Login 本地账户登录，成功后签发会话。
func (s *Service) Login(ctx context.Context, username, password, userAgent, ip string) (*store.User, *store.Session, error) {
	u, err := s.st.UserByUsername(ctx, username)
	if err == store.ErrNotFound {
		// 用户不存在也跑一次 bcrypt，避免时序差异枚举用户名
		ComparePassword([]byte("$2a$12$C6UzMDM.H6dfI/f/IKcEeO7ZBpQ0N1J9rS0QGimFzP0y50W1Gq0Ru"), password)
		return nil, nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, nil, err
	}
	if u.Disabled || !ComparePassword(u.PasswordHash, password) {
		return nil, nil, ErrInvalidCredentials
	}
	if err := s.st.UpdateUserLogin(ctx, u.ID, time.Now()); err != nil {
		return nil, nil, err
	}
	sess, err := s.createSession(ctx, u.ID, userAgent, ip)
	if err != nil {
		return nil, nil, err
	}
	return u, sess, nil
}

func (s *Service) createUser(ctx context.Context, username, password, role string) (*store.User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &store.User{
		Username:     username,
		PasswordHash: hash,
		Role:         role,
	}
	if err := s.st.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) createSession(ctx context.Context, userID int64, userAgent, ip string) (*store.Session, error) {
	sess := &store.Session{
		ID:        RandomToken(32),
		UserID:    userID,
		CSRFToken: RandomToken(24),
		UserAgent: userAgent,
		IP:        ip,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.sessionTTL),
	}
	if err := s.st.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// CreateSessionFor 为已认证用户直接签发会话（首次引导后自动登录）。
func (s *Service) CreateSessionFor(ctx context.Context, userID int64, userAgent, ip string) (*store.Session, error) {
	return s.createSession(ctx, userID, userAgent, ip)
}

// Logout 吊销会话。
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	return s.st.RevokeSession(ctx, sessionID)
}

// CreateAPIToken 生成明文 token 并存哈希。明文只在返回值中出现一次。
func (s *Service) CreateAPIToken(ctx context.Context, userID int64, name string, expires *time.Time) (plain string, t *store.APIToken, err error) {
	plain = "rw_" + RandomToken(32)
	sum := sha256.Sum256([]byte(plain))
	t = &store.APIToken{
		UserID:    userID,
		Name:      name,
		TokenHash: hex.EncodeToString(sum[:]),
		CreatedAt: time.Now(),
		ExpiresAt: expires,
	}
	if err := s.st.CreateAPIToken(ctx, t); err != nil {
		return "", nil, err
	}
	return plain, t, nil
}

// AuthByAPIToken 用 Bearer Token 换取用户。
func (s *Service) AuthByAPIToken(ctx context.Context, raw string) (*store.User, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
	if raw == "" {
		return nil, ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(raw))
	t, err := s.st.APITokenByHash(ctx, hex.EncodeToString(sum[:]))
	if err == store.ErrNotFound {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if t.RevokedAt != nil || (t.ExpiresAt != nil && t.ExpiresAt.Before(now)) {
		return nil, ErrInvalidToken
	}
	u, err := s.st.UserByID(ctx, t.UserID)
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, ErrInvalidToken
	}
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.st.TouchAPIToken(ctx2, t.ID)
	}()
	return u, nil
}

// UserBySession 校验会话有效性并返回用户与 CSRF token。
func (s *Service) UserBySession(ctx context.Context, sessionID string) (*store.User, *store.Session, error) {
	if sessionID == "" {
		return nil, nil, ErrInvalidSession
	}
	sess, err := s.st.SessionByID(ctx, sessionID)
	if err == store.ErrNotFound {
		return nil, nil, ErrInvalidSession
	}
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	if sess.RevokedAt != nil || sess.ExpiresAt.Before(now) {
		return nil, nil, ErrInvalidSession
	}
	u, err := s.st.UserByID(ctx, sess.UserID)
	if err != nil {
		return nil, nil, err
	}
	if u.Disabled {
		return nil, nil, ErrInvalidSession
	}
	return u, sess, nil
}

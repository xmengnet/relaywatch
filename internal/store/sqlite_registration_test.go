package store

import (
	"testing"
	"time"
)

func TestRegistrationCodeLifecycle(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	now := time.Now()

	if err := s.PutRegistrationCode(ctx, "a@example.com", "111111", now.Add(10*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistrationCode(ctx, "b@example.com", "222222", now.Add(10*time.Minute), now); err != nil {
		t.Fatal(err)
	}

	// 两个邮箱互不覆盖
	ra, ok, err := s.GetRegistrationCode(ctx, "a@example.com")
	if err != nil || !ok || ra.Code != "111111" {
		t.Fatalf("a code = %+v ok=%v err=%v", ra, ok, err)
	}
	rb, ok, err := s.GetRegistrationCode(ctx, "b@example.com")
	if err != nil || !ok || rb.Code != "222222" {
		t.Fatalf("b code = %+v ok=%v err=%v", rb, ok, err)
	}

	// 错误验证码不消费
	if consumed, err := s.ConsumeRegistrationCode(ctx, "a@example.com", "000000", now); err != nil || consumed {
		t.Fatalf("wrong code consumed=%v err=%v", consumed, err)
	}
	// 正确验证码只能消费一次
	if consumed, err := s.ConsumeRegistrationCode(ctx, "a@example.com", "111111", now); err != nil || !consumed {
		t.Fatalf("first consume=%v err=%v", consumed, err)
	}
	if consumed, err := s.ConsumeRegistrationCode(ctx, "a@example.com", "111111", now); err != nil || consumed {
		t.Fatalf("second consume=%v err=%v", consumed, err)
	}

	// 过期验证码不可消费
	if err := s.PutRegistrationCode(ctx, "c@example.com", "333333", now.Add(-time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if consumed, err := s.ConsumeRegistrationCode(ctx, "c@example.com", "333333", now); err != nil || consumed {
		t.Fatalf("expired consume=%v err=%v", consumed, err)
	}

	// 覆盖写：同邮箱新码替换旧码
	if err := s.PutRegistrationCode(ctx, "a@example.com", "444444", now.Add(10*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	ra, ok, err = s.GetRegistrationCode(ctx, "a@example.com")
	if err != nil || !ok || ra.Code != "444444" {
		t.Fatalf("overwritten code = %+v ok=%v err=%v", ra, ok, err)
	}
}

// 过期清理只删过期行，不影响有效记录。
func TestSweepRegistrationCodes(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	now := time.Now()
	if err := s.PutRegistrationCode(ctx, "old@example.com", "1", now.Add(-time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistrationCode(ctx, "new@example.com", "2", now.Add(10*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if err := s.SweepRegistrationCodes(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetRegistrationCode(ctx, "old@example.com"); ok {
		t.Fatal("expired code should be swept")
	}
	if _, ok, _ := s.GetRegistrationCode(ctx, "new@example.com"); !ok {
		t.Fatal("valid code should survive sweep")
	}
}

package compare

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"relaywatch/internal/store"
)

// EnqueueFunc 对比事件入队函数签名。
type EnqueueFunc func(ctx context.Context, follower *store.Monitor, ref *store.Monitor, event EventType) error

// Evaluator 双轨对比评估器。
type Evaluator struct {
	Store   store.Store
	Enqueue EnqueueFunc

	mu      sync.Mutex
	running map[int64]bool // 保证同一 followerID 在内存中不并发评估
}

func NewEvaluator(st store.Store, enqueue EnqueueFunc) *Evaluator {
	return &Evaluator{
		Store:   st,
		Enqueue: enqueue,
		running: map[int64]bool{},
	}
}

func (e *Evaluator) tryLock(id int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[id] {
		return false
	}
	e.running[id] = true
	return true
}

func (e *Evaluator) unlock(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.running, id)
}

// EvaluateOnChecked 在任意监控完成一次检查后被调用（可挂在 Checker 钩子上）。
func (e *Evaluator) EvaluateOnChecked(ctx context.Context, m *store.Monitor) {
	if m == nil {
		return
	}
	// 1. 若自身配置了参照监控，作为 follower 评估
	if m.ReferenceMonitorID != nil {
		if err := e.EvaluatePair(ctx, m.ID); err != nil {
			slog.Error("evaluate pair (as follower)", "follower_id", m.ID, "err", err)
		}
	}
	// 2. 若自身被其他监控引用，作为 reference 驱动所有 followers 评估
	followers, err := e.Store.ListFollowerMonitors(ctx, m.ID)
	if err == nil {
		for _, f := range followers {
			if err := e.EvaluatePair(ctx, f.ID); err != nil {
				slog.Error("evaluate pair (as reference)", "ref_id", m.ID, "follower_id", f.ID, "err", err)
			}
		}
	}
}

// EvaluatePair 重新加载并评估一条跟随监控的对比状态。
func (e *Evaluator) EvaluatePair(ctx context.Context, followerID int64) error {
	if !e.tryLock(followerID) {
		return nil // 正在评估中，防重入跳过
	}
	defer e.unlock(followerID)

	follower, err := e.Store.GetMonitor(ctx, followerID)
	if err != nil {
		return err
	}
	if follower.ReferenceMonitorID == nil {
		return nil
	}

	ref, err := e.Store.GetMonitor(ctx, *follower.ReferenceMonitorID)
	if err != nil {
		return fmt.Errorf("load reference monitor %d: %w", *follower.ReferenceMonitorID, err)
	}

	// 容错保护：任一侧不可用（未启用 / 错误 / 丢失 / 无版本）时跳过评估，保持原状态
	// 避免「落后 -> 上游故障 -> 恢复」期间误报追平
	if !follower.Enabled || follower.State == store.StateError || follower.State == store.StateMissing || follower.LatestVersion == "" {
		return nil
	}
	if !ref.Enabled || ref.State == store.StateError || ref.State == store.StateMissing || ref.LatestVersion == "" {
		return nil
	}

	newState, err := ComputeState(follower.LatestVersion, ref.LatestVersion, follower.SourceType, ref.SourceType)
	if err != nil {
		return fmt.Errorf("compute compare state: %w", err)
	}

	event, shouldNotify := DecideEvent(follower.CompareState, newState, ref.LatestVersion, follower.CompareNotifiedRef)
	compareNotifiedRef := follower.CompareNotifiedRef
	if shouldNotify && e.Enqueue != nil {
		if err := e.Enqueue(ctx, follower, ref, event); err != nil {
			slog.Error("enqueue compare notification failed", "follower_id", follower.ID, "event", event, "err", err)
		} else {
			if event == EventEnteredLagging || event == EventGapGrew {
				compareNotifiedRef = ref.LatestVersion
			}
		}
	}

	now := time.Now()
	var compareSince *time.Time
	if newState != follower.CompareState {
		compareSince = &now
	} else {
		compareSince = follower.CompareSince
	}

	return e.Store.UpdateMonitorCompare(ctx, follower.ID, newState, ref.LatestVersion, compareSince, compareNotifiedRef)
}

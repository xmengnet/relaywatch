// Package check 实现检查编排与状态机。
package check

import (
	"regexp"

	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

func compile(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

var compare = source.CompareVersions

// VersionFilter 应用 include/ignore 正则。
// 返回 skipped=true 表示该版本被过滤（记入历史但不改变状态）。
func VersionFilter(version, include, ignore string) (skipped bool, err error) {
	if include != "" {
		re, err := compile(include)
		if err != nil {
			return false, err
		}
		if re != nil && !re.MatchString(version) {
			return true, nil
		}
	}
	if ignore != "" {
		re, err := compile(ignore)
		if err != nil {
			return false, err
		}
		if re != nil && re.MatchString(version) {
			return true, nil
		}
	}
	return false, nil
}

// Decision 是状态机对一次成功检查的裁决（纯函数，便于表驱动测试）。
type Decision struct {
	State               store.MonitorState
	CurrentVersion      string // 基线（回退时不动）
	LatestVersion       string // 跟随上游
	Change              *ChangeRecord
	LastError           string
	ConsecutiveFailures int // 成功时为 0
}

// ChangeRecord 需要记录的版本变更。
type ChangeRecord struct {
	Kind       string // update | rollback | bump（周期内连发）
	OldVersion string
	NewVersion string
	Notify     bool // 是否触发投递
}

// Decide 根据检查结果与监控当前状态做出裁决。
// baseline 是 current_version，latest 是 latest_version，openChange 表示存在未确认变更。
// version 已规范化并经过滤。
func Decide(m *store.Monitor, version string, openChange *store.VersionChange) Decision {
	d := Decision{
		State:               m.State,
		CurrentVersion:      m.CurrentVersion,
		LatestVersion:       version,
		ConsecutiveFailures: 0,
	}

	// 无基线：首次检查，建立基线，不通知
	if m.CurrentVersion == "" {
		d.State = store.StateUpToDate
		d.CurrentVersion = version
		d.LatestVersion = version
		return d
	}

	c, err := compare(m.CurrentVersion, version)
	if err != nil {
		// 比较本身不应失败（version.go 保证回退），防御性处理
		d.State = store.StateError
		d.LastError = "版本比较失败: " + err.Error()
		d.ConsecutiveFailures = m.ConsecutiveFailures + 1
		return d
	}

	switch {
	case c == 0:
		// 与基线相同
		d.State = store.StateUpToDate
		d.LatestVersion = version

	case c < 0:
		// 上游更高 → has_update
		d.State = store.StateHasUpdate
		d.LatestVersion = version
		if openChange != nil {
			// 周期内连发：更新变更记录，不重复投递（静默期语义在投递层处理）
			d.Change = &ChangeRecord{Kind: "bump", OldVersion: openChange.NewVersion, NewVersion: version, Notify: false}
		} else {
			d.Change = &ChangeRecord{Kind: "update", OldVersion: m.CurrentVersion, NewVersion: version, Notify: true}
		}

	default:
		// 上游回退：latest 跟随下调，基线不动，状态不变，不通知
		if version != m.LatestVersion {
			d.Change = &ChangeRecord{Kind: "rollback", OldVersion: m.LatestVersion, NewVersion: version, Notify: false}
		}
		// 状态保持：up_to_date / has_update / error / missing 维持原值
		if m.State == store.StateUnknown {
			d.State = store.StateUpToDate
		}
	}
	return d
}

// FailureDecision 检查失败时的裁决。
func FailureDecision(m *store.Monitor, checkErr string, threshold int, notFound bool) Decision {
	failures := m.ConsecutiveFailures + 1
	d := Decision{
		State:               m.State,
		CurrentVersion:      m.CurrentVersion,
		LatestVersion:       m.LatestVersion,
		LastError:           checkErr,
		ConsecutiveFailures: failures,
	}
	switch {
	case notFound:
		// 上游明确不存在：立即 missing（可恢复，恢复后自动回 up_to_date）
		d.State = store.StateMissing
		d.ConsecutiveFailures = 0
	case m.State == store.StateMissing:
		// missing 期间的一般错误：保持 missing（上游还在消失状态）
	case failures >= threshold && m.State != store.StateError:
		d.State = store.StateError
	}
	return d
}

// RecoverDecision 检查成功且此前处于 error/missing 时的恢复裁决。
// 直接复用 Decide 的逻辑即可（Decide 已处理所有状态迁移），
// 此函数仅为语义清晰保留文档。

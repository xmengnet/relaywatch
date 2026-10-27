package compare

import (
	"relaywatch/internal/source"
)

const (
	StateNone    = ""
	StateInSync  = "in_sync"
	StateLagging = "lagging"
	StateAhead   = "ahead"
)

type EventType string

const (
	EventEnteredLagging EventType = "entered_lagging" // 首次进入落后
	EventGapGrew        EventType = "gap_grew"        // 仍落后且上游版本进一步变大
	EventCaughtUp       EventType = "caught_up"       // 从落后追平
)

// ComputeState 比较跟随者版本与参照者版本，返回对比状态。
// followerVer < refVer -> lagging
// followerVer == refVer -> in_sync
// followerVer > refVer -> ahead
func ComputeState(followerVer, refVer, followerType, refType string) (string, error) {
	af := Align(followerVer, followerType)
	ar := Align(refVer, refType)

	cmp, err := source.CompareVersions(af, ar)
	if err != nil {
		return StateNone, err
	}
	switch {
	case cmp < 0:
		return StateLagging, nil
	case cmp == 0:
		return StateInSync, nil
	default:
		return StateAhead, nil
	}
}

// DecideEvent 根据对比状态变化与上游版本判定是否产生通知事件。
func DecideEvent(oldState, newState string, refVer, lastNotifiedRef string) (EventType, bool) {
	switch {
	case oldState != StateLagging && newState == StateLagging:
		return EventEnteredLagging, true
	case oldState == StateLagging && newState == StateLagging:
		if refVer != "" && refVer != lastNotifiedRef {
			return EventGapGrew, true
		}
		return "", false
	case oldState == StateLagging && newState == StateInSync:
		return EventCaughtUp, true
	default:
		return "", false
	}
}

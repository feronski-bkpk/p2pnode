package tunnel

import "fmt"

type State int

const (
	StateBuilding State = iota

	StateActive

	StateDegraded

	StateClosing

	StateDead
)

func (s State) String() string {
	switch s {
	case StateBuilding:
		return "BUILDING"
	case StateActive:
		return "ACTIVE"
	case StateDegraded:
		return "DEGRADED"
	case StateClosing:
		return "CLOSING"
	case StateDead:
		return "DEAD"
	default:
		return fmt.Sprintf("STATE(%d)", int(s))
	}
}

func (s State) IsTerminal() bool {
	return s == StateDead
}

func (s State) CanSend() bool {
	return s == StateActive || s == StateDegraded
}

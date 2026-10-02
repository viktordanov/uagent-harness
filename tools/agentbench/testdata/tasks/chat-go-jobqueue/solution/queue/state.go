package queue

import (
	"fmt"
	"strings"
)

// State is where a job is in its life.
type State string

// The states of a job. A new job is Pending; a worker that claims it makes it
// Running; it ends Done when its handler succeeds, or Dead, the dead-letter
// state, when it has used up its attempts. A failed attempt with attempts
// left puts it back to Pending, to run again after the retry delay.
const (
	Pending State = "pending"
	Running State = "running"
	Done    State = "done"
	Dead    State = "dead"
)

// legacyFailed is the name Dead had before the dead-letter state: store files
// written then still load, and their failed jobs are dead.
const legacyFailed = "failed"

// states lists every state in the order a job passes through them. ParseState,
// the store's counts, and the CLI's help all read it.
var states = []State{Pending, Running, Done, Dead}

// States returns every state, in the order a job passes through them.
func States() []State {
	return append([]State(nil), states...)
}

// ParseState returns the state named s, ignoring case and surrounding space.
func ParseState(s string) (State, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	if name == legacyFailed {
		return Dead, nil
	}
	for _, st := range states {
		if string(st) == name {
			return st, nil
		}
	}
	return "", fmt.Errorf("unknown state %q (want one of %s)", s, StateNames())
}

// StateNames returns the names of all states joined by ", ", for messages.
func StateNames() string {
	names := make([]string, len(states))
	for i, st := range states {
		names[i] = string(st)
	}
	return strings.Join(names, ", ")
}

// Valid reports whether s is one of the known states.
func (s State) Valid() bool {
	for _, st := range states {
		if s == st {
			return true
		}
	}
	return false
}

// Terminal reports whether a job in state s is finished: no worker will claim
// it again unless it is requeued.
func (s State) Terminal() bool {
	return s == Done || s == Dead
}

// String returns the state's name.
func (s State) String() string { return string(s) }

// MarshalText writes the state's name. It refuses an unknown state so a
// corrupt job is never saved.
func (s State) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("unknown state %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText reads a state's name, as ParseState does.
func (s *State) UnmarshalText(b []byte) error {
	st, err := ParseState(string(b))
	if err != nil {
		return err
	}
	*s = st
	return nil
}

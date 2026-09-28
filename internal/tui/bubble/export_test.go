package bubble

// Busy reports whether the state has a run or a message on its way, for
// the tests' driver: the screen cannot tell, since "λ Starting" between a
// run's end and the session's Idle carries no "esc to interrupt".
func (m Model) Busy() bool { return m.st.Busy }

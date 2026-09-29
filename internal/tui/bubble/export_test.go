package bubble

// Busy reports whether the state has a run or a message on its way, for
// the tests' driver: the screen cannot tell, since "λ Starting" between a
// run's end and the session's Idle carries no "esc to interrupt".
func (m Model) Busy() bool { return m.st.Busy }

// Draft is the composer's text.
func (m Model) Draft() string { return m.composer.Value() }

// Attached lists the labels of the draft's images.
func (m Model) Attached() []string {
	labels := make([]string, 0, len(m.st.Attached))
	for _, img := range m.st.Attached {
		labels = append(labels, img.Label)
	}

	return labels
}

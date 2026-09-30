package app

// Close stops the work sushi does in the background: watching directories
// and searching. Call it once the program has finished.
func (m Model) Close() {
	m.watch.stop()
	m.stopFind()
}

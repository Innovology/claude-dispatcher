package usage

// forgetScans drops every remembered reading, as a fresh process starts. Only
// tests need it: a process's readings live as long as it does.
func forgetScans() {
	scanMu.Lock()
	scans = map[string]*fileScan{}
	scanMu.Unlock()
}

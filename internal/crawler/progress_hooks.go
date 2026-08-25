// Package crawler progress helpers invoke optional ProgressCallbacks on Config.
package crawler

func (c *Config) emitPhase(phase string) {
	if c == nil || c.Progress == nil || c.Progress.OnPhase == nil {
		return
	}
	c.Progress.OnPhase(phase)
}

func (c *Config) emitProgress(current, total int) {
	if c == nil || c.Progress == nil || c.Progress.OnProgress == nil {
		return
	}
	c.Progress.OnProgress(current, total)
}

func (c *Config) emitDownload(destPath string) error {
	if c == nil || c.Progress == nil || c.Progress.OnDownload == nil {
		return nil
	}
	return c.Progress.OnDownload(destPath)
}

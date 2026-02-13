package core

func (c *Config) SetExecutor(exec SystemExecutor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.executor = exec
}

func (c *Config) getExecutor() SystemExecutor {
	// c.mu.Lock() // Avoid lock here if possible or be careful with deadlocks if caller holds lock
	// Since executor is set once at startup, we might get away with no lock or atomic value,
	// but to be safe and simple, let's assume it's accessed where safe.
	// Actually, most Config methods lock c.mu. We cannot lock again.
	// We should just access c.executor since we are inside locked methods usually.
	return c.executor
}

package config

// PinFader sets pages.main.faders[ch] = key, creating the page and map as needed.
func (c *Config) PinFader(ch int, key string) {
	c.pagesMu.Lock()
	defer c.pagesMu.Unlock()
	page := c.ensureMainPageLocked()
	k := key
	page.Faders[ch] = &k
	c.Pages["main"] = page
}

// UnpinFader removes pages.main.faders[ch] if it currently matches key.
func (c *Config) UnpinFader(ch int, key string) {
	c.pagesMu.Lock()
	defer c.pagesMu.Unlock()
	if c.Pages == nil {
		return
	}
	page, ok := c.Pages["main"]
	if !ok {
		return
	}
	if existing := page.Faders[ch]; existing != nil && *existing == key {
		delete(page.Faders, ch)
		c.Pages["main"] = page
	}
}

func (c *Config) ensureMainPageLocked() PageConfig {
	if c.Pages == nil {
		c.Pages = make(map[string]PageConfig)
	}
	page := c.Pages["main"]
	if page.Faders == nil {
		page.Faders = make(map[int]*string)
	}
	return page
}

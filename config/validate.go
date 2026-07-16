package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Validate checks the config for semantic errors.
func (c *Config) Validate() error {
	for key, dev := range c.Devices {
		if err := validateDeviceConfig(dev, key); err != nil {
			return err
		}
		if dev.Knob != nil {
			if err := c.validateKnobConfig(*dev.Knob, "device "+key+" knob"); err != nil {
				return err
			}
		}
	}
	if err := c.validateDefaultKnobs(); err != nil {
		return err
	}
	if err := c.validatePageSlots(); err != nil {
		return err
	}
	if err := c.validateExecTargets(); err != nil {
		return err
	}
	return c.ValidateRouter(8)
}

func validateDeviceConfig(d DeviceConfig, key string) error {
	switch d.Type {
	case BindInput, BindPlayback, BindOutput:
	default:
		return fmt.Errorf("device %s: unknown type %q", key, d.Type)
	}
	if d.MatchRegex != "" {
		if _, err := regexp.Compile("(?i)" + d.MatchRegex); err != nil {
			return fmt.Errorf("device %s: invalid match-regex %q: %w", key, d.MatchRegex, err)
		}
	}
	return nil
}

func (c *Config) validateDefaultKnobs() error {
	for _, def := range []struct {
		label string
		knob  KnobConfig
	}{
		{label: "defaults.input-knob", knob: c.Defaults.InputKnob},
		{label: "defaults.playback-knob", knob: c.Defaults.PlaybackKnob},
		{label: "defaults.output-knob", knob: c.Defaults.OutputKnob},
	} {
		if def.knob.Type == "" {
			continue
		}
		if err := c.validateKnobConfig(def.knob, def.label); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateKnobConfig(k KnobConfig, loc string) error {
	switch k.Type {
	case KnobGain, KnobSend, KnobNone, "":
	default:
		return fmt.Errorf("%s: unknown knob type %q", loc, k.Type)
	}
	if !k.IsSend() {
		return nil
	}
	if err := c.validateSendBus(k.BusA, loc, "bus-a"); err != nil {
		return err
	}
	return c.validateSendBus(k.BusB, loc, "bus-b")
}

func (c *Config) validateSendBus(key, loc, field string) error {
	if key == "" {
		return nil
	}
	if c.DeviceFor(key) == nil {
		return fmt.Errorf("%s: %s device %q not found in devices", loc, field, key)
	}
	return nil
}

func (c *Config) validatePageSlots() error {
	for pageName, page := range c.Pages {
		if err := c.validateSlotMap(pageName, "fader", page.Faders); err != nil {
			return err
		}
		if err := c.validateSlotMap(pageName, "knob", page.Knobs); err != nil {
			return err
		}
		if err := c.validateSlotMap(pageName, "channel", page.Channels); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateSlotMap(pageName, slotName string, slots map[int]*string) error {
	for pos, key := range slots {
		if key == nil || *key == "" {
			continue
		}
		if _, ok := c.Devices[*key]; !ok {
			return fmt.Errorf("page %s %s %d: unknown device %q", pageName, slotName, pos, *key)
		}
	}
	return nil
}

func (c *Config) validateExecTargets() error {
	for key, target := range c.Exec {
		if target.Command == "" {
			return fmt.Errorf("exec %s: command is required", key)
		}
		if len(target.Scale) != 0 && len(target.Scale) != 2 {
			return fmt.Errorf("exec %s: scale must contain exactly two numbers", key)
		}
	}
	return nil
}

// ValidateRouter validates router assignments against the provided surface strip count.
func (c *Config) ValidateRouter(strips int) error {
	routerRules := make(map[string]string)
	for strip, assignment := range c.Router.Assignments {
		if strip < 0 || strip >= strips {
			return fmt.Errorf("router assignment %d: strip outside surface range 0..%d", strip, strips-1)
		}
		if err := c.validateRouterAssignment(fmt.Sprintf("router assignment %d", strip), assignment); err != nil {
			return err
		}
		if strings.HasPrefix(assignment.Target, "pipewire:rule/") {
			routerRules[strings.TrimPrefix(assignment.Target, "pipewire:rule/")] = "base"
		}
	}
	legacyButtons := make(map[string]string)
	for name, page := range c.Pages {
		if page.Button != "" {
			legacyButtons[page.Button] = name
		}
	}
	routerButtons := make(map[string]string)
	for i, page := range c.Router.Pages {
		loc := fmt.Sprintf("router page %d", i)
		if page.Name == "" {
			return fmt.Errorf("%s: name is required", loc)
		}
		if page.Button == "" {
			return fmt.Errorf("router page %q: button is required", page.Name)
		}
		if legacy, ok := legacyButtons[page.Button]; ok {
			return fmt.Errorf("router page %q: button %q collides with legacy page %q", page.Name, page.Button, legacy)
		}
		if prev, ok := routerButtons[page.Button]; ok {
			return fmt.Errorf("router page %q: button %q collides with router page %q", page.Name, page.Button, prev)
		}
		routerButtons[page.Button] = page.Name
		if len(page.Assignments) == 0 {
			return fmt.Errorf("router page %q: assignments are required", page.Name)
		}
		for j, assignment := range page.Assignments {
			if err := c.validateRouterAssignment(fmt.Sprintf("router page %q assignment %d", page.Name, j), assignment); err != nil {
				return err
			}
		}
	}
	for pageName, page := range c.Pages {
		for _, slots := range []map[int]*string{page.Faders, page.Knobs, page.Channels} {
			for _, key := range slots {
				if key == nil {
					continue
				}
				if routerPage, exists := routerRules[*key]; exists {
					return fmt.Errorf("device %q is controlled by router page %q and legacy page %q", *key, routerPage, pageName)
				}
			}
		}
	}
	return nil
}

func (c *Config) validateRouterAssignment(loc string, assignment AssignmentConfig) error {
	if assignment.Target == "" {
		return fmt.Errorf("%s: target is required", loc)
	}
	if assignment.Params == nil || len(assignment.Params) == 0 {
		return fmt.Errorf("%s: params are required", loc)
	}
	if err := c.validateRouterTarget(loc, assignment.Target); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateRouterTarget(loc, target string) error {
	switch {
	case strings.HasPrefix(target, "exec:"):
		key := strings.TrimPrefix(target, "exec:")
		if key == "" || c.Exec == nil {
			return fmt.Errorf("%s: unknown exec target %q", loc, key)
		}
		if _, ok := c.Exec[key]; !ok {
			return fmt.Errorf("%s: unknown exec target %q", loc, key)
		}
		return nil
	case strings.HasPrefix(target, "pipewire:node/"):
		if strings.TrimPrefix(target, "pipewire:node/") == "" {
			return fmt.Errorf("%s: invalid PipeWire node target %q", loc, target)
		}
		return nil
	case strings.HasPrefix(target, "pipewire:rule/"):
		key := strings.TrimPrefix(target, "pipewire:rule/")
		if _, ok := c.Devices[key]; !ok {
			return fmt.Errorf("%s: unknown PipeWire rule target %q", loc, key)
		}
		return nil
	default:
		return fmt.Errorf("%s: unsupported target %q", loc, target)
	}
}

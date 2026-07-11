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
	for strip, assignment := range c.Router.Assignments {
		if strip < 0 || strip >= strips {
			return fmt.Errorf("router assignment %d: strip outside surface range 0..%d", strip, strips-1)
		}
		if assignment.Target == "" {
			return fmt.Errorf("router assignment %d: target is required", strip)
		}
		if assignment.Params == nil || len(assignment.Params) == 0 {
			return fmt.Errorf("router assignment %d: params are required", strip)
		}
		if err := c.validateRouterTarget(strip, assignment.Target); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateRouterTarget(strip int, target string) error {
	const prefix = "exec:"
	if !strings.HasPrefix(target, prefix) || len(target) == len(prefix) {
		return fmt.Errorf("router assignment %d: unsupported target %q", strip, target)
	}
	key := strings.TrimPrefix(target, prefix)
	if c.Exec == nil {
		return fmt.Errorf("router assignment %d: unknown exec target %q", strip, key)
	}
	if _, ok := c.Exec[key]; !ok {
		return fmt.Errorf("router assignment %d: unknown exec target %q", strip, key)
	}
	return nil
}

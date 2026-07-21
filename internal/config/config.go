// Package config resolves named scan profiles: presets for technique
// selection, TLD profile, concurrency, timeout, QPS and candidate limit.
//
// A profile keeps the common case ("--profile quick") a single flag while
// leaving every knob it sets individually overridable. Precedence runs
// builtin profile < a same-named profile in the config file < environment
// (YATT_PROFILE) < an explicit --profile choosing a different name <
// whatever flag the user actually typed for a single knob — the last of
// those always wins, since a flag someone typed beats a preset they were
// merely handed.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Profile is a named preset of scan settings.
//
// A zero-valued field means "unset": it leaves whatever the flag's own
// default or an explicit override already supplies, rather than blanking it.
// That is what lets a config-file profile name only the fields it wants to
// change instead of restating every knob.
type Profile struct {
	Techniques  []string      `mapstructure:"techniques"`
	TLDProfile  string        `mapstructure:"tld_profile"`
	Concurrency int           `mapstructure:"concurrency"`
	QPS         float64       `mapstructure:"qps"`
	Timeout     time.Duration `mapstructure:"timeout"`
	Limit       int           `mapstructure:"limit"`
}

// Builtins are the profiles available with no config file at all.
//
// "quick" favours the three cheapest, lowest-fan-out techniques so a first
// scan comes back fast. "full" runs everything, including the two
// fan-out-heavy techniques (homoglyph, TLD swap against the whole IANA
// list), at higher concurrency to keep it from taking forever; it leaves
// Timeout and Limit unset, since a deliberately large scan should not also
// have its own candidate count capped or its per-query patience shortened
// by the preset that asked for more candidates in the first place.
var Builtins = map[string]Profile{
	"quick": {
		Techniques:  []string{"omission", "transposition", "keyboard"},
		TLDProfile:  "common",
		Concurrency: 20,
		Timeout:     3 * time.Second,
	},
	"full": {
		Techniques:  []string{"omission", "transposition", "keyboard", "homoglyph", "tld"},
		TLDProfile:  "full",
		Concurrency: 50,
	},
}

// Names returns the builtin profile names, sorted. It is what static help
// text uses — flags are declared before any --config is read, so that is all
// there is to list at that point; Config.Names extends this with whatever a
// loaded config file adds.
func Names() []string {
	names := make([]string, 0, len(Builtins))
	for name := range Builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Overlay returns a copy of p with every field the caller reports as changed
// cleared back to its zero value, so applying the result can never override a
// flag the user actually typed.
//
// changed is asked about the Profile field's own mapstructure tag
// ("techniques", "tld_profile", "concurrency", "qps", "timeout", "limit") —
// the same name a config-file author would use — leaving the mapping from
// tag to command-line flag name to the caller, since that mapping is a CLI
// concern this package has no business knowing.
func (p Profile) Overlay(changed func(field string) bool) Profile {
	out := p
	if changed("techniques") {
		out.Techniques = nil
	}
	if changed("tld_profile") {
		out.TLDProfile = ""
	}
	if changed("concurrency") {
		out.Concurrency = 0
	}
	if changed("qps") {
		out.QPS = 0
	}
	if changed("timeout") {
		out.Timeout = 0
	}
	if changed("limit") {
		out.Limit = 0
	}
	return out
}

// Config reads yatt's config file and resolves named profiles against it.
//
// The zero value is not usable; construct one with New.
type Config struct {
	v *viper.Viper
}

// New returns a Config with no file loaded yet: profile resolution falls
// back to Builtins alone until Load is called.
func New() *Config {
	v := viper.New()
	v.SetEnvPrefix("yatt")
	v.AutomaticEnv()
	return &Config{v: v}
}

// BindPFlags exposes flags to the config, so a bound flag's value takes
// precedence over the config file and environment for the same key exactly
// when the user actually set it — Viper only lets a bound flag win once it
// reports itself changed, and otherwise falls through to the environment and
// the config file beneath it.
func (c *Config) BindPFlags(flags *pflag.FlagSet) error {
	return c.v.BindPFlags(flags)
}

// Load reads the config file at path. An empty path is not an error — it
// means no config file was given, so profile resolution stays builtins-only —
// but a path that does not exist or does not parse is, since a mistyped
// --config should not be silently ignored.
func (c *Config) Load(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("--config: %w", err)
	}
	c.v.SetConfigFile(path)
	if err := c.v.ReadInConfig(); err != nil {
		return fmt.Errorf("--config: %w", err)
	}
	return nil
}

// ProfileName returns the active profile name, or empty when none is
// selected. It reads the "profile" key, which resolves through the chain a
// bound --profile flag, YATT_PROFILE, and a top-level "profile" key in the
// config file all feed into — in that order of precedence, courtesy of
// Viper.
func (c *Config) ProfileName() string {
	return strings.TrimSpace(c.v.GetString("profile"))
}

// Names returns every profile name this Config knows: the builtins, plus any
// name defined under "profiles" in the loaded config file, sorted.
func (c *Config) Names() []string {
	seen := make(map[string]bool, len(Builtins))
	var names []string
	for name := range Builtins {
		seen[name] = true
		names = append(names, name)
	}
	for name := range c.v.GetStringMap("profiles") {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Resolve returns the named profile: a builtin, if one exists, overridden
// field by field by a same-named profile under "profiles" in the config
// file, if any. Either alone is enough to make the name valid; neither makes
// it an error to name a profile the config file defines from scratch.
//
// An empty name resolves to the zero Profile, which changes nothing — that
// is how a scan with no profile selected behaves exactly as if profiles did
// not exist.
//
// The name is matched case-insensitively against builtins and config-file
// profiles alike. Viper already lowercases config keys, so without folding
// the builtin lookup too, "--profile QUICK" would miss the builtin while
// still matching a config-file "profiles.quick" section — overlaying the
// file's fields onto a zero Profile instead of the builtin preset.
func (c *Config) Resolve(name string) (Profile, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Profile{}, nil
	}

	profile, known := Builtins[name]
	key := "profiles." + name
	if c.v.IsSet(key) {
		if err := c.v.UnmarshalKey(key, &profile); err != nil {
			return Profile{}, fmt.Errorf("config: profile %q: %w", name, err)
		}
		known = true
	}
	if !known {
		return Profile{}, fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(c.Names(), ", "))
	}
	if err := profile.validate(name); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// validate rejects profile values that can only be a config-file mistake.
//
// The timeout check exists because a bare YAML number decodes into a
// time.Duration as nanoseconds — viper's duration hook converts only strings
// — so `timeout: 5` silently becomes five nanoseconds and every DNS query in
// the scan times out. No DNS timeout below a millisecond is plausible, so
// anything under it is diagnosed instead of applied.
func (p Profile) validate(name string) error {
	if p.Timeout != 0 && p.Timeout < time.Millisecond {
		return fmt.Errorf(
			"config: profile %q: timeout %v is less than 1ms — a bare number is read as nanoseconds; write a duration string such as \"5s\"",
			name, p.Timeout)
	}
	return nil
}

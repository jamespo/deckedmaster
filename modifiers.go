package main

import (
	"fmt"
	"sort"
	"strings"
)

// Modifiers is a set of keyboard modifier keys, held on a regular keyboard
// while a deck control is used. Left and right variants are not told apart.
type Modifiers uint8

const (
	ModShift Modifiers = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// modifierNames maps the accepted (case-insensitive) spellings in a deck
// config to the modifier they stand for.
var modifierNames = map[string]Modifiers{
	"shift":   ModShift,
	"ctrl":    ModCtrl,
	"control": ModCtrl,
	"alt":     ModAlt,
	"super":   ModSuper,
	"meta":    ModSuper,
	"win":     ModSuper,
	"logo":    ModSuper,
}

// String returns the canonical spelling of a modifier set, e.g. "ctrl+shift".
func (m Modifiers) String() string {
	if m == 0 {
		return "none"
	}
	var parts []string
	for _, p := range []struct {
		mod  Modifiers
		name string
	}{{ModCtrl, "ctrl"}, {ModAlt, "alt"}, {ModShift, "shift"}, {ModSuper, "super"}} {
		if m&p.mod != 0 {
			parts = append(parts, p.name)
		}
	}
	return strings.Join(parts, "+")
}

// ParseModifiers parses a modifier combination like "super", "ctrl+shift" or
// "Ctrl-Alt". Modifiers may be separated by "+" or "-", in any order.
func ParseModifiers(s string) (Modifiers, error) {
	var m Modifiers
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == '+' || r == '-' || r == ' '
	})
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty modifier combination '%s'", s)
	}
	for _, f := range fields {
		mod, ok := modifierNames[strings.ToLower(f)]
		if !ok {
			return 0, fmt.Errorf("unknown modifier '%s' in '%s' (use shift, ctrl, alt or super)", f, s)
		}
		m |= mod
	}
	return m, nil
}

// forModifiers picks the action to run when the given modifiers are held. An
// entry in a.Modifiers that matches the held modifiers exactly wins;
// otherwise the action itself is returned. The bool reports whether a
// modifier-specific action was chosen.
func (a *ActionConfig) forModifiers(held Modifiers) (*ActionConfig, bool) {
	if a == nil || held == 0 || len(a.Modifiers) == 0 {
		return a, false
	}

	// iterate in a stable order, so that duplicate spellings of the same
	// combination (rejected by Validate, but possible via the API) are at
	// least resolved deterministically
	combos := make([]string, 0, len(a.Modifiers))
	for combo := range a.Modifiers {
		combos = append(combos, combo)
	}
	sort.Strings(combos)

	for _, combo := range combos {
		m, err := ParseModifiers(combo)
		if err != nil {
			errorLog(err, "ignoring modifier action")
			continue
		}
		if m == held {
			return a.Modifiers[combo], true
		}
	}
	return a, false
}

// resolveModifiers returns the action to run for a, taking the modifier keys
// currently held on the keyboard into account. The keyboard is only queried
// when a actually has modifier-specific actions.
func (a *ActionConfig) resolveModifiers() (*ActionConfig, bool) {
	if a == nil || len(a.Modifiers) == 0 {
		return a, false
	}
	held := heldModifiers()
	resolved, ok := a.forModifiers(held)
	if ok {
		verboseLog("Modifiers %s held, using modifier action", held)
	}
	return resolved, ok
}

// validateModifiers checks the modifier-specific actions of a.
func (a *ActionConfig) validateModifiers() error {
	if a == nil {
		return nil
	}
	seen := map[Modifiers]string{}
	for combo, ma := range a.Modifiers {
		m, err := ParseModifiers(combo)
		if err != nil {
			return err
		}
		if prev, ok := seen[m]; ok {
			return fmt.Errorf("modifier combinations '%s' and '%s' are the same", prev, combo)
		}
		seen[m] = combo
		if ma != nil && len(ma.Modifiers) > 0 {
			return fmt.Errorf("modifier action '%s' can't have modifiers of its own", combo)
		}
	}
	return nil
}

// validateModifiers checks every action bound to a key.
func (k KeyConfig) validateModifiers() error {
	for name, a := range map[string]*ActionConfig{
		"action":              k.Action,
		"action_hold":         k.ActionHold,
		"action_rotate_left":  k.ActionRotateLeft,
		"action_rotate_right": k.ActionRotateRight,
	} {
		if err := a.validateModifiers(); err != nil {
			return fmt.Errorf("key %d %s: %w", k.Index, name, err)
		}
	}
	return nil
}

// validateModifiers checks the modifier actions of every key in a deck,
// including its per-window overrides.
func (c DeckConfig) validateModifiers() error {
	for _, k := range c.Keys {
		if err := k.validateModifiers(); err != nil {
			return err
		}
	}
	for _, w := range c.Windows {
		for _, k := range w.Keys {
			if err := k.validateModifiers(); err != nil {
				return err
			}
		}
	}
	return nil
}

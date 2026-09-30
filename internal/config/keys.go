package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// KeyList is the keys bound to one action under keys: in the config file,
// written as a list, [k, up], or as a single key on its own. An empty list
// leaves the action unbound. The app checks the key names, since only it
// knows the actions.
type KeyList []string

// UnmarshalYAML reads a single key as well as a list. Anything else is a
// type error, which the decoder notes while it carries on with the rest of
// the file, so one bad entry doesn't lose every other setting.
func (k *KeyList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*k = KeyList{node.Value}
		return nil
	case yaml.SequenceNode:
		// Not nil even when empty, as [] unbinds the action
		keys := make(KeyList, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return keyListError(item)
			}
			keys = append(keys, item.Value)
		}
		*k = keys
		return nil
	}
	return keyListError(node)
}

// keyListError describes a keys: entry that is neither a key nor a list
func keyListError(node *yaml.Node) error {
	return &yaml.TypeError{Errors: []string{fmt.Sprintf("line %d: want a key or a list of keys", node.Line)}}
}

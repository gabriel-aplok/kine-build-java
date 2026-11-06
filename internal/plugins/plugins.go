package plugins

import "fmt"

// Plugin is one validated plugin jar.
type Plugin struct {
	Jar      string
	Manifest Manifest
}

// Load reads plugins.toml and every plugin manifest.
func Load(workspaceRoot string) ([]Plugin, error) {
	jars, err := LoadConfig(workspaceRoot)
	if err != nil {
		return nil, err
	}

	plugins := make([]Plugin, 0, len(jars))
	pluginIDs := make(map[string]string, len(jars))
	providerIDs := make(map[string]string, len(jars))

	for _, jar := range jars {
		manifest, err := ReadManifest(jar)
		if err != nil {
			return nil, err
		}
		if other, ok := pluginIDs[manifest.ID]; ok {
			return nil, fmt.Errorf("plugin %s: duplicate plugin id %q (also %s)", jar, manifest.ID, other)
		}
		pluginIDs[manifest.ID] = jar

		for _, provider := range manifest.Providers {
			if other, ok := providerIDs[provider]; ok {
				return nil, fmt.Errorf("plugin %s: provider %q is already provided by %s", jar, provider, other)
			}
			providerIDs[provider] = jar
		}
		plugins = append(plugins, Plugin{Jar: jar, Manifest: manifest})
	}
	return plugins, nil
}

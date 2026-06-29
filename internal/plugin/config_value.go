package plugin

import "pulseops/internal/pluginconfig"

type ConfigValueValidationOptions = pluginconfig.ValidationOptions

func ValidateConfigValues(schema *ConfigSchema, classes map[string]ConfigClass, values map[string]any, opts ConfigValueValidationOptions) error {
	return pluginconfig.ValidateValues(schema, classes, values, opts)
}

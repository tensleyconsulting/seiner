// Package config loads process configuration from flags, Google's project
// and impersonation environment variables, and a YAML config file. Flags
// override the environment, which overrides the file, which overrides
// defaults. Seiner has no environment variables of its own.
package config

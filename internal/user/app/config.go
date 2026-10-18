package app

// Config is the user-service specific configuration, read from the "service"
// key of the YAML config. Shared settings live in gokit's config.Base.
// GOOGLE_CLIENT_ID and APPLE_CLIENT_ID env vars override the YAML values.
type Config struct {
	GoogleClientID string `yaml:"google_client_id" mapstructure:"google_client_id"`
	AppleClientID  string `yaml:"apple_client_id" mapstructure:"apple_client_id"`
}

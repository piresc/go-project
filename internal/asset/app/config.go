package app

// Config is the asset-service specific configuration, read from the
// "service" key of the YAML config. Shared settings (server, database, redis,
// auth) live in gokit's config.Base.
type Config struct {
	MarketBaseURL string `yaml:"market_base_url" mapstructure:"market_base_url"`
}

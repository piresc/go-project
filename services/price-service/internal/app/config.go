package app

// Config is the price-service specific configuration, read from the
// "service" key of the YAML config. Shared settings (server, redis, auth)
// live in gokit's config.Base.
type Config struct {
	CacheTTLSeconds int `yaml:"cache_ttl_seconds" mapstructure:"cache_ttl_seconds"`
}

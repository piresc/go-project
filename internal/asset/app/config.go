package app

// Config is the asset-service specific configuration, read from the
// "service" key of the YAML config. Shared settings (server, database, redis,
// auth) live in gokit's config.Base. The price-service client is configured
// under service.http_clients.price, which pkg/platform reads.
type Config struct{}

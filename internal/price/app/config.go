package app

// Config is the price-service specific configuration, read from the
// "service" key of the YAML config. The market client and the refresh
// schedule are platform settings (service.http_clients, service.tasks), so
// there is nothing service specific yet.
type Config struct{}

package platform

// Settings are the platform-level options every service can set under the
// "service:" key of its YAML config, next to the service's own fields.
type Settings struct {
	Auth   AuthSettings   `yaml:"auth" mapstructure:"auth"`
	Events EventsSettings `yaml:"events" mapstructure:"events"`
	// Tasks configures the asynq worker and scheduler. Used only when asynq
	// is enabled.
	Tasks TasksSettings `yaml:"tasks" mapstructure:"tasks"`
	// HTTPClients are named upstream clients, keyed by name. Each key must be
	// present in the YAML for environment variables to override it.
	HTTPClients map[string]HTTPClientSettings `yaml:"http_clients" mapstructure:"http_clients"`
	// Lock tunes the distributed Locker. Used only when Redis is configured.
	Lock LockSettings `yaml:"lock" mapstructure:"lock"`
}

// AuthSettings tunes the token middleware.
type AuthSettings struct {
	// FailOpen lets requests through when the revocation check errors.
	// The default (false) answers 503 with Retry-After: 5.
	FailOpen bool `yaml:"fail_open" mapstructure:"fail_open"`
	// RevocationRedis is a separate Redis that only token revocation reads,
	// for services whose own Redis is not the token issuer's. Used when host
	// is set. To override a field from the environment
	// (SERVICE_AUTH_REVOCATION_REDIS_HOST, ...) the key must be present in
	// the YAML.
	RevocationRedis RevocationRedisSettings `yaml:"revocation_redis" mapstructure:"revocation_redis"`
}

// RevocationRedisSettings addresses the revocation Redis.
type RevocationRedisSettings struct {
	Host     string `yaml:"host" mapstructure:"host"`
	Port     int    `yaml:"port" mapstructure:"port"`
	Password string `yaml:"password" mapstructure:"password"`
	DB       int    `yaml:"db" mapstructure:"db"`
}

// serviceConfig is the type read from the "service:" key. Settings and the
// service's own struct S share that key (squashed).
type serviceConfig[S any] struct {
	Settings `mapstructure:",squash"`
	App      S `mapstructure:",squash"`
}

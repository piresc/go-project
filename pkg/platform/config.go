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
	// SkipRevocationCheck disables the revocation check and the Revocations
	// store. For services that do not own the token issuer's Redis: a revoked
	// token stays valid there until it expires. Default false.
	SkipRevocationCheck bool `yaml:"skip_revocation_check" mapstructure:"skip_revocation_check"`
}

// serviceConfig is the type read from the "service:" key. Settings and the
// service's own struct S share that key (squashed).
type serviceConfig[S any] struct {
	Settings `mapstructure:",squash"`
	App      S `mapstructure:",squash"`
}

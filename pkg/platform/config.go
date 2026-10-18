package platform

// Settings are the platform-level options every service can set under the
// "service:" key of its YAML config, next to the service's own fields.
type Settings struct {
	Auth AuthSettings `yaml:"auth" mapstructure:"auth"`
}

// AuthSettings tunes the token middleware.
type AuthSettings struct {
	// FailOpen lets requests through when the revocation check errors.
	// The default (false) answers 503 with Retry-After: 5.
	FailOpen bool `yaml:"fail_open" mapstructure:"fail_open"`
}

// serviceConfig is the type read from the "service:" key. Settings and the
// service's own struct S share that key (squashed).
type serviceConfig[S any] struct {
	Settings `mapstructure:",squash"`
	App      S `mapstructure:",squash"`
}

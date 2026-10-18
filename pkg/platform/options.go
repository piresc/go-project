package platform

import (
	"embed"
	"fmt"
)

// Dependency is infrastructure a service can demand through Require.
type Dependency int

// Dependencies understood by Require.
const (
	Postgres Dependency = iota + 1
	Redis
	Events
	Tasks
	AuthKeys
)

func (d Dependency) String() string {
	switch d {
	case Postgres:
		return "postgres"
	case Redis:
		return "redis"
	case Events:
		return "events"
	case Tasks:
		return "tasks"
	case AuthKeys:
		return "auth keys"
	default:
		return fmt.Sprintf("dependency(%d)", int(d))
	}
}

// hint tells the operator which config makes the dependency available.
func (d Dependency) hint() string {
	switch d {
	case Postgres:
		return "set database.host"
	case Redis:
		return "set redis.host"
	case Events:
		return "set nats.enabled and nats.url"
	case Tasks:
		return "set asynq.enabled"
	case AuthKeys:
		return "set auth.public_key_path or JWT_PUBLIC_KEY_PEM"
	default:
		return "unknown dependency"
	}
}

type migrationSpec struct {
	fsys embed.FS
	dir  string
}

type options struct {
	require    []Dependency
	migrations *migrationSpec
}

// Option customises New.
type Option func(*options)

// WithMigrations runs the embedded SQL migrations in dir against the service
// database during New. It needs Postgres and fails New when database.host is
// not set.
func WithMigrations(fsys embed.FS, dir string) Option {
	return func(o *options) {
		o.migrations = &migrationSpec{fsys: fsys, dir: dir}
	}
}

// Require makes New fail, with an error naming the missing config, unless every
// listed dependency is configured.
func Require(deps ...Dependency) Option {
	return func(o *options) {
		o.require = append(o.require, deps...)
	}
}

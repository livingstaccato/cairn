module github.com/livingstaccato/cairn

go 1.26.0

require (
	github.com/bmatcuk/doublestar/v4 v4.10.0
	github.com/provide-io/provide-telemetry/go v0.9.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/kr/text v0.2.0 // indirect
)

// Superseded. Use v0.3.0 or later.
retract [v0.1.0, v0.2.1]

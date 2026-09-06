module github.com/livingstaccato/cairndex/exampleSite

go 1.26.0

require (
	github.com/livingstaccato/cairndex v0.0.0
	github.com/livingstaccato/cairndex/themes/reference v0.0.0
)

// The example lives inside the module it demonstrates, so both are resolved
// locally. A consumer drops the replace lines and takes a tagged version.
replace github.com/livingstaccato/cairndex => ../

replace github.com/livingstaccato/cairndex/themes/reference => ../themes/reference

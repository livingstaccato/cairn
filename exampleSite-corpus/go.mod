module github.com/livingstaccato/cairndex/exampleSite-corpus

go 1.26.0

// The example lives inside the module it demonstrates, so cairndex is
// resolved locally. corpus-hugo is a separate, independently published
// module — imported for real, not replaced, so this exercises the same
// module resolution a real consumer site would go through.
replace github.com/livingstaccato/cairndex => ../

require (
	github.com/livingstaccato/cairndex v0.0.0-20260908154955-84be95e650d7 // indirect
	github.com/livingstaccato/corpus-hugo v0.0.0-20260907170211-89eaa4a4e682 // indirect
)

module github.com/trpcgo/example-start-trpc

go 1.26.0

replace github.com/befabri/trpcgo => ../../..

tool github.com/befabri/trpcgo/cmd/trpcgo

require (
	github.com/befabri/trpcgo v0.0.0-00010101000000-000000000000
	github.com/go-chi/chi/v5 v5.3.2
	github.com/go-playground/validator/v10 v10.30.4
)

require (
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/leodido/go-urn v1.5.0 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

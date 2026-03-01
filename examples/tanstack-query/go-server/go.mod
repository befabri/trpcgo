module github.com/trpcgo/example-query

go 1.26.0

replace github.com/trpcgo/trpcgo => ../../..

tool github.com/trpcgo/trpcgo/cmd/trpcgo

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/trpcgo/trpcgo v0.0.0-00010101000000-000000000000
)

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
)

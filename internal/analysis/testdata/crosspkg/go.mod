module example.com/crosspkg

go 1.26.0

require github.com/befabri/trpcgo v0.0.0

require (
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
)

replace github.com/befabri/trpcgo => ../../../..

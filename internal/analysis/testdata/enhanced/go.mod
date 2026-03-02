module example.com/enhanced

go 1.26.0

require github.com/befabri/trpcgo v0.0.0

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
)

replace github.com/befabri/trpcgo => ../../../..

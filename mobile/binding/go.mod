module zakki/mobile/binding

go 1.27.3

require (
	golang.org/x/mobile v0.0.0-20261011034528-197770444867
	zakki/core v0.0.0-00010101000000-000000000000
)

require (
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
)

replace zakki/core => ../../core

tool golang.org/x/mobile/cmd/gomobile

// A collage plugin that runs a gesso application's lifecycle inside collage:
// its modules start when the server starts serving and stop after it has
// stopped.
//
// It requires collage the way any consumer does.
module github.com/Elagoht/collage-gesso

go 1.26.10

require (
	github.com/Elagoht/collage v0.80.0
	github.com/Elagoht/gesso v0.5.0
)
